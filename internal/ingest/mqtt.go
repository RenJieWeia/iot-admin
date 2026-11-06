package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// IngestTopicPrefix 是 MQTT 接入主题前缀：iot/ingest/{gateway_id}。
const IngestTopicPrefix = "iot/ingest/"

// grantedFailure 是 SUBACK 里表示订阅失败的下界：0x80 起都是错误码（paho 的 Result() 原样给出）。
const grantedFailure byte = 0x80

// Subscribe 订阅 iot/ingest/+，每条 QoS 1 消息走与 HTTP 完全相同的业务管道。
// 回调与 HTTP handler 并发，靠 Processor 内部按终端分片的写锁串行化。
//
// 订阅结果必须显式校验：paho v1.5.1 的 Token.Error() 对 SUBACK 0x80（订阅被拒）返回 nil，
// 被拒信息只在 SubscribeToken.Result() 里。不校验就会出现"日志写 MQTT 已连接、之后零数据零报错"。
func Subscribe(client mqtt.Client, p *Processor, log *slog.Logger) error {
	filter := IngestTopicPrefix + "+"
	tok := client.Subscribe(filter, 1, func(_ mqtt.Client, m mqtt.Message) {
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
	if err := tok.Error(); err != nil {
		return err
	}
	granted := grantedOf(tok)
	if err := CheckGranted(granted); err != nil {
		return err
	}
	log.Info("MQTT 订阅成功", "topic", filter, "granted", granted)
	return nil
}

// grantedOf 取 SUBACK 的 granted QoS；不是订阅 token 或未回填时返回 nil（按通过处理）。
func grantedOf(tok mqtt.Token) map[string]byte {
	sub, ok := tok.(*mqtt.SubscribeToken)
	if !ok {
		return nil
	}
	return sub.Result()
}

// CheckGranted 校验 SUBACK 的 granted 值：任一项 >= 0x80 即订阅失败，返回带主题与取值的错误。
// nil 或空 map 视为通过——paho 在部分路径下不回填 Result。
//
// 导出是为了让 package ingest_test（既有 mqtt_test.go 的包）能零依赖地表驱动测试；
// 它是 internal 包内的纯函数，不对外暴露。
func CheckGranted(filters map[string]byte) error {
	for topic, qos := range filters {
		if qos >= grantedFailure {
			return fmt.Errorf("订阅被拒绝: topic=%q granted=0x%02X（%d），请检查 Broker ACL 与配置", topic, qos, qos)
		}
	}
	return nil
}
