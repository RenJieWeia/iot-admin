package ingest

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// GatewayStatusSuffix 是网关状态主题后缀：iot/gateway/{gateway_id}/status。
const (
	GatewayStatusPrefix = "iot/gateway/"
	GatewayStatusSuffix = "/status"
)

// GatewayStatusPayload 是 Broker 脚本/LWT 发布的状态载荷。
type GatewayStatusPayload struct {
	Online    bool   `json:"online"`
	GatewayID string `json:"gateway_id"`
	ClientID  string `json:"client_id"`
	Error     string `json:"error"`
}

// SetGatewayOnline 是状态消费方需要实现的最小接口（由 admin.API 提供）。
type SetGatewayOnline func(ctx context.Context, gatewayID string, online bool) error

// SubscribeGatewayStatus 订阅 iot/gateway/+/status，把上下线事件交给 onStatus。
// 订阅被拒（ACL 缺失）会显式报错，避免"连上了但永远收不到状态"。
func SubscribeGatewayStatus(client mqtt.Client, onStatus SetGatewayOnline, log *slog.Logger) error {
	filter := GatewayStatusPrefix + "+" + GatewayStatusSuffix
	tok := client.Subscribe(filter, 1, func(_ mqtt.Client, m mqtt.Message) {
		gatewayID, ok := parseGatewayStatusTopic(m.Topic())
		if !ok {
			return
		}
		var payload GatewayStatusPayload
		if err := json.Unmarshal(m.Payload(), &payload); err != nil {
			log.Warn("网关状态载荷解析失败", "topic", m.Topic(), "err", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := onStatus(ctx, gatewayID, payload.Online); err != nil {
			log.Warn("更新网关在线状态失败", "gateway", gatewayID, "online", payload.Online, "err", err)
			return
		}
		log.Info("网关状态更新", "gateway", gatewayID, "online", payload.Online,
			"client", payload.ClientID, "error", payload.Error)
	})
	tok.Wait()
	if err := tok.Error(); err != nil {
		return err
	}
	if err := CheckGranted(grantedOf(tok)); err != nil {
		return err
	}
	log.Info("MQTT 订阅成功", "topic", filter)
	return nil
}

// parseGatewayStatusTopic 从 iot/gateway/{id}/status 提取 gateway_id。
func parseGatewayStatusTopic(topic string) (string, bool) {
	rest, ok := strings.CutPrefix(topic, GatewayStatusPrefix)
	if !ok || !strings.HasSuffix(rest, GatewayStatusSuffix) {
		return "", false
	}
	id := strings.TrimSuffix(rest, GatewayStatusSuffix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}
