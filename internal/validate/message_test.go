package validate_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

const atOK = "2026-11-05T10:00:00+08:00"

func wire(t *testing.T, terminalID, items string) validate.TerminalWire {
	t.Helper()
	return validate.TerminalWire{TerminalID: terminalID, Items: json.RawMessage(items)}
}

func wantEnvelopeErr(t *testing.T, err error, wantReason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 *EnvelopeError(%s)，实际 nil", wantReason)
	}
	ee, ok := err.(*validate.EnvelopeError)
	if !ok {
		t.Fatalf("错误类型: 期望 *validate.EnvelopeError，实际 %T (%v)", err, err)
	}
	if ee.Reason != wantReason {
		t.Fatalf("reason: 期望 %q，实际 %q (%v)", wantReason, ee.Reason, ee)
	}
}

// 1) 合法批量信封：ver 缺省=1，多终端。
func TestParseEnvelopeOK(t *testing.T) {
	payload := []byte(`{
		"messageId": "m-1",
		"gatewayId": "GW1",
		"terminals": [
			{"terminalId": "T1", "items": [
				{"code": "temp", "value": 25.4, "unit": "C", "collectedAt": "2026-11-05T10:00:00+08:00"},
				{"code": "hum",  "value": 61,   "unit": "%", "collectedAt": "2026-11-05T10:00:30+08:00"}
			]},
			{"terminalId": "T2", "items": [
				{"code": "temp", "value": 21.1, "unit": "C", "collectedAt": "2026-11-05T10:00:00Z"}
			]}
		]
	}`)
	env, err := validate.ParseEnvelope(payload)
	if err != nil {
		t.Fatalf("ParseEnvelope: 期望成功，实际 %v", err)
	}
	if env.Ver != 1 {
		t.Fatalf("Ver: 期望缺省 1，实际 %d", env.Ver)
	}
	if env.GatewayID != "GW1" || env.MessageID != "m-1" {
		t.Fatalf("网关/消息: 期望 GW1/m-1，实际 %q/%q", env.GatewayID, env.MessageID)
	}
	if len(env.Terminals) != 2 {
		t.Fatalf("终端数: 期望 2，实际 %d", len(env.Terminals))
	}
	if env.Terminals[0].TerminalID != "T1" || env.Terminals[1].TerminalID != "T2" {
		t.Fatalf("terminalId 顺序不符: %q, %q", env.Terminals[0].TerminalID, env.Terminals[1].TerminalID)
	}
	wantItems := []int{2, 1} // T1 两条、T2 一条
	for i, tw := range env.Terminals {
		tp, terr := validate.ParseTerminal(tw)
		if terr != nil {
			t.Fatalf("终端 %d (%s) 期望通过，实际 %v", i, tw.TerminalID, terr)
		}
		if want := wantItems[i]; len(tp.Items) != want {
			t.Fatalf("终端 %s items: 期望 %d，实际 %d", tw.TerminalID, want, len(tp.Items))
		}
		if tp.Items[0].ParsedTime.IsZero() {
			t.Fatalf("终端 %s 的 ParsedTime 未回填", tw.TerminalID)
		}
	}
}

// 2) ver=2 → unsupported_version。
func TestParseEnvelopeUnsupportedVersion(t *testing.T) {
	_, err := validate.ParseEnvelope([]byte(`{"ver":2,"gatewayId":"GW1","terminals":[{"terminalId":"T1","items":[]}]}`))
	wantEnvelopeErr(t, err, validate.ReasonUnsupportedVersion)
}

// 3) terminals 不是数组 / 为空 / null → terminals_invalid。
func TestParseEnvelopeTerminalsInvalid(t *testing.T) {
	cases := map[string]string{
		"对象不是数组": `{"gatewayId":"GW1","terminals":{"terminalId":"T1","items":[]}}`,
		"空数组":    `{"gatewayId":"GW1","terminals":[]}`,
		"null":   `{"gatewayId":"GW1","terminals":null}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := validate.ParseEnvelope([]byte(payload))
			wantEnvelopeErr(t, err, validate.ReasonTerminalsInvalid)
		})
	}
}

// 4) 非法 JSON → invalid_json。
func TestParseEnvelopeInvalidJSON(t *testing.T) {
	_, err := validate.ParseEnvelope([]byte(`{"gatewayId":"GW1",`))
	wantEnvelopeErr(t, err, validate.ReasonInvalidJSON)
}

// 5) 缺 gatewayId（含纯空白）→ gateway_id_missing。
func TestParseEnvelopeGatewayMissing(t *testing.T) {
	for _, payload := range []string{
		`{"terminals":[{"terminalId":"T1","items":[]}]}`,
		`{"gatewayId":"   ","terminals":[{"terminalId":"T1","items":[]}]}`,
	} {
		_, err := validate.ParseEnvelope([]byte(payload))
		wantEnvelopeErr(t, err, validate.ReasonGatewayMissing)
	}
}

// 6) 单终端缺 required → MissingRequired 返回缺失 code（只含 required 的）。
func TestMissingRequired(t *testing.T) {
	props := []validate.Property{
		{ItemCode: "temp", Required: true},
		{ItemCode: "hum", Required: true},
		{ItemCode: "note", Required: false},
		{ItemCode: "batt", Required: true},
	}
	items := []validate.RawItem{{Code: "temp"}, {Code: "note"}}
	got := validate.MissingRequired(props, items)
	if len(got) != 2 || got[0] != "hum" || got[1] != "batt" {
		t.Fatalf("缺失 code: 期望 [hum batt]，实际 %v", got)
	}
	if all := validate.MissingRequired(props, []validate.RawItem{{Code: "temp"}, {Code: "hum"}, {Code: "batt"}}); all != nil {
		t.Fatalf("全覆盖时期望 nil，实际 %v", all)
	}
}

// 7)、8)、9) 单终端拒收：value=null / code 重复 / 跨度 70 分钟。
func TestParseTerminalRejects(t *testing.T) {
	cases := []struct {
		name       string
		tw         validate.TerminalWire
		wantReason string
		wantCode   string // 期望出现在 Codes 中；空表示不检查
	}{
		{
			name:       "value 为 null",
			tw:         wire(t, "T1", `[{"code":"temp","value":null,"collectedAt":"`+atOK+`"}]`),
			wantReason: validate.ReasonMsgIncomplete,
			wantCode:   "temp",
		},
		{
			name: "code 重复",
			tw: wire(t, "T1", `[
				{"code":"temp","value":25.4,"unit":"C","collectedAt":"`+atOK+`"},
				{"code":"temp","value":25.5,"unit":"C","collectedAt":"2026-11-05T10:00:10+08:00"}
			]`),
			wantReason: validate.ReasonMsgIncomplete,
			wantCode:   "temp",
		},
		{
			name: "跨度 70 分钟",
			tw: wire(t, "T1", `[
				{"code":"temp","value":25.4,"unit":"C","collectedAt":"`+atOK+`"},
				{"code":"hum","value":61,"unit":"%","collectedAt":"2026-11-05T11:10:00+08:00"}
			]`),
			wantReason: validate.ReasonProtocolError,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, terr := validate.ParseTerminal(c.tw)
			if terr == nil {
				t.Fatalf("期望 *TerminalError(%s)，实际 nil", c.wantReason)
			}
			if terr.Reason != c.wantReason {
				t.Fatalf("reason: 期望 %q，实际 %q (%v)", c.wantReason, terr.Reason, terr)
			}
			if c.wantCode == "" {
				return
			}
			found := false
			for _, cd := range terr.Codes {
				if cd == c.wantCode {
					found = true
				}
			}
			if !found {
				t.Fatalf("Codes 应含 %q，实际 %v", c.wantCode, terr.Codes)
			}
		})
	}
}

// 10) 跨度恰好 60 分钟 → 通过（边界）。
func TestParseTerminalSpanBoundary(t *testing.T) {
	tw := wire(t, "T1", `[
		{"code":"temp","value":25.4,"unit":"C","collectedAt":"`+atOK+`"},
		{"code":"hum","value":61,"unit":"%","collectedAt":"2026-11-05T11:00:00+08:00"}
	]`)
	tp, terr := validate.ParseTerminal(tw)
	if terr != nil {
		t.Fatalf("跨度恰好 1h 期望通过，实际 %v", terr)
	}
	if len(tp.Items) != 2 || tp.TerminalID != "T1" {
		t.Fatalf("payload: 期望 T1/2 items，实际 %q/%d", tp.TerminalID, len(tp.Items))
	}
}

// 11) ParseCollectedAt 接受四种格式且 Instant 正确（统一到 Asia/Shanghai）。
func TestParseCollectedAtAccepts(t *testing.T) {
	base := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC) // == 10:00+08:00
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2026-11-05T10:00:00+08:00", base},
		{"2026-11-05T02:00:00Z", base},
		{"2026-11-05T10:00:00", base},
		{"2026-11-05T10:00:00.500+08:00", base.Add(500 * time.Millisecond)},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := validate.ParseCollectedAt(c.in)
			if err != nil {
				t.Fatalf("期望成功，实际 %v", err)
			}
			if !got.Equal(c.want) {
				t.Fatalf("Instant: 期望 %s，实际 %s", c.want, got.UTC())
			}
			if got.Location().String() != staging.Zone.String() || got.Hour() != 10 {
				t.Fatalf("期望统一到 %s 且小时为 10，实际 %s/%d", staging.Zone, got.Location(), got.Hour())
			}
		})
	}
}

// 12) ParseCollectedAt 拒绝 epoch 数字 / 仅日期 / 空格分隔 / 空串。
func TestParseCollectedAtRejects(t *testing.T) {
	for _, in := range []string{"1700000000", "2026-11-05", "2026-11-05 10:00:00", ""} {
		t.Run(in, func(t *testing.T) {
			if got, err := validate.ParseCollectedAt(in); err == nil {
				t.Fatalf("%q 期望失败，实际成功: %s", in, got)
			}
		})
	}
}

// 13) FindFuture：now+6m 命中、now+4m 不命中、边界 now+5m 不命中。
func TestFindFuture(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, staging.Zone)
	item := func(code string, at time.Time) validate.RawItem {
		return validate.RawItem{Code: code, ParsedTime: at}
	}
	if got, ok := validate.FindFuture([]validate.RawItem{item("a", now.Add(time.Minute)), item("b", now.Add(6*time.Minute))}, now); !ok || got.Code != "b" {
		t.Fatalf("now+6m: 期望命中 b，实际 ok=%v got=%q", ok, got.Code)
	}
	if got, ok := validate.FindFuture([]validate.RawItem{item("a", now.Add(4*time.Minute))}, now); ok {
		t.Fatalf("now+4m: 期望不命中，实际命中 %q", got.Code)
	}
	if got, ok := validate.FindFuture([]validate.RawItem{item("a", now.Add(validate.MaxClockSkew))}, now); ok {
		t.Fatalf("边界 now+5m: 期望不命中，实际命中 %q", got.Code)
	}
}
