// Package validate 负责完整消息的解析校验与物模型加载。
package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrIncomplete 是"整条拒收 + msg_incomplete"的唯一判据。
var ErrIncomplete = errors.New("msg_incomplete")

// Message 是上报消息（HTTP body 与 MQTT payload 同构）。
type Message struct {
	MessageID  string    `json:"messageId"`
	GatewayID  string    `json:"gatewayId"`
	TerminalID string    `json:"terminalId"`
	Items      []RawItem `json:"items"`
}

// RawItem 保持 value 的原始 JSON，等拿到物模型声明再按类型解析。
type RawItem struct {
	Code  string          `json:"code"`
	Value json.RawMessage `json:"value"`
	Unit  string          `json:"unit"`
	Time  string          `json:"time"`

	ParsedTime time.Time `json:"-"`
}

// ParseMessage 做结构级校验：JSON 可解析、必备字段齐备、每项 time 合法。失败即 ErrIncomplete。
func ParseMessage(payload []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, fmt.Errorf("%w: 报文不是合法 JSON: %v", ErrIncomplete, err)
	}
	if m.GatewayID == "" {
		return nil, fmt.Errorf("%w: 缺少 gatewayId", ErrIncomplete)
	}
	if m.TerminalID == "" {
		return nil, fmt.Errorf("%w: 缺少 terminalId", ErrIncomplete)
	}
	if len(m.Items) == 0 {
		return nil, fmt.Errorf("%w: items 为空", ErrIncomplete)
	}
	for i := range m.Items {
		it := &m.Items[i]
		if it.Code == "" {
			return nil, fmt.Errorf("%w: items[%d] 缺少 code", ErrIncomplete, i)
		}
		if len(it.Value) == 0 || string(it.Value) == "null" {
			return nil, fmt.Errorf("%w: items[%d](%s) 缺少 value", ErrIncomplete, i, it.Code)
		}
		if it.Time == "" {
			return nil, fmt.Errorf("%w: items[%d](%s) 缺少 time", ErrIncomplete, i, it.Code)
		}
		t, err := time.Parse(time.RFC3339Nano, it.Time)
		if err != nil {
			return nil, fmt.Errorf("%w: items[%d](%s) time 不可解析: %v", ErrIncomplete, i, it.Code, err)
		}
		it.ParsedTime = t
	}
	m.Items = dedupe(m.Items)
	return &m, nil
}

// dedupe 同一 code 重复出现时保留最后一条（不做幂等，只是结构去重）。
func dedupe(items []RawItem) []RawItem {
	last := make(map[string]int, len(items))
	out := make([]RawItem, 0, len(items))
	for _, it := range items {
		if prev, ok := last[it.Code]; ok {
			out[prev] = it
			continue
		}
		last[it.Code] = len(out)
		out = append(out, it)
	}
	return out
}
