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
	ReasonGatewayMissing     = "gateway_id_missing"
	ReasonBodyTooLarge       = "body_too_large"
)

// 逐终端拒收的 reason。
const (
	ReasonMsgIncomplete   = "msg_incomplete"
	ReasonProtocolError   = "protocol_error"
	ReasonCleanFailed     = "clean_failed"
	ReasonTerminalUnknown = "terminal_unknown"
)

// MaxMissingCodes 结果中回传的缺失/非法 code 上限。
const MaxMissingCodes = 10

// MaxSpanPerTerminal 单终端内 items 的采集时间允许跨度。
const MaxSpanPerTerminal = time.Hour

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
	GatewayID string
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
	HasOffset  bool      `json:"-"` // 是否显式带时区偏移，仅用于统计
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
	Terminals json.RawMessage `json:"terminals"`
}

// ParseEnvelope 只做整包级校验（R1/R13）：JSON 可解析、ver 受支持、gatewayId 非空、
// terminals 为数组且非空。任何一条不满足都返回 *EnvelopeError，调用方按 400 处理。
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
	if strings.TrimSpace(w.GatewayID) == "" {
		return nil, &EnvelopeError{Reason: ReasonGatewayMissing}
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
	return &Envelope{Ver: ver, MessageID: w.MessageID, GatewayID: w.GatewayID, Terminals: terminals}, nil
}

// ParseTerminal 做单终端校验（R2/R3/R4/R7/R8）：terminalId 非空、items 非空、
// code 非空、value 非 null、collectedAt 可解析、code 不重复、采集时间跨度不超 1 小时。
// 失败返回 *TerminalError，调用方只拒收该终端。
func ParseTerminal(tw TerminalWire) (TerminalPayload, *TerminalError) {
	tp := TerminalPayload{TerminalID: tw.TerminalID}
	if strings.TrimSpace(tw.TerminalID) == "" {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "缺少 terminalId"}
	}
	if len(tw.Items) == 0 || string(tw.Items) == "null" {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "items 缺失"}
	}
	var items []RawItem
	if err := json.Unmarshal(tw.Items, &items); err != nil {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "items 不是数组: " + err.Error()}
	}
	if len(items) == 0 {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "items 为空"}
	}

	seen := make(map[string]struct{}, len(items))
	var codes []string
	var minAt, maxAt time.Time
	for i := range items {
		it := &items[i]
		if strings.TrimSpace(it.Code) == "" {
			codes = append(codes, fmt.Sprintf("#%d(code 为空)", i))
			continue
		}
		if len(it.Value) == 0 || string(it.Value) == "null" {
			codes = append(codes, it.Code)
			continue
		}
		t, hasOffset, err := ParseCollectedAt(it.CollectedAt)
		if err != nil {
			codes = append(codes, it.Code)
			continue
		}
		if _, dup := seen[it.Code]; dup {
			return tp, &TerminalError{
				Reason: ReasonMsgIncomplete, Detail: "code 重复: " + it.Code, Codes: []string{it.Code},
			}
		}
		seen[it.Code] = struct{}{}
		it.ParsedTime = t
		it.HasOffset = hasOffset
		if minAt.IsZero() || t.Before(minAt) {
			minAt = t
		}
		if maxAt.IsZero() || t.After(maxAt) {
			maxAt = t
		}
	}
	if len(codes) > 0 {
		return tp, &TerminalError{Reason: ReasonMsgIncomplete, Detail: "value 缺失或 collectedAt 非法", Codes: codes}
	}
	if maxAt.Sub(minAt) > MaxSpanPerTerminal {
		return tp, &TerminalError{
			Reason: ReasonProtocolError,
			Detail: fmt.Sprintf("单终端采集时间跨度 %s 超过 1 小时", maxAt.Sub(minAt)),
		}
	}
	tp.Items = items
	return tp, nil
}

// ParseCollectedAt 解析采集时间（R4）：
//   - 接受 +08:00 偏移、Z（换算到 UTC+8）、省略偏移（按 UTC+8 解释）、毫秒精度；
//   - 不接受 epoch 数字、仅日期、空格分隔、空串；
//   - 返回值统一转 Asia/Shanghai；hasOffset 仅用于统计，不作为拒收依据。
func ParseCollectedAt(s string) (t time.Time, hasOffset bool, err error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, false, errors.New("collectedAt 为空")
	}
	if isDigits(s) {
		return time.Time{}, false, errors.New("collectedAt 不接受 epoch 数字")
	}
	if parsed, perr := time.Parse(time.RFC3339Nano, s); perr == nil {
		return parsed.In(staging.Zone), true, nil
	}
	// 省略偏移：按 Asia/Shanghai 解释。
	if parsed, perr := time.ParseInLocation("2006-01-02T15:04:05.999999999", s, staging.Zone); perr == nil {
		return parsed, false, nil
	}
	return time.Time{}, false, fmt.Errorf("collectedAt 不可解析: %q", s)
}

func isDigits(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}
