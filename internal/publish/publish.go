// Package publish 是实时主题的唯一出口：QoS 1、retain=false。
package publish

import (
	"encoding/json"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Publisher 抽象发布动作，测试用假实现记录发布的报文。
type Publisher interface {
	Publish(topic string, payload []byte) error
}

// Topic 生成实时主题 iot/realtime/{domain}/{type}/{terminal}。
func Topic(domainCode, typeCode, terminalID string) string {
	return fmt.Sprintf("iot/realtime/%s/%s/%s", domainCode, typeCode, terminalID)
}

// Item 是实时报文中的数据项。
type Item struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
	Unit  string `json:"unit"`
	Time  string `json:"time"`
}

// Payload 是一条完整消息清洗后的一条实时报文。
type Payload struct {
	TerminalID string `json:"terminalId"`
	TS         string `json:"ts"`
	Items      []Item `json:"items"`
}

// Build 组装实时报文（ts 为平台发布时刻）。
func Build(terminalID string, ts time.Time, items []Item) ([]byte, error) {
	return json.Marshal(Payload{TerminalID: terminalID, TS: ts.Format(time.RFC3339Nano), Items: items})
}

// MQTT 用 paho 客户端发布。
type MQTT struct {
	client mqtt.Client
}

func NewMQTT(client mqtt.Client) *MQTT { return &MQTT{client: client} }

func (m *MQTT) Publish(topic string, payload []byte) error {
	tok := m.client.Publish(topic, 1, false, payload)
	tok.Wait()
	return tok.Error()
}

// Discard 用于未配置 MQTT 的场景（仅记录日志，不阻断落库）。
type Discard struct{}

func (Discard) Publish(string, []byte) error { return nil }
