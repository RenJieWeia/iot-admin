// Package ingest 是 HTTP 与 MQTT 两路接入的唯一业务管道。
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/latest"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// ReasonInternal 表示平台侧故障（DB 不可用等），与数据问题区分。
const ReasonInternal = "internal_error"

// Processor 执行"信封校验 -> 逐终端校验/清洗 -> 写最新值 -> 按终端发布"。
type Processor struct {
	DB    *pgxpool.Pool
	Meta  *meta.Store
	Pub   publish.Publisher
	Exc   *exception.Recorder
	Locks *store.ShardedLock
	Now   func() time.Time
	Log   *slog.Logger
}

// 项级失败原因闭集（唯一来源在 validate，这里只做别名，方便 ingest 的调用方与用例引用）。
const (
	ItemValueType       = validate.ItemValueType
	ItemValueMissing    = validate.ItemValueMissing
	ItemTimeUnparseable = validate.ItemTimeUnparseable
)

// MaxItemResults 是应答里逐项失败明细的上限；超出只截断明细，FailedCount 仍报真实总数。
// **对外契约**：网关侧只能依赖前 MaxItemResults 条明细，需要全量时按 failedCount 自行核对。
const MaxItemResults = 20

// ItemResult 是单条数据项的失败明细（对外应答契约）。
type ItemResult struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Result 是单个终端的处理结局（HTTP 响应的 results 项）。
//
// Accepted 的判据是 !(失败项>0 且 成功项==0)：全部项失败时 Accepted=false 且
// Reason=all_items_failed（项级全失败），Items 列出全部失败项；部分失败时 Accepted=true
// 并带 FailedCount/Items。终端级结构性拒收仍走各自的 Reason。
//
// **对外契约**（改动需通知网关侧）：
//   - failedCount/items 是本次新增字段：items 只列**失败项**，最多 MaxItemResults 条，
//     超出时 failedCount 仍是真实总数；成功项不逐项回传。
//   - missingCodes 语义已收窄：只回传 value_missing（值缺失/必填未上报）的 code，
//     采集时间不可解析的项不再进入该字段（改由 items[].reason=time_unparseable 表达）。
type Result struct {
	TerminalID   string       `json:"terminalId,omitempty"`
	Accepted     bool         `json:"accepted"`
	Reason       string       `json:"reason,omitempty"`
	MissingCodes []string     `json:"missingCodes,omitempty"`
	Written      int          `json:"written"`
	Published    bool         `json:"published"`
	FailedCount  int          `json:"failedCount,omitempty"`
	Items        []ItemResult `json:"items,omitempty"`
}

// BatchResult 是一批消息的逐终端结果。
type BatchResult struct {
	Results []Result `json:"results"`
}

// Handle 处理一条 v2 批量消息。返回的 error 只可能是整包级错误（*validate.EnvelopeError）
// 或平台故障；逐终端拒收体现在 Results 里，不影响同批其他终端。
//
// 写入路径只剩 latest_value：窗口判定、小时暂存、结算与快照已随 0009 迁移下线。
//
// 等价于 HandleSource(ctx, "api", …)：HTTP 接入路径沿用原签名。
func (p *Processor) Handle(ctx context.Context, ingressGateway string, raw []byte) (BatchResult, error) {
	return p.HandleSource(ctx, "api", ingressGateway, raw)
}

// HandleSource 与 Handle 相同，额外记录接入来源（mqtt|api），只用于异常 detail 的来源标记
// （R-030 §2.4 / R-031），不参与任何路径判定。
func (p *Processor) HandleSource(ctx context.Context, source, ingressGateway string, raw []byte) (BatchResult, error) {
	now := p.Now()
	env, err := validate.ParseEnvelope(raw)
	if err != nil {
		return BatchResult{}, err
	}
	ingress := strings.TrimSpace(ingressGateway)
	if ingress == "" {
		ingress = env.GatewayID
	}
	p.Log.Debug("接入批量消息", "messageId", env.MessageID, "gateway", ingress,
		"terminals", len(env.Terminals))

	out := BatchResult{Results: make([]Result, 0, len(env.Terminals))}
	// 接入网关与报文 gatewayId 的一致性只与信封有关：整批一次判定，逐终端给同样 reason。
	if ingress != env.GatewayID {
		terr := &validate.TerminalError{
			Reason: validate.ReasonProtocolError,
			Detail: fmt.Sprintf("网关标识不一致: 接入=%q 报文=%q", ingress, env.GatewayID),
		}
		for _, tw := range env.Terminals {
			out.Results = append(out.Results, p.rejectTerminal(ctx, tw.TerminalID, ingress, now, terr, ""))
		}
		return out, nil
	}

	for _, tw := range env.Terminals {
		out.Results = append(out.Results, p.handleTerminal(ctx, source, env, ingress, tw, now))
	}
	return out, nil
}

// handleTerminal 处理单个终端；任何失败都只影响本终端。
func (p *Processor) handleTerminal(ctx context.Context, source string, env *validate.Envelope, ingress string,
	tw validate.TerminalWire, now time.Time) Result {

	// 2.1/2.2 结构、value、collectedAt、code 重复、跨度
	// 项级失败（value 缺失/时间不可解析）从解析阶段带出，结构性错误仍是 terr。
	tp, terr, parseFailures := validate.ParseTerminal(tw)
	if terr != nil {
		return p.rejectTerminal(ctx, tw.TerminalID, ingress, now, terr, "")
	}
	// 2.3 网关三方一致（R9）中"注册表 gateway_id"这一侧；信封侧一致性已在 Handle 判定。
	term, err := p.Meta.LoadTerminal(ctx, tp.TerminalID)
	if err != nil {
		if errors.Is(err, validate.ErrTerminalUnknown) {
			return p.rejectTerminal(ctx, tp.TerminalID, ingress, now, &validate.TerminalError{
				Reason: validate.ReasonTerminalUnknown,
				Detail: "终端未注册或已停用: " + tp.TerminalID,
			}, "")
		}
		p.Log.Error("读取终端失败", "terminal", tp.TerminalID, "err", err)
		return Result{TerminalID: tp.TerminalID, Reason: ReasonInternal}
	}
	if term.GatewayID != ingress {
		return p.rejectTerminal(ctx, tp.TerminalID, ingress, now, &validate.TerminalError{
			Reason: validate.ReasonProtocolError,
			Detail: fmt.Sprintf("网关标识不一致: 接入=%q 注册=%q", ingress, term.GatewayID),
		}, "")
	}
	if item, bad := validate.FindFuture(tp.Items, now); bad {
		return p.rejectTerminal(ctx, tp.TerminalID, ingress, now, &validate.TerminalError{
			Reason: validate.ReasonProtocolError,
			Detail: fmt.Sprintf("采集时间超前平台时间 5 分钟以上: %s", item.Code),
		}, "")
	}
	// 2.4 完整消息：required 属性全覆盖
	props, err := p.Meta.LoadProperties(ctx, term.TypeID)
	if err != nil {
		p.Log.Error("读取物模型失败", "type", term.TypeID, "err", err)
		return Result{TerminalID: tp.TerminalID, Reason: ReasonInternal}
	}
	// 项级失败：先装解析阶段的失败项，再补 required 缺失（同一 code 只记一次，
	// 例如「value 为 null 的必填项」两边都会命中，保留更具体的 value_missing 明细）。
	failures := append([]validate.ItemFailure(nil), parseFailures...)
	failedCodes := make(map[string]struct{}, len(failures))
	for _, f := range failures {
		failedCodes[f.Code] = struct{}{}
	}
	for _, code := range validate.MissingRequired(props, tp.Items) {
		if _, dup := failedCodes[code]; dup {
			continue
		}
		failedCodes[code] = struct{}{}
		failures = append(failures, validate.ItemFailure{
			Code: code, Reason: validate.ItemValueMissing, Detail: "required 属性缺失",
		})
	}
	rules, err := p.Meta.LoadRules(ctx, term.ID)
	if err != nil {
		p.Log.Error("读取质量规则失败", "terminal", term.ID, "err", err)
		return Result{TerminalID: tp.TerminalID, Reason: ReasonInternal}
	}

	// 2.5 逐项清洗
	propByCode := make(map[string]validate.Property, len(props))
	for _, pr := range props {
		propByCode[pr.ItemCode] = pr
	}
	type violationHit struct {
		hour   time.Time
		code   string
		source string // thing_model | rule
		detail string
	}
	// 行级质量标记（R-022）：按 item_code 聚合位掩码与可读摘要，随 store.Value 落库。
	qualityBits := make(map[string]int16, len(tp.Items))
	qualityNotes := make(map[string][]string, len(tp.Items))
	markQuality := func(code string, bit int16, note string) {
		qualityBits[code] |= bit
		qualityNotes[code] = append(qualityNotes[code], note)
	}
	var (
		cleaned          []clean.Out
		unknownCodes     []string
		violations       []violationHit
		unitUnknownHours []time.Time
	)
	cleanFailed := 0 // 项级 value_type 失败数（用于 okCount 与事件明细）
	for _, item := range tp.Items {
		pr, ok := propByCode[item.Code]
		if !ok {
			unknownCodes = append(unknownCodes, item.Code)
			continue
		}
		out, vios, cerr := clean.Item(pr, item)
		if cerr != nil {
			// 值类型与物模型声明不符属项级失败：剔除该项，其余项照常入库。
			cleanFailed++
			failures = append(failures, validate.ItemFailure{
				Code: item.Code, Reason: validate.ItemValueType, Detail: cerr.Error(),
			})
			continue
		}
		if out.UnitUnknown {
			unitUnknownHours = append(unitUnknownHours, staging.HourStart(out.Time))
			markQuality(out.Code, store.QualityUnitUnknown,
				fmt.Sprintf("source=clean item=%s 单位不在换算闭集内，已透传原单位", out.Code))
		}
		for _, v := range vios { // 物模型 min/max 越界
			detail := fmt.Sprintf("%s: %s", v.RuleType, v.Detail)
			violations = append(violations, violationHit{
				hour: staging.HourStart(out.Time), code: out.Code,
				source: "thing_model", detail: detail,
			})
			markQuality(out.Code, store.QualityOutOfRange,
				fmt.Sprintf("source=thing_model item=%s %s", out.Code, detail))
		}
		for _, v := range clean.Check(rules, out) { // 终端质量规则命中
			detail := fmt.Sprintf("%s: %s", v.RuleType, v.Detail)
			violations = append(violations, violationHit{
				hour: staging.HourStart(out.Time), code: out.Code,
				source: "rule", detail: detail,
			})
			markQuality(out.Code, store.QualityRuleHit,
				fmt.Sprintf("source=rule item=%s %s", out.Code, detail))
			p.Log.Info("质量规则命中", "terminal", term.ID, "item", out.Code, "rule", string(v.RuleType))
		}
		cleaned = append(cleaned, out)
	}

	// 异常事件口径不变：msg_incomplete / clean_failed 仍按「终端+小时+类型」各记一次，
	// detail 里列失败项；改成项级只影响"是否整终端拒收"，不改类型与计数方式。
	var msgCodes, typeCodes []string
	for _, f := range failures {
		if f.Reason == validate.ItemValueType {
			typeCodes = append(typeCodes, f.Code)
			continue
		}
		msgCodes = append(msgCodes, f.Code)
	}
	if len(msgCodes) > 0 {
		p.Exc.Event(ctx, exception.MsgIncomplete, term.ID, ingress, staging.HourStart(now), 1,
			fmt.Sprintf("项级失败项被剔除: %v", msgCodes))
	}
	if len(typeCodes) > 0 {
		p.Exc.Event(ctx, exception.CleanFailed, term.ID, ingress, staging.HourStart(now), 1,
			fmt.Sprintf("清洗失败项被剔除: %v", typeCodes))
	}

	// 2.6 写入：只写 latest_value。小时暂存/结算/快照已随 0009 迁移整条下线，
	// 窗口判定、late_data 事件与窗口外直写快照的分支一并消失。
	// 同一终端的写入仍按分片锁串行（latest_value 的 value_time 守卫依赖它）。
	var written, regressed []clean.Out
	if len(cleaned) > 0 {
		var werr error
		written, regressed, werr = p.write(ctx, term.ID, cleaned, qualityBits, qualityNotes)
		if werr != nil {
			p.Log.Error("写最新值失败", "terminal", term.ID, "err", werr)
			// 写失败属平台侧故障：异常仍记 cache_write_failed 保可观测，但返回值必须是
			// internal_error，让 HTTP 层整包 503、网关按状态码整批重传。
			// 200 + accepted:false 会被网关当成数据问题而丢弃整批（R-014）。
			p.Exc.Event(ctx, exception.CacheWriteFailed, term.ID, ingress, staging.HourStart(now), 1, werr.Error())
			return Result{TerminalID: tp.TerminalID, Reason: ReasonInternal}
		}
		for _, o := range regressed {
			// R-026/R-031：detail 带来源与 msgType，便于区分实时上报与补传回填
			// （msgType 只是来源标记，不参与任何路径判定）。
			// 守卫跳过的两种情形（同值幂等重发 / 更早的回拨）在 SQL 层无法区分，detail 如实写「未覆盖」。
			p.Exc.Event(ctx, exception.ProtocolError, term.ID, ingress, staging.HourStart(o.Time), 1,
				fmt.Sprintf("source=%s msgType=%s value_time 未大于已有值，未覆盖: item=%s collectedAt=%s",
					sourceTag(source), msgTypeLabel(env.MsgType), o.Code,
					o.Time.Format(time.RFC3339Nano)))
		}
	}

	// 判定规则：Accepted = !(失败项>0 且 成功项==0)。okCount 统计"没被判为项级失败的数据项"，
	// 含物模型未声明的编码（那类继续忽略并计 clean_failed，不算失败）。
	// 全部项失败时给机器可读的 reason=all_items_failed；该路径不经过 rejectTerminal，
	// 因此不产生终端级拒收事件，也不影响 HTTP 503 判定（503 只看 internal_error）。
	okCount := len(tp.Items) - cleanFailed
	failedCount := len(failures)
	accepted := !(failedCount > 0 && okCount == 0)
	reason := ""
	if !accepted {
		reason = validate.ReasonAllItemsFailed
	}
	res := Result{
		TerminalID:   tp.TerminalID,
		Accepted:     accepted,
		Reason:       reason,
		Written:      len(written),
		FailedCount:  failedCount,
		Items:        itemResults(failures),
		MissingCodes: missingCodes(failures),
	}
	// 2.9 按终端发布一条：只发**实际写入成功**的项（written 来自 latest.Upsert 的 returning）。
	// 幂等重发被 value_time 守卫跳过、不在 written 里，因此不会重复发布（验收 2）。
	if len(written) > 0 {
		res.Published = p.publishRealtime(ctx, term, written, ingress, now)
	}
	for _, v := range violations {
		p.Exc.Event(ctx, exception.QualityViolation, term.ID, ingress, v.hour, 1,
			fmt.Sprintf("source=%s item=%s %s", v.source, v.code, v.detail))
	}
	for _, h := range unitUnknownHours {
		p.Exc.Event(ctx, exception.CleanFailed, term.ID, ingress, h, 1, "单位不在换算闭集内，已透传原单位")
	}
	if len(unknownCodes) > 0 {
		p.Exc.Event(ctx, exception.CleanFailed, term.ID, ingress, staging.HourStart(now), 1,
			fmt.Sprintf("物模型未声明的数据项被忽略: %v", unknownCodes))
	}
	return res
}

// write 在同一事务里把本终端的全部清洗结果写入 latest_value。
//
// 返回值：
//   - written：真正落库的项（来自 latest.Upsert 的 returning）；幂等重发被 value_time 守卫跳过的
//     不在集合里 —— 发布只认这个集合，因此重复上报不会重复发布；
//   - regressed：被守卫跳过的项（collectedAt 回拨不覆盖），由调用方记协议异常。
func (p *Processor) write(ctx context.Context, terminalID string, cleaned []clean.Out,
	qualityBits map[string]int16, qualityNotes map[string][]string) (written, regressed []clean.Out, err error) {
	unlock := p.Locks.Lock(terminalID)
	defer unlock()

	vals := make([]store.Value, len(cleaned))
	for i, o := range cleaned {
		vals[i] = store.Value{
			TerminalID: terminalID, ItemCode: o.Code,
			ValueNum: o.Number, ValueText: o.Text, Unit: o.Unit, ValueTime: o.Time,
			Quality:       qualityBits[o.Code],
			QualityReason: strings.Join(qualityNotes[o.Code], "; "),
		}
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
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	for _, o := range cleaned {
		if _, ok := writtenSet[o.Code]; ok {
			written = append(written, o)
			continue
		}
		regressed = append(regressed, o)
	}
	return written, regressed, nil
}

// sourceTag 把接入来源收敛成 source= 标记（只用于异常 detail/日志）。
func sourceTag(source string) string {
	if source == "mqtt" {
		return "mqtt"
	}
	return "api"
}

// msgTypeLabel 把可选 msgType 变成可读标记（只用于 detail/日志，不参与路径判定）。
func msgTypeLabel(msgType int) string {
	switch msgType {
	case 1:
		return "1(实时)"
	case 2:
		return "2(补传)"
	case 0:
		return "未声明"
	default:
		return fmt.Sprintf("%d", msgType)
	}
}

func (p *Processor) publishRealtime(ctx context.Context, term validate.Terminal, outs []clean.Out,
	gatewayID string, now time.Time) bool {
	items := make([]publish.Item, 0, len(outs))
	for _, o := range outs {
		items = append(items, publish.Item{
			Code: o.Code, Value: o.Value, Unit: o.Unit,
			CollectedAt: o.Time.In(staging.Zone).Format(time.RFC3339Nano),
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

// rejectTerminal 只拒收一个终端并按 reason 记异常（事件型）。
func (p *Processor) rejectTerminal(ctx context.Context, terminalID, gatewayID string, now time.Time,
	terr *validate.TerminalError, sample string) Result {
	kind := eventTypeFor(terr.Reason)
	detail := terr.Error()
	if sample != "" {
		detail += " | " + sample
	}
	p.Exc.Event(ctx, kind, terminalID, gatewayID, staging.HourStart(now), 1, detail)
	p.Log.Info("终端被拒收", "reason", terr.Reason, "terminal", terminalID, "detail", detail)
	codes := terr.Codes
	if len(codes) > validate.MaxMissingCodes {
		codes = codes[:validate.MaxMissingCodes]
	}
	return Result{TerminalID: terminalID, Reason: terr.Reason, MissingCodes: codes}
}

// itemResults 把项级失败转成应答明细，最多 MaxItemResults 条（FailedCount 仍报真实总数）。
func itemResults(failures []validate.ItemFailure) []ItemResult {
	if len(failures) == 0 {
		return nil
	}
	n := len(failures)
	if n > MaxItemResults {
		n = MaxItemResults
	}
	out := make([]ItemResult, 0, n)
	for _, f := range failures[:n] {
		out = append(out, ItemResult{Code: f.Code, Reason: f.Reason, Detail: f.Detail})
	}
	return out
}

// missingCodes 兼容老网关：只回传 value_missing 的 code，沿用 MaxMissingCodes 上限。
func missingCodes(failures []validate.ItemFailure) []string {
	var codes []string
	for _, f := range failures {
		if f.Reason != validate.ItemValueMissing {
			continue
		}
		codes = append(codes, f.Code)
		if len(codes) >= validate.MaxMissingCodes {
			break
		}
	}
	return codes
}

// eventTypeFor 把逐终端 reason 映射到唯一的事件型枚举。
func eventTypeFor(reason string) exception.ErrorType {
	switch reason {
	case validate.ReasonMsgIncomplete:
		return exception.MsgIncomplete
	case validate.ReasonProtocolError:
		return exception.ProtocolError
	case validate.ReasonCleanFailed:
		return exception.CleanFailed
	case validate.ReasonTerminalUnknown:
		return exception.TerminalUnknown
	default:
		return exception.ProtocolError
	}
}
