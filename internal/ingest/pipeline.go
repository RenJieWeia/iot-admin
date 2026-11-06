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
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// ReasonInternal 表示平台侧故障（DB 不可用等），与数据问题区分。
const ReasonInternal = "internal_error"

// Processor 执行"信封校验 -> 逐终端校验/清洗 -> 写缓存/暂存 -> 按终端发布"。
type Processor struct {
	DB    *pgxpool.Pool
	Meta  *meta.Store
	Pub   publish.Publisher
	Exc   *exception.Recorder
	Locks *store.ShardedLock
	Now   func() time.Time
	Log   *slog.Logger
}

// Result 是单个终端的处理结局（HTTP 响应的 results 项）。
type Result struct {
	TerminalID   string   `json:"terminalId,omitempty"`
	Accepted     bool     `json:"accepted"`
	Reason       string   `json:"reason,omitempty"`
	MissingCodes []string `json:"missingCodes,omitempty"`
	Written      int      `json:"written"`
	Published    bool     `json:"published"`
}

// BatchResult 是一批消息的逐终端结果。
type BatchResult struct {
	Results []Result `json:"results"`
}

// Handle 处理一条 v2 批量消息。返回的 error 只可能是整包级错误（*validate.EnvelopeError）
// 或平台故障；逐终端拒收体现在 Results 里，不影响同批其他终端。
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

	cfg, err := p.Meta.LoadWindowConfig(ctx)
	if err != nil {
		// 平台侧故障：逐终端标 internal_error，由 HTTP 层整包 503（不记数据类异常）。
		p.Log.Error("读取结算窗口配置失败", "err", err)
		for _, tw := range env.Terminals {
			out.Results = append(out.Results, Result{TerminalID: tw.TerminalID, Reason: ReasonInternal})
		}
		return out, nil
	}
	for _, tw := range env.Terminals {
		out.Results = append(out.Results, p.handleTerminal(ctx, source, env, ingress, tw, now, cfg))
	}
	return out, nil
}

// handleTerminal 处理单个终端；任何失败都只影响本终端。
func (p *Processor) handleTerminal(ctx context.Context, source string, env *validate.Envelope, ingress string,
	tw validate.TerminalWire, now time.Time, cfg settle.WindowConfig) Result {

	// 2.1/2.2 结构、value、collectedAt、code 重复、跨度
	tp, terr := validate.ParseTerminal(tw)
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
	if missing := validate.MissingRequired(props, tp.Items); len(missing) > 0 {
		return p.rejectTerminal(ctx, tp.TerminalID, ingress, now, &validate.TerminalError{
			Reason: validate.ReasonMsgIncomplete,
			Detail: fmt.Sprintf("required 属性缺失: %v", missing),
			Codes:  missing,
		}, "")
	}
	rules, err := p.Meta.LoadRules(ctx, term.ID)
	if err != nil {
		p.Log.Error("读取质量规则失败", "terminal", term.ID, "err", err)
		return Result{TerminalID: tp.TerminalID, Reason: ReasonInternal}
	}
	window := cfg.Resolve(term.TypeID, term.DomainID)

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
	for _, item := range tp.Items {
		pr, ok := propByCode[item.Code]
		if !ok {
			unknownCodes = append(unknownCodes, item.Code)
			continue
		}
		out, vios, cerr := clean.Item(pr, item)
		if cerr != nil {
			return p.rejectTerminal(ctx, tp.TerminalID, ingress, now, &validate.TerminalError{
				Reason: validate.ReasonCleanFailed,
				Detail: cerr.Error(),
				Codes:  []string{item.Code},
			}, "")
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

	// 2.6 两段写入模型（R-030）：按归属小时是否已到关闭时刻分流，**不再丢弃**。
	//   - 窗口内：latest_value + hourly_staging，等结算任务；
	//   - 窗口外：latest_value 照常写 + 直接修正 hourly_snapshot（revision++），不发布实时。
	// 窗口判定只走 staging.InWindow/CloseAt 这一处口径。
	var inWindow, outWindow []clean.Out
	lateCount := 0 // 仅用于事件统计；口径判定已改为按实际写入行数
	for _, o := range cleaned {
		hour := staging.HourStart(o.Time)
		if !staging.InWindow(hour, window, now) {
			lateCount++
			outWindow = append(outWindow, o)
			// R-026/R-031：detail 带来源与 msgType，便于区分实时上报与补传回填。
			p.Exc.Event(ctx, exception.LateData, term.ID, ingress, hour, 1,
				fmt.Sprintf("source=%s msgType=%s item=%s collectedAt=%s",
					sourceTag(source), msgTypeLabel(env.MsgType), o.Code,
					o.Time.Format(time.RFC3339Nano)))
			continue
		}
		inWindow = append(inWindow, o)
	}

	// 2.7 串行化写入：窗口内与窗口外同一事务内分别处理（混存时一次提交）
	var written, regressed []clean.Out
	if len(cleaned) > 0 && (len(inWindow) > 0 || len(outWindow) > 0) {
		var werr error
		written, regressed, werr = p.write(ctx, term.ID, inWindow, outWindow, qualityBits, qualityNotes,
			writeVersionMeta{source: versionSourceFor(env.MsgType), actor: ""})
		if werr != nil {
			p.Log.Error("写缓存/暂存失败", "terminal", term.ID, "err", werr)
			// 写失败属平台侧故障：异常仍记 cache_write_failed 保可观测，但返回值必须是
			// internal_error，让 HTTP 层整包 503、网关按状态码整批重传。
			// 200 + accepted:false 会被网关当成数据问题而丢弃整批（R-014）。
			p.Exc.Event(ctx, exception.CacheWriteFailed, term.ID, ingress, staging.HourStart(now), 1, werr.Error())
			return Result{TerminalID: tp.TerminalID, Reason: ReasonInternal}
		}
		for _, o := range regressed {
			p.Exc.Event(ctx, exception.ProtocolError, term.ID, ingress, staging.HourStart(o.Time), 1,
				fmt.Sprintf("collectedAt 回拨不覆盖: item=%s collectedAt=%s", o.Code, o.Time.Format(time.RFC3339Nano)))
		}
	}

	res := Result{TerminalID: tp.TerminalID, Accepted: true, Written: len(written)}
	// 2.9 按终端发布一条：只发窗口内的值（窗口外数据不触发实时主题）
	if published := inWindowOnly(written, inWindow); len(published) > 0 {
		res.Published = p.publishRealtime(ctx, term, published, ingress, now)
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
	// 整条消息全部落在窗口外时，仍按迟到语义回报（accepted=false + reason=late_data），
	// 但数据已经直写快照、latest_value 也写入了 —— R-030 只取消了"丢弃"，没有改对外结果口径。
	// R-035 口径：窗口外数据已落快照 → accepted=true、reason=""。
	// 幂等重发（value_time 相等被守卫跳过，Written=0）同样属于"已受理"，不再回 late_data；
	// 迟到信息仍由 late_data 事件承载（R-026）。真正的写入失败在 write() 里已返回
	// internal_error（HTTP 整包 503），不会被这里的 accepted=true 掩盖。
	return res
}

// writeVersionMeta 决定这次写入是否要在版本表留痕（R-034）：
// source=0 表示实时写入（不写版本行）；1=MQTT 补录；2=手工 API。actor 仅手工 API 用。
type writeVersionMeta struct {
	source int16
	actor  string
}

// versionSourceFor 把 msgType 映射成版本来源：只有 msgType=2（补录）才留痕。
func versionSourceFor(msgType int) int16 {
	if msgType == 2 {
		return settle.VersionSourceMQTTBackfill
	}
	return 0
}

// write 在同一事务里串行化写入 latest_value、hourly_staging 与（窗口外）hourly_snapshot；
// 补录来源时在同一事务里追加 snapshot_cell_history 版本行。
//
// 返回值：
//   - written：实际写入任一表的项（窗口内看 latest 守卫、窗口外看快照 returning）
//   - regressed：**只有窗口内**的回拨项（窗口外旧值不更新是预期，不算回拨）
func (p *Processor) write(ctx context.Context, terminalID string, inWindow, outWindow []clean.Out,
	qualityBits map[string]int16, qualityNotes map[string][]string,
	ver writeVersionMeta) (written, regressed []clean.Out, err error) {
	unlock := p.Locks.Lock(terminalID)
	defer unlock()

	all := make([]clean.Out, 0, len(inWindow)+len(outWindow))
	all = append(all, inWindow...)
	all = append(all, outWindow...)
	vals := make([]store.Value, len(all))
	for i, o := range all {
		vals[i] = store.Value{
			TerminalID: terminalID, ItemCode: o.Code,
			ValueNum: o.Number, ValueText: o.Text, Unit: o.Unit, ValueTime: o.Time,
			Quality:       qualityBits[o.Code],
			QualityReason: strings.Join(qualityNotes[o.Code], "; "),
		}
	}
	rows := make([]staging.Row, len(inWindow))
	for i, o := range inWindow {
		rows[i] = staging.Row{Value: vals[i], HourStart: staging.HourStart(o.Time)}
	}
	// 窗口外：直接进快照（与结算共用 UpsertSnapshot，不另写一份 SQL）。
	var snaps []staging.Row
	for i, o := range outWindow {
		v := vals[len(inWindow)+i]
		snaps = append(snaps, staging.Row{Value: v, HourStart: staging.HourStart(o.Time)})
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
	var cells []settle.CellRevision
	if len(snaps) > 0 {
		cells, err = settle.UpsertSnapshot(ctx, tx, snaps)
		if err != nil {
			return nil, nil, err
		}
	}
	// 版本留痕：只有补录来源、且本次确实写入了快照格（同一事务）。
	if ver.source != 0 && len(cells) > 0 {
		byCode := make(map[string]clean.Out, len(outWindow))
		for _, o := range outWindow {
			byCode[o.Code] = o
		}
		versions := make([]settle.VersionRow, 0, len(cells))
		for _, c := range cells {
			o, ok := byCode[c.ItemCode]
			if !ok {
				continue
			}
			versions = append(versions, settle.VersionRow{
				TerminalID: c.TerminalID, ItemCode: c.ItemCode, HourStart: c.HourStart,
				Revision: c.Revision, Source: ver.source, ValueTime: o.Time, Actor: ver.actor,
			})
		}
		if err := settle.InsertVersions(ctx, tx, versions); err != nil {
			return nil, nil, err
		}
	}
	snapshotWritten := make(map[string]struct{}, len(cells))
	for _, c := range cells {
		snapshotWritten[c.ItemCode] = struct{}{}
	}
	for _, o := range inWindow {
		if _, ok := writtenSet[o.Code]; ok {
			written = append(written, o)
		} else {
			regressed = append(regressed, o)
		}
	}
	for _, o := range outWindow {
		if _, ok := snapshotWritten[o.Code]; ok {
			written = append(written, o)
		}
		// 窗口外被 value_time 守卫跳过：属于幂等跳过，不记为回拨。
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
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

// inWindowOnly 从写入成功的项里筛出窗口内的（实时发布只覆盖窗口内数据）。
func inWindowOnly(written, inWindow []clean.Out) []clean.Out {
	if len(written) == 0 || len(inWindow) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(inWindow))
	for _, o := range inWindow {
		keep[o.Code] = struct{}{}
	}
	out := make([]clean.Out, 0, len(written))
	for _, o := range written {
		if _, ok := keep[o.Code]; ok {
			out = append(out, o)
		}
	}
	return out
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
