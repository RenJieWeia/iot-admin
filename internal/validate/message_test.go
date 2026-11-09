package validate_test

import (
	"encoding/json"
	"strings"
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
	if env.MessageID != "m-1" {
		t.Fatalf("消息 ID: 期望 m-1，实际 %q", env.MessageID)
	}
	if len(env.Terminals) != 2 {
		t.Fatalf("终端数: 期望 2，实际 %d", len(env.Terminals))
	}
	if env.Terminals[0].TerminalID != "T1" || env.Terminals[1].TerminalID != "T2" {
		t.Fatalf("terminalId 顺序不符: %q, %q", env.Terminals[0].TerminalID, env.Terminals[1].TerminalID)
	}
	wantItems := []int{2, 1} // T1 两条、T2 一条
	for i, tw := range env.Terminals {
		tp, terr, failures := validate.ParseTerminal(tw)
		if terr != nil {
			t.Fatalf("终端 %d (%s) 期望通过，实际 %v", i, tw.TerminalID, terr)
		}
		if len(failures) != 0 {
			t.Fatalf("终端 %d (%s) 不应有项级失败，实际 %v", i, tw.TerminalID, failures)
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

// 5) 未知字段（含历史 gatewayId）一律忽略，身份由传输层决定。
func TestParseEnvelopeIgnoresUnknownFields(t *testing.T) {
	env, err := validate.ParseEnvelope([]byte(`{"terminals":[{"terminalId":"T1","items":[]}]}`))
	if err != nil {
		t.Fatalf("缺 gatewayId 不应报错: %v", err)
	}
	if env.MessageID != "" {
		t.Fatalf("未声明 messageId 应为空，实际 %q", env.MessageID)
	}
	if _, err := validate.ParseEnvelope([]byte(`{"gatewayId":"GW2","terminals":[{"terminalId":"T1","items":[]}]}`)); err != nil {
		t.Fatalf("带 gatewayId 不应报错: %v", err)
	}
}

// 6) 只对 required=true 的字段报警：required=false 可不上报。
func TestMissingRequired(t *testing.T) {
	props := []validate.Property{
		{ItemCode: "temp", Required: true},
		{ItemCode: "hum", Required: true},
		{ItemCode: "note", Required: false},
		{ItemCode: "batt", Required: true},
	}
	got := validate.MissingRequired(props, []validate.RawItem{{Code: "temp"}, {Code: "note"}})
	if len(got) != 2 || got[0] != "hum" || got[1] != "batt" {
		t.Fatalf("缺失 code: 期望 [hum batt]，实际 %v", got)
	}
	// 只差 required=false 的 note：不算缺失。
	only := validate.MissingRequired(props, []validate.RawItem{{Code: "temp"}, {Code: "hum"}, {Code: "batt"}})
	if len(only) != 0 {
		t.Fatalf("required=false 的属性不应判缺失，实际 %v", only)
	}
	if all := validate.MissingRequired(props,
		[]validate.RawItem{{Code: "temp"}, {Code: "hum"}, {Code: "batt"}, {Code: "note"}}); all != nil {
		t.Fatalf("全覆盖时期望 nil，实际 %v", all)
	}
	// 未声明的 item 不参与"缺失"计算（由 clean 侧忽略并计 clean_failed）。
	if extra := validate.MissingRequired(props,
		[]validate.RawItem{{Code: "temp"}, {Code: "hum"}, {Code: "batt"}, {Code: "note"}, {Code: "bogus"}}); extra != nil {
		t.Fatalf("多余 item 不应影响缺失判定，实际 %v", extra)
	}
}

// 7)、9) 结构性拒收不变：code 重复 / 跨度 70 分钟 / code 为空。
// 这些是「没有扩大化」的回归护栏：改造后仍必须整终端拒回，且不得附带项级失败。
func TestParseTerminalRejects(t *testing.T) {
	cases := []struct {
		name       string
		tw         validate.TerminalWire
		wantReason string
		wantCode   string // 期望出现在 Codes 中；空表示不检查
	}{
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
		{
			name:       "code 为空",
			tw:         wire(t, "T1", `[{"code":"","value":1,"collectedAt":"`+atOK+`"}]`),
			wantReason: validate.ReasonMsgIncomplete,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, terr, failures := validate.ParseTerminal(c.tw)
			if terr == nil {
				t.Fatalf("期望 *TerminalError(%s)，实际 nil", c.wantReason)
			}
			if terr.Reason != c.wantReason {
				t.Fatalf("reason: 期望 %q，实际 %q (%v)", c.wantReason, terr.Reason, terr)
			}
			if len(failures) != 0 {
				t.Fatalf("结构性拒收不得同时返回项级失败，实际 %v", failures)
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

// 8) value=null / collectedAt 不可解析 → 项级失败（value_missing / time_unparseable），
// 不再整终端拒回；合法项照常进入 payload。
func TestParseTerminalItemFailures(t *testing.T) {
	tw := wire(t, "T1", `[
		{"code":"temp","value":null,"unit":"C","collectedAt":"`+atOK+`"},
		{"code":"hum","value":61,"unit":"%","collectedAt":"2026-11-05 10:00:00"},
		{"code":"power","value":1.0,"unit":"kW","collectedAt":"`+atOK+`"}
	]`)
	tp, terr, failures := validate.ParseTerminal(tw)
	if terr != nil {
		t.Fatalf("项级失败不应产生 *TerminalError，实际 %v", terr)
	}
	if len(tp.Items) != 1 || tp.Items[0].Code != "power" {
		t.Fatalf("合法项应保留 power 一条，实际 %+v", tp.Items)
	}
	want := map[string]string{
		"temp": validate.ItemValueMissing,
		"hum":  validate.ItemTimeUnparseable,
	}
	if len(failures) != len(want) {
		t.Fatalf("项级失败数 = %d，期望 %d：%v", len(failures), len(want), failures)
	}
	for _, f := range failures {
		if want[f.Code] != f.Reason {
			t.Fatalf("code %s 的失败原因 = %q，期望 %q", f.Code, f.Reason, want[f.Code])
		}
		if f.Detail == "" {
			t.Fatalf("code %s 的项级失败应带 detail", f.Code)
		}
	}
}

// 10) 跨度恰好 60 分钟 → 通过（边界）。
func TestParseTerminalSpanBoundary(t *testing.T) {
	tw := wire(t, "T1", `[
		{"code":"temp","value":25.4,"unit":"C","collectedAt":"`+atOK+`"},
		{"code":"hum","value":61,"unit":"%","collectedAt":"2026-11-05T11:00:00+08:00"}
	]`)
	tp, terr, _ := validate.ParseTerminal(tw)
	if terr != nil {
		t.Fatalf("跨度恰好 1h 期望通过，实际 %v", terr)
	}
	if len(tp.Items) != 2 || tp.TerminalID != "T1" {
		t.Fatalf("payload: 期望 T1/2 items，实际 %q/%d", tp.TerminalID, len(tp.Items))
	}
}

// 11) ParseCollectedAt 接受四种字符串格式与 R-029 的秒/毫秒时间戳（Instant 正确且统一到 Asia/Shanghai）。
func TestParseCollectedAtAccepts(t *testing.T) {
	base := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC) // == 10:00+08:00
	cases := []struct {
		in        string
		want      time.Time
		wantHour  int
		wantLocal bool // true 表示额外断言"小时等于 wantHour"（字符串格式专用）
	}{
		{in: "2026-11-05T10:00:00+08:00", want: base, wantLocal: true, wantHour: 10},
		{in: "2026-11-05T02:00:00Z", want: base, wantLocal: true, wantHour: 10},
		{in: "2026-11-05T10:00:00", want: base, wantLocal: true, wantHour: 10},
		{in: "2026-11-05T10:00:00.500+08:00", want: base.Add(500 * time.Millisecond), wantLocal: true, wantHour: 10},
		// R-029：纯数字按范围判定秒/毫秒，均按 UTC 解释后转 Asia/Shanghai。
		{in: "1700000000", want: time.Unix(1700000000, 0).In(staging.Zone)},
		{in: "1700000000123", want: time.UnixMilli(1700000000123).In(staging.Zone)},
		{in: "1577836800", want: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},    // 下界
		{in: "4102444800", want: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)},    // 上界
		{in: "1577836800000", want: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, // 毫秒下界
		{in: "4102444800000", want: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}, // 毫秒上界
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
			if got.Location().String() != staging.Zone.String() {
				t.Fatalf("期望统一到 %s，实际 %s", staging.Zone, got.Location())
			}
			if c.wantLocal && got.Hour() != c.wantHour {
				t.Fatalf("期望小时 %d，实际 %d", c.wantHour, got.Hour())
			}
		})
	}
}

// 12) ParseCollectedAt 拒绝：数值越界（早于/超出）、非数字形态、仅日期、空格分隔、空串。
// R-029 后 epoch 数字按范围判定，越界值分别给出"早于"与"超出"两类错误。
func TestParseCollectedAtRejects(t *testing.T) {
	cases := []struct {
		in      string
		wantSub string
	}{
		{"1", "早于"},                     // 纯数字但远早于 2020 下界
		{"1577836799", "早于"},            // 下界前 1 秒
		{"4102444801", "超出"},            // 秒上界后 1 秒
		{"5000000000", "超出"},            // 落在秒上界与毫秒下界之间的空档
		{"9e12", "不可解析"},                // 指数写法：不是纯数字
		{"2026-11-05", "不可解析"},          // 仅日期
		{"2026-11-05 10:00:00", "不可解析"}, // 空格分隔
		{"", "为空"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := validate.ParseCollectedAt(c.in)
			if err == nil {
				t.Fatalf("%q 期望失败，实际成功: %s", c.in, got)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("%q 的错误应含 %q，实际 %v", c.in, c.wantSub, err)
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
