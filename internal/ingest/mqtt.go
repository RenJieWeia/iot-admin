package ingest

import (
	"context"
	"log/slog"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// IngestTopicPrefix 是 MQTT 接入主题前缀：iot/ingest/{gateway_id}。
const IngestTopicPrefix = "iot/ingest/"

// Subscribe 订阅 iot/ingest/+，每条 QoS 1 消息走与 HTTP 完全相同的业务管道。
// 回调与 HTTP handler 并发，靠 Processor 内部按终端分片的写锁串行化。
func Subscribe(client mqtt.Client, p *Processor, log *slog.Logger) error {
	tok := client.Subscribe(IngestTopicPrefix+"+", 1, func(_ mqtt.Client, m mqtt.Message) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		gatewayID := strings.TrimPrefix(m.Topic(), IngestTopicPrefix)
		res, err := p.Handle(ctx, gatewayID, m.Payload())
		if err != nil {
			log.Info("MQTT 接入整包被拒", "topic", m.Topic(), "err", err)
			return
		}
		for _, r := range res.Results {
			if !r.Accepted {
				log.Info("MQTT 终端未入库", "topic", m.Topic(), "terminal", r.TerminalID, "reason", r.Reason)
			}
		}
	})
	tok.Wait()
	return tok.Error()
}
