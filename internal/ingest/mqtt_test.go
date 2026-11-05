package ingest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// 验收：HTTP 与 MQTT 两路同源数据，落库结果与发布内容完全一致。
// 需要真实 Broker（BroMQ），通过 MQTT_BROKER_URL 注入；未注入则跳过。
func TestHTTPAndMQTTPathsProduceIdenticalResults(t *testing.T) {
	brokerURL := os.Getenv("MQTT_BROKER_URL")
	if brokerURL == "" {
		t.Skip("未设置 MQTT_BROKER_URL，跳过需要 MQTT Broker 的用例")
	}
	e := newEnv(t)
	ctx := context.Background()
	hour := hourOf(baseTime)

	opts := mqtt.NewClientOptions().AddBroker(brokerURL).
		SetClientID(fmt.Sprintf("iot-test-%d", time.Now().UnixNano())).
		SetAutoReconnect(false).SetConnectTimeout(5 * time.Second)
	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		t.Fatalf("连接 Broker 失败: %v", tok.Error())
	}
	defer client.Disconnect(100)

	realtime := make(chan publish.Payload, 4)
	subTok := client.Subscribe("iot/realtime/#", 1, func(_ mqtt.Client, m mqtt.Message) {
		var p publish.Payload
		if err := json.Unmarshal(m.Payload(), &p); err == nil {
			realtime <- p
		}
	})
	subTok.Wait()
	if err := subTok.Error(); err != nil {
		t.Fatalf("订阅实时主题失败: %v", err)
	}

	// MQTT 通道：用真实发布器 + 订阅回调处理终端 T2。
	mqttProc := &ingest.Processor{
		DB:    e.pool,
		Reg:   validate.NewRegistry(e.pool),
		Pub:   publish.NewMQTT(client),
		Exc:   exception.New(e.pool, e.clock.Now, quietLog),
		Locks: store.NewShardedLock(16),
		Now:   e.clock.Now,
		Log:   quietLog,
	}
	if err := ingest.Subscribe(client, mqttProc, quietLog); err != nil {
		t.Fatalf("订阅接入主题失败: %v", err)
	}

	at := baseTime.Add(20 * time.Minute)
	payload := build(testTerminalAlt, testGatewayAlt,
		item{Code: "temp", Value: 26.5, Unit: "C", Time: at},
		item{Code: "power", Value: 3.25, Unit: "kW", Time: at})
	pubTok := client.Publish(ingest.IngestTopicPrefix+testGatewayAlt, 1, false, payload)
	pubTok.Wait()
	if err := pubTok.Error(); err != nil {
		t.Fatalf("发布接入消息失败: %v", err)
	}

	// 等待 MQTT 通道落库 + 实时发布。
	waitFor(t, func() bool {
		var n int
		_ = e.pool.QueryRow(ctx, `select count(*) from latest_value where terminal_id = $1`, testTerminalAlt).Scan(&n)
		return n == 2
	}, 5*time.Second, "MQTT 通道落库")

	var got publish.Payload
	select {
	case got = <-realtime:
	case <-time.After(5 * time.Second):
		t.Fatal("未收到实时主题报文")
	}
	if got.TerminalID != testTerminalAlt {
		t.Fatalf("实时报文 terminalId = %q", got.TerminalID)
	}

	// HTTP 通道：同一份数据经处理器（假发布器）落到终端 T1。
	httpPayload := build("", "", item{Code: "temp", Value: 26.5, Unit: "C", Time: at},
		item{Code: "power", Value: 3.25, Unit: "kW", Time: at})
	if res := e.handle(httpPayload); !res.Accepted || !res.Published {
		t.Fatalf("HTTP 通道处理失败: %+v", res)
	}

	// 两路落库结果一致。
	for _, code := range []string{"temp", "power"} {
		var numA, numB float64
		var timeA, timeB time.Time
		if err := e.pool.QueryRow(ctx, `select value_num, value_time from latest_value where terminal_id = $1 and item_code = $2`,
			testTerminalAlt, code).Scan(&numA, &timeA); err != nil {
			t.Fatalf("读取 MQTT 落库值: %v", err)
		}
		if err := e.pool.QueryRow(ctx, `select value_num, value_time from latest_value where terminal_id = $1 and item_code = $2`,
			testTerminalID, code).Scan(&numB, &timeB); err != nil {
			t.Fatalf("读取 HTTP 落库值: %v", err)
		}
		if numA != numB || !timeA.Equal(timeB) {
			t.Fatalf("两路落库不一致 %s: mqtt=%v@%s http=%v@%s", code, numA, timeA, numB, timeB)
		}
	}

	// 发布内容与落库一致。
	for _, it := range got.Items {
		var num float64
		var vt time.Time
		if err := e.pool.QueryRow(ctx, `select value_num, value_time from latest_value where terminal_id = $1 and item_code = $2`,
			testTerminalAlt, it.Code).Scan(&num, &vt); err != nil {
			t.Fatalf("读取实时项对应落库值: %v", err)
		}
		if it.Value.(float64) != num || it.Time != vt.Format(time.RFC3339Nano) {
			t.Fatalf("实时报文与落库不一致 %s: publish=%v@%s db=%v@%s", it.Code, it.Value, it.Time, num, vt)
		}
	}

	// 两路快照一致。
	e.clock.Set(closeOf(hour).Add(time.Second))
	if _, err := e.settler.Run(ctx); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	var snapM, snapH float64
	if err := e.pool.QueryRow(ctx, `select value_num from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalAlt, hour).Scan(&snapM); err != nil {
		t.Fatalf("MQTT 侧快照缺失: %v", err)
	}
	if err := e.pool.QueryRow(ctx, `select value_num from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&snapH); err != nil {
		t.Fatalf("HTTP 侧快照缺失: %v", err)
	}
	if snapM != snapH {
		t.Fatalf("两路快照不一致: mqtt=%v http=%v", snapM, snapH)
	}
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}
