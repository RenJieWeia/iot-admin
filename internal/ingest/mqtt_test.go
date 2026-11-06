package ingest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/store"
)

// 用例 30：HTTP 与 MQTT 两路同源数据 → 落库结果与发布内容完全一致。
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

	mqttProc := &ingest.Processor{
		DB:    e.pool,
		Meta:  meta.NewStore(meta.NewPGLoader(e.pool), meta.DefaultTTL, nil),
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
	payload := batch(testGatewayAlt, termOf(testTerminalAlt,
		it("temp", 26.5, "C", at), it("power", 3.25, "kW", at)))
	pubTok := client.Publish(ingest.IngestTopicPrefix+testGatewayAlt, 1, false, payload)
	pubTok.Wait()
	if err := pubTok.Error(); err != nil {
		t.Fatalf("发布接入消息失败: %v", err)
	}

	waitFor(t, func() bool {
		return e.count2(`select count(*) from latest_value where terminal_id = $1`, testTerminalAlt) == 2
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

	// HTTP 通道：同一份数据经处理器落到 T1
	httpPayload := batch(testGatewayID, termOf(testTerminalID,
		it("temp", 26.5, "C", at), it("power", 3.25, "kW", at)))
	res, err := e.proc.Handle(ctx, testGatewayID, httpPayload)
	if err != nil {
		t.Fatalf("HTTP 通道处理失败: %v", err)
	}
	if r := resultOf(t, res, testTerminalID); !r.Accepted || !r.Published {
		t.Fatalf("HTTP 通道未入库: %+v", r)
	}

	for _, code := range []string{"temp", "power"} {
		numA, _, _, timeA, okA := e.latestOf(testTerminalAlt, code)
		numB, _, _, timeB, okB := e.latestOf(testTerminalID, code)
		if !okA || !okB {
			t.Fatalf("两路落库缺失 %s", code)
		}
		if *numA != *numB || !timeA.Equal(timeB) {
			t.Fatalf("两路落库不一致 %s: mqtt=%v@%s http=%v@%s", code, *numA, timeA, *numB, timeB)
		}
	}
	for _, item := range got.Items {
		num, _, _, vt, ok := e.latestOf(testTerminalAlt, item.Code)
		if !ok {
			t.Fatalf("实时项未落库: %s", item.Code)
		}
		if item.Value.(float64) != *num || item.CollectedAt != vt.Format(time.RFC3339Nano) {
			t.Fatalf("实时报文与落库不一致 %s: publish=%v@%s db=%v@%s",
				item.Code, item.Value, item.CollectedAt, *num, vt)
		}
	}

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

// 订阅结果校验：SUBACK 的 granted 值决定启动成败（R 级：连上了但零数据零报错是事故形态）。
// paho v1.5.1 的 Token.Error() 对被拒订阅返回 nil，只能靠 Result() 判定。
func TestCheckGranted(t *testing.T) {
	cases := []struct {
		name    string
		granted map[string]byte
		wantErr bool
	}{
		{"granted QoS0", map[string]byte{"iot/ingest/+": 0}, false},
		{"granted QoS1", map[string]byte{"iot/ingest/+": 1}, false},
		{"granted QoS2", map[string]byte{"iot/ingest/+": 2}, false},
		{"各主题全部成功", map[string]byte{"iot/ingest/+": 1, "iot/realtime/#": 0}, false},
		{"0x80 订阅被拒", map[string]byte{"iot/ingest/+": 0x80}, true},
		{"0x87 非法 QoS", map[string]byte{"iot/ingest/+": 0x87}, true},
		{"多主题里任一项被拒", map[string]byte{"iot/ingest/+": 1, "iot/realtime/#": 0x80}, true},
		{"nil map 视为通过", nil, false},
		{"空 map 视为通过", map[string]byte{}, false},
	}
	for _, c := range cases {
		err := ingest.CheckGranted(c.granted)
		if c.wantErr && err == nil {
			t.Fatalf("%s: 期望返回错误，实际 nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Fatalf("%s: 期望通过，实际 %v", c.name, err)
		}
	}

	// 错误信息必须带主题与取值（便于一眼看出是 ACL 还是配置问题），且不得包含口令。
	err := ingest.CheckGranted(map[string]byte{"iot/ingest/+": 0x80})
	if err == nil {
		t.Fatal("0x80 必须报错")
	}
	msg := err.Error()
	for _, want := range []string{"iot/ingest/+", "128", "0x80"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息缺少 %q: %s", want, msg)
		}
	}
}
