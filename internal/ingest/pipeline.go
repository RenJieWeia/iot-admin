// Package ingest 是 HTTP 与 MQTT 两路接入的唯一业务管道。
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/latest"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// ReasonInternal 表示平台侧故障（DB 不可用等），与数据问题区分。
const ReasonInternal = "internal_error"

// Processor 执行"校验 -> 清洗 -> 写缓存/暂存 -> 发布实时主题"。
type Processor struct {
	DB    *pgxpool.Pool
	Reg   *validate.Registry
	Pub   publish.Publisher
	Exc   *exception.Recorder
	Locks *store.ShardedLock
	Now   func() time.Time
	Log   *slog.Logger
}

// Result 是一条消息的处理结局。
type Result struct {
	Accepted  bool   `json:"accepted"`
	Reason    string `json:"reason,omitempty"`
	Written   int    `json:"written"`
	Published bool   `json:"published"`
}

// Handle 处理一条完整消息。gatewayID 来自 HTTP header 或 MQTT 主题。
func (p *Processor) Handle(ctx context.Context, gatewayID string, raw []byte) Result {
	now := p.Now()
	msg, err := validate.ParseMessage(raw)
	if err != nil {
		return p.reject(ctx, exception.MsgIncomplete, "", gatewayID, now, raw, err)
	}
	if gatewayID == "" {
		gatewayID = msg.GatewayID
	}
	if msg.GatewayID != gatewayID {
		return p.reject(ctx, exception.ProtocolError, msg.TerminalID, gatewayID, now, raw,
			fmt.Errorf("网关标识不一致: 接入=%q 报文=%q", gatewayID, msg.GatewayID))
	}
	if item, bad := validate.FindFuture(msg.Items, now); bad {
		return p.reject(ctx, exception.ProtocolError, msg.TerminalID, gatewayID, now, raw,
			fmt.Errorf("采集时间超前平台时间 5 分钟以上: %s", item.Code))
	}
	p.Log.Debug("接入消息", "messageId", msg.MessageID, "gateway", gatewayID, "terminal", msg.TerminalID)

	term, err := p.Reg.LoadTerminal(ctx, msg.TerminalID)
	if err != nil {
		if errors.Is(err, validate.ErrTerminalUnknown) {
			return p.reject(ctx, exception.AuthFailed, msg.TerminalID, gatewayID, now, raw,
				fmt.Errorf("终端不存在或已停用: %s", msg.TerminalID))
		}
		p.Log.Error("读取终端失败", "terminal", msg.TerminalID, "err", err)
		return Result{Reason: ReasonInternal}
	}
	props, err := p.Reg.LoadProperties(ctx, term.TypeID)
	if err != nil {
		p.Log.Error("读取物模型失败", "type", term.TypeID, "err", err)
		return Result{Reason: ReasonInternal}
	}
	if err := validate.CheckComplete(props, msg.Items); err != nil {
		return p.reject(ctx, exception.MsgIncomplete, term.ID, gatewayID, now, raw, err)
	}
	rules, err := clean.LoadRules(ctx, p.DB, term.ID)
	if err != nil {
		p.Log.Error("读取质量规则失败", "terminal", term.ID, "err", err)
		return Result{Reason: ReasonInternal}
	}
	window, err := settle.WindowSeconds(ctx, p.DB, term.TypeID, term.DomainID)
	if err != nil {
		p.Log.Error("读取结算窗口失败", "type", term.TypeID, "err", err)
		return Result{Reason: ReasonInternal}
	}

	propByCode := make(map[string]validate.Property, len(props))
	for _, pr := range props {
		propByCode[pr.ItemCode] = pr
	}
	var (
		cleaned          []clean.Out
		unknownCodes     []string
		violationHours   []time.Time
		unitUnknownHours []time.Time
	)
	for _, item := range msg.Items {
		pr, ok := propByCode[item.Code]
		if !ok {
			unknownCodes = append(unknownCodes, item.Code)
			continue
		}
		out, vios, cerr := clean.Item(pr, item)
		if cerr != nil {
			return p.reject(ctx, exception.CleanFailed, term.ID, gatewayID, now, raw, cerr)
		}
		if out.UnitUnknown {
			unitUnknownHours = append(unitUnknownHours, staging.HourStart(out.Time))
		}
		vios = append(vios, clean.Check(rules, out)...)
		if len(vios) > 0 {
			violationHours = append(violationHours, staging.HourStart(out.Time))
			p.Log.Info("质量规则命中", "terminal", term.ID, "item", out.Code,
				"rules", fmt.Sprint(vios))
		}
		cleaned = append(cleaned, out)
	}

	// 归属小时 + 窗口判定：到达关闭时刻即迟到，不写缓存、不写暂存。
	var inWindow []clean.Out
	lateCount := 0
	for _, o := range cleaned {
		hour := staging.HourStart(o.Time)
		if !staging.InWindow(hour, window, now) {
			lateCount++
			p.Exc.Event(ctx, exception.LateData, term.ID, gatewayID, hour, 1, string(raw))
			continue
		}
		inWindow = append(inWindow, o)
	}

	var written []clean.Out
	if len(inWindow) > 0 {
		var regressed []clean.Out
		var werr error
		written, regressed, werr = p.write(ctx, term.ID, inWindow)
		if werr != nil {
			p.Log.Error("写缓存/暂存失败", "terminal", term.ID, "err", werr)
			p.Exc.Event(ctx, exception.CacheWriteFailed, term.ID, gatewayID, staging.HourStart(now), 1, werr.Error())
			return Result{Reason: string(exception.CacheWriteFailed)}
		}
		for _, o := range regressed {
			p.Exc.Event(ctx, exception.ProtocolError, term.ID, gatewayID, staging.HourStart(o.Time), 1,
				fmt.Sprintf("value_time 回拨不覆盖: item=%s time=%s", o.Code, o.Time.Format(time.RFC3339Nano)))
		}
	}

	res := Result{Accepted: true, Written: len(written)}
	if len(written) > 0 {
		res.Published = p.publishRealtime(ctx, term, written, gatewayID, now)
	}
	for _, h := range violationHours {
		p.Exc.Event(ctx, exception.QualityViolation, term.ID, gatewayID, h, 1, "")
	}
	for _, h := range unitUnknownHours {
		p.Exc.Event(ctx, exception.CleanFailed, term.ID, gatewayID, h, 1, "单位不可识别，已透传原单位")
	}
	if len(unknownCodes) > 0 {
		p.Exc.Event(ctx, exception.CleanFailed, term.ID, gatewayID, staging.HourStart(now), 1,
			fmt.Sprintf("物模型未声明的数据项被忽略: %v", unknownCodes))
	}
	if res.Written == 0 && len(cleaned) > 0 && lateCount == len(cleaned) {
		res.Accepted = false
		res.Reason = string(exception.LateData)
	}
	return res
}

// write 在同一事务里串行化写入 latest_value 与 hourly_staging；返回写入项与回拨项。
// 每条消息对每张表只发一条批量语句，避免逐项 round trip。
func (p *Processor) write(ctx context.Context, terminalID string, outs []clean.Out) (written, regressed []clean.Out, err error) {
	unlock := p.Locks.Lock(terminalID)
	defer unlock()
	vals := make([]store.Value, len(outs))
	rows := make([]staging.Row, len(outs))
	for i, o := range outs {
		vals[i] = store.Value{
			TerminalID: terminalID, ItemCode: o.Code,
			ValueNum: o.Number, ValueText: o.Text, Unit: o.Unit, ValueTime: o.Time,
		}
		rows[i] = staging.Row{Value: vals[i], HourStart: staging.HourStart(o.Time)}
	}
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	writtenSet, err := latest.Upsert(ctx, tx, vals)
	if err != nil {
		return nil, nil, err
	}
	if err := staging.Upsert(ctx, tx, rows); err != nil {
		return nil, nil, err
	}
	for _, o := range outs {
		if _, ok := writtenSet[o.Code]; ok {
			written = append(written, o)
		} else {
			regressed = append(regressed, o)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return written, regressed, nil
}

func (p *Processor) publishRealtime(ctx context.Context, term validate.Terminal, outs []clean.Out, gatewayID string, now time.Time) bool {
	items := make([]publish.Item, 0, len(outs))
	for _, o := range outs {
		items = append(items, publish.Item{
			Code: o.Code, Value: o.Value, Unit: o.Unit, Time: o.Time.Format(time.RFC3339Nano),
		})
	}
	payload, err := publish.Build(term.ID, now, items)
	if err != nil {
		p.Log.Error("组装实时报文失败", "terminal", term.ID, "err", err)
		return false
	}
	topic := publish.Topic(term.DomainCode, term.TypeCode, term.ID)
	if err := p.Pub.Publish(topic, payload); err != nil {
		p.Log.Error("发布实时主题失败", "topic", topic, "err", err)
		p.Exc.Event(ctx, exception.MQTTPublishFailed, term.ID, gatewayID, staging.HourStart(now), 1, err.Error())
		return false
	}
	return true
}

// reject 拒收并记异常。状态型异常进区间表，事件型进计数表。
func (p *Processor) reject(ctx context.Context, kind exception.ErrorType, terminalID, gatewayID string,
	now time.Time, raw []byte, cause error) Result {
	sample := string(raw)
	if cause != nil {
		sample += "\n# " + cause.Error()
	}
	if kind.IsInterval() {
		if err := p.Exc.OpenInterval(ctx, kind, terminalID, gatewayID, nil); err != nil {
			p.Log.Error("记录区间型异常失败", "type", kind, "err", err)
		}
	} else {
		p.Exc.Event(ctx, kind, terminalID, gatewayID, staging.HourStart(now), 1, sample)
	}
	p.Log.Info("消息被拒收", "reason", kind, "terminal", terminalID, "detail", errText(cause))
	return Result{Reason: string(kind)}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
