// Package validate 负责批量信封的解析校验与物模型加载。
package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// 整包级错误的 reason（对应 HTTP 400），与逐终端 reason 分开。
const (
	ReasonInvalidJSON        = "invalid_json"
	ReasonTerminalsInvalid   = "terminals_invalid"
	ReasonUnsupportedVersion = "unsupported_version"
	// ReasonGatewayMissing 只表示"传输层没带身份"（HTTP 缺 X-Gateway-Id；
	// MQTT topic 必然带）。payload 里的 gatewayId 已不再参与校验。
	ReasonGatewayMissing     = "gateway_id_missing"
	// ReasonGatewayUnknown / ReasonGatewayDisabled 是接入鉴权的整包级拒收：
	// 未注册的网关不能在接入面发布消息（HTTP 403，MQTT 只记日志丢弃）。
	ReasonGatewayUnknown  = "gateway_unknown"
	ReasonGatewayDisabled = "gateway_disabled"
	ReasonBodyTooLarge    = "body_too_large"
)

// 逐终端拒收的 reason。
const (
	ReasonMsgIncomplete   = "msg_incomplete"
	ReasonProtocolError   = "protocol_error"
	ReasonCleanFailed     = "clean_failed"
	ReasonTerminalUnknown = "terminal_unknown"
	// ReasonAllItemsFailed 表示终端被正常解析、但**所有数据项都不合格**
	// （项级失败：值类型不符 / 值缺失 / 采集时间不可解析）。
	// 它不是结构性拒收：同样 accepted=false，但 items 里会列出全部失败项，
	// 且不产生状态型/事件型的拒收计数（不走 rejectTerminal）。
	ReasonAllItemsFailed = "all_items_failed"
)

// MaxMissingCodes 结果中回传的缺失/非法 code 上限。
const MaxMissingCodes = 10

// MaxSpanPerTerminal 单终端内 items 的采集时间允许跨度。
const MaxSpanPerTerminal = time.Hour

// 项级失败原因闭集（只允许这三个值）：只剔除该项，不再整终端拒收。
const (
	ItemValueType       = "value_type"       // 值类型与物模型声明不符
	ItemValueMissing    = "value_missing"    // 必填项缺失 / value 为 null
	ItemTimeUnparseable = "time_unparseable" // collectedAt 不可解析
)

// ItemFailure 是单条数据项的校验失败；调用方剔除该项并继续处理其余项。
type ItemFailure struct {
	Code   string
	Reason string
	Detail string
}

// EnvelopeError 是整包级错误，调用方必须以 400 整体拒收。
type EnvelopeError struct {
	Reason string
	Detail string
}

func (e *EnvelopeError) Error() string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Detail
}

// TerminalError 是单终端拒收原因；Codes 为缺失或非法的 code（最多 MaxMissingCodes 个）。
type TerminalError struct {
	Reason string
	Detail string
	Codes  []string
}

func (e *TerminalError) Error() string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Detail
}

// Envelope 是 v2 批量信封：一条消息承载同一网关下的多个终端。
type Envelope struct {
	Ver       int
	MessageID string
	// GatewayID 是历史自报字段：MQTT 身份只认 topic，payload 里的值解析后忽略，
	// 不参与校验、日志判定与落库（仅为兼容存量报文的 JSON 形状保留）。
	GatewayID string
	// MsgType 是可选的上游报文类型（R-031）：1 实时、2 补传；0 表示未声明。
	// **只用于异常 detail/日志的来源标记，不参与任何路径判定**。
	MsgType   int
	Terminals []TerminalWire
}

// TerminalWire 保持 items 的原始 JSON，逐终端解析失败只影响该终端。
type TerminalWire struct {
	TerminalID string          `json:"terminalId"`
	Items      json.RawMessage `json:"items"`
}

// RawItem 保持 value 的原始 JSON，等拿到物模型声明再按类型解析。
type RawItem struct {
	Code        string          `json:"code"`
	Value       json.RawMessage `json:"value"`
	Unit        string          `json:"unit"`
	CollectedAt string          `json:"collectedAt"`

	ParsedTime time.Time `json:"-"`
}

// TerminalPayload 是逐终端解析结果，items 已完成结构与时间校验。
type TerminalPayload struct {
	TerminalID string
	Items      []RawItem
}

type envelopeWire struct {
	Ver       *int            `json:"ver"`
	MessageID string          `json:"messageId"`
	GatewayID string          `json:"gatewayId"`
	MsgType   *int            `json:"msgType"` // 可选：1 实时 / 2 补传，仅来源标记
	Terminals json.RawMessage `json:"terminals"`
}

// ParseEnvelope 只做整包级校验（R1/R13）：JSON 可解析、ver 受支持、terminals 为数组且非空。
// gatewayId 只是历史自报字段，解析后保留形状但不参与校验（身份来自接入层）。
// 任何一条不满足都返回 *EnvelopeError，调用方按 400 处理。
func ParseEnvelope(payload []byte) (*Envelope, error) {
	var w envelopeWire
	if err := json.Unmarshal(payload, &w); err != nil {
		return nil, &EnvelopeError{Reason: ReasonInvalidJSON, Detail: err.Error()}
	}
	ver := 1
	if w.Ver != nil {
		ver = *w.Ver
	}
	if ver != 1 {
		return nil, &EnvelopeError{Reason: ReasonUnsupportedVersion, Detail: fmt.Sprintf("ver=%d", ver)}
	}
	if len(w.Terminals) == 0 || string(w.Terminals) == "null" {
		return nil, &EnvelopeError{Reason: ReasonTerminalsInvalid, Detail: "terminals 缺失"}
	}
	var terminals []TerminalWire
	if err := json.Unmarshal(w.Terminals, &terminals); err != nil {
		return nil, &EnvelopeError{Reason: ReasonTerminalsInvalid, Detail: err.Error()}
	}
	if len(terminals) == 0 {
		return nil, &EnvelopeError{Reason: ReasonTerminalsInvalid, Detail: "terminals 为空"}
	}
	msgType := 0
	if w.MsgType != nil {
		msgType = *w.MsgType
	}
	return &Envelope{Ver: ver, MessageID: w.MessageID, GatewayID: w.GatewayID,
		MsgType: msgType, Terminals: terminals}, nil
}

// ParseTerminal 做单终端校验（R2/R3/R4/R7/R8）：terminalId 非空、items 非空、code 非空、
// code 不重复、采集时间跨度不超 1 小时。
//
// 项级失败（value 缺失/null、collectedAt 不可解析）不再整终端拒回：它们进第三个返回值，
// 调用方剔除该项后继续处理其余项。**code 为空仍是结构性错误**（该项连标识都没有），
// 依旧整终端拒收，且 Codes 与改动前一致（同一次解析里的其它非法项也一并回传）。
func ParseTerminal(tw TerminalWire) (TerminalPayload, *TerminalError, []ItemFailure) {
	tp := TerminalPayload{TerminalID: tw.TerminalID}
	if strings.TrimSpace(tw.TerminalID) == "" {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "缺少 terminalId"}, nil
	}
	if len(tw.Items) == 0 || string(tw.Items) == "null" {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "items 缺失"}, nil
	}
	var items []RawItem
	if err := json.Unmarshal(tw.Items, &items); err != nil {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "items 不是数组: " + err.Error()}, nil
	}
	if len(items) == 0 {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "items 为空"}, nil
	}

	seen := make(map[string]struct{}, len(items))
	var codes []string         // 结构性拒收时回传的 code 集合（与改动前保持同一份）
	var failures []ItemFailure // 项级失败：只剔除该项
	var valid []RawItem
	structural := false
	var minAt, maxAt time.Time
	for i := range items {
		it := &items[i]
		if strings.TrimSpace(it.Code) == "" {
			codes = append(codes, fmt.Sprintf("#%d(code 为空)", i))
			structural = true
			continue
		}
		if len(it.Value) == 0 || string(it.Value) == "null" {
			codes = append(codes, it.Code)
			failures = append(failures, ItemFailure{
				Code: it.Code, Reason: ItemValueMissing, Detail: "value 缺失或为 null",
			})
			continue
		}
		t, err := ParseCollectedAt(it.CollectedAt)
		if err != nil {
			codes = append(codes, it.Code)
			failures = append(failures, ItemFailure{
				Code: it.Code, Reason: ItemTimeUnparseable, Detail: err.Error(),
			})
			continue
		}
		if _, dup := seen[it.Code]; dup {
			return tp, &TerminalError{
				Reason: ReasonMsgIncomplete, Detail: "code 重复: " + it.Code, Codes: []string{it.Code},
			}, nil
		}
		seen[it.Code] = struct{}{}
		it.ParsedTime = t
		if minAt.IsZero() || t.Before(minAt) {
			minAt = t
		}
		if maxAt.IsZero() || t.After(maxAt) {
			maxAt = t
		}
		valid = append(valid, *it)
	}
	if structural {
		// 与改动前一致：code 为空即整终端拒收，Codes 收集同一次解析里的全部非法项。
		// 文案只描述结构性原因——value 缺失与 collectedAt 非法已降为项级失败，不再出现在这里。
		return tp, &TerminalError{
			Reason: ReasonMsgIncomplete, Detail: "数据项缺少编码", Codes: codes,
		}, nil
	}
	if maxAt.Sub(minAt) > MaxSpanPerTerminal {
		return tp, &TerminalError{
			Reason: ReasonProtocolError,
			Detail: fmt.Sprintf("单终端采集时间跨度 %s 超过 1 小时", maxAt.Sub(minAt)),
		}, nil
	}
	tp.Items = valid
	return tp, nil, failures
}

// epoch 数值时间戳的可解释区间（R-029）。两端都是闭区间：
//
//	秒：EpochSecondsMin(2020-01-01T00:00:00Z) … EpochSecondsMax(2100-01-01T00:00:00Z)
//	毫秒：同区间的毫秒形态
//
// 用**范围**判定而不是位数：秒区间 10 位、毫秒区间 13 位，天然不重叠；同时挡住 0/1 这类
// 垃圾值（本部署不存在早于 2020 的采集时间，收进来只会变成 late_data 噪声）与 1e15 这类越界值。
const (
	// EpochSecondsMin 是秒级时间戳下界（2020-01-01T00:00:00Z）。
	EpochSecondsMin int64 = 1_577_836_800
	// EpochSecondsMax 是秒级时间戳上界（2100-01-01T00:00:00Z）。
	EpochSecondsMax int64 = 4_102_444_800
	// EpochMillisMin 是毫秒级时间戳下界（同 EpochSecondsMin 的时刻）。
	EpochMillisMin int64 = EpochSecondsMin * 1000
	// EpochMillisMax 是毫秒级时间戳上界（同 EpochSecondsMax 的时刻）。
	EpochMillisMax int64 = EpochSecondsMax * 1000
)

// ParseCollectedAt 解析采集时间（R4 + R-029）：
//   - 接受 +08:00 偏移、Z（换算到 UTC+8）、省略偏移（按 UTC+8 解释）、毫秒精度；
//   - 纯数字按范围判定秒/毫秒（见 EpochSecondsMin…EpochMillisMax），区间外的数值拒收；
//   - 不接受仅日期、空格分隔、空串；
//   - 返回值统一转 Asia/Shanghai。
func ParseCollectedAt(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, errors.New("collectedAt 为空")
	}
	if isDigits(s) {
		return parseEpochDigits(s)
	}
	if parsed, perr := time.Parse(time.RFC3339Nano, s); perr == nil {
		return parsed.In(staging.Zone), nil
	}
	// 省略偏移：按 Asia/Shanghai 解释。
	if parsed, perr := time.ParseInLocation("2006-01-02T15:04:05.999999999", s, staging.Zone); perr == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("collectedAt 不可解析: %q", s)
}

// parseEpochDigits 把纯数字时间戳解释成 UTC 时刻并转到 Asia/Shanghai。
// epoch 数值天然是 UTC，按秒/毫秒的**范围**判定；区间外分别给出"早于/超出"两类错误，
// 非法输入（非数字）不走这里，仍由调用方的"不可解析"分支处理。
func parseEpochDigits(s string) (time.Time, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// 纯数字但超出 int64：必然高于毫秒上界。
		return time.Time{}, errors.New("collectedAt 数值超出可解释范围")
	}
	if v < EpochSecondsMin {
		return time.Time{}, errors.New("collectedAt 数值早于可解释范围")
	}
	if v > EpochMillisMax {
		return time.Time{}, errors.New("collectedAt 数值超出可解释范围")
	}
	if v <= EpochSecondsMax {
		return time.Unix(v, 0).In(staging.Zone), nil
	}
	if v >= EpochMillisMin {
		return time.UnixMilli(v).In(staging.Zone), nil
	}
	// 秒上界与毫秒下界之间的空档（约 2100-01-01 ~ 2020-01-01 的毫秒表示），无解释。
	return time.Time{}, errors.New("collectedAt 数值超出可解释范围")
}

// isDigits 判断是否纯十进制数字串（不做范围判断）。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
