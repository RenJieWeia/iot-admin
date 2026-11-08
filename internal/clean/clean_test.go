package clean_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

const atOK = "2026-11-05T10:00:00+08:00"

func f64(v float64) *float64 { return &v }
func intp(v int) *int        { return &v }

func rawItem(t *testing.T, code, valueJSON, unit, at string) validate.RawItem {
	t.Helper()
	parsed, err := validate.ParseCollectedAt(at)
	if err != nil {
		t.Fatalf("ParseCollectedAt(%q): %v", at, err)
	}
	return validate.RawItem{
		Code:        code,
		Value:       json.RawMessage(valueJSON),
		Unit:        unit,
		CollectedAt: at,
		ParsedTime:  parsed,
	}
}

func approx(t *testing.T, label string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: 期望 %v，实际 %v（容差 %v）", label, want, got, tol)
	}
}

// 14) Convert 闭集内四组换算数值正确。
func TestConvertInClosedSet(t *testing.T) {
	cases := []struct {
		v    float64
		from string
		to   string
		want float64
	}{
		{100, "C", "F", 212},
		{212, "F", "C", 100},
		{1.5, "kW", "W", 1500},
		{1500, "W", "kW", 1.5},
		{1.5, "kPa", "Pa", 1500},
		{1.5, "kWh", "Wh", 1500},
		{1500, "Wh", "kWh", 1.5},
	}
	for _, c := range cases {
		t.Run(c.from+"->"+c.to, func(t *testing.T) {
			got, ok := clean.Convert(c.v, c.from, c.to)
			if !ok {
				t.Fatalf("%v %s->%s: 期望 ok=true，实际 false", c.v, c.from, c.to)
			}
			approx(t, c.from+"->"+c.to, got, c.want, 1e-9)
		})
	}
}

// 15) 闭集外单位一律 ok=false（含同单位、量纲不匹配、目标不在闭集）。
func TestConvertOutsideClosedSet(t *testing.T) {
	cases := [][3]string{
		{"bar", "Pa", "bar→Pa"},
		{"Pa", "bar", "Pa→bar"},
		{"hp", "W", "hp→W"},
		{"W", "hp", "W→hp"},
		{"mm", "Pa", "mm→Pa"},
		{"K", "C", "K→C"},
		{"C", "K", "C→K"},
		{"K", "K", "K→K"},
		{"kW", "kPa", "kW→kPa（量纲不匹配）"},
	}
	for _, c := range cases {
		t.Run(c[2], func(t *testing.T) {
			got, ok := clean.Convert(7, c[0], c[1])
			if ok {
				t.Fatalf("%s: 期望 ok=false，实际 true（值 %v）", c[2], got)
			}
			if got != 7 {
				t.Fatalf("%s: 失败时应透传原值 7，实际 %v", c[2], got)
			}
		})
	}
}

// 16) number 类型：字符串数值可转换；NaN/Inf 字符串 → ErrConvert。
func TestItemNumberParsing(t *testing.T) {
	p := validate.Property{ItemCode: "temp", ValueType: "number", Unit: "C"}

	out, vs, err := clean.Item(p, rawItem(t, "temp", `"25.4"`, "C", atOK))
	if err != nil {
		t.Fatalf(`字符串 "25.4": 期望成功，实际 %v`, err)
	}
	if out.Number == nil || *out.Number != 25.4 || out.Text != nil {
		t.Fatalf(`字符串 "25.4": 期望 Number=25.4，实际 %+v`, out)
	}
	if len(vs) != 0 {
		t.Fatalf(`字符串 "25.4": 期望无 violation，实际 %v`, vs)
	}

	if out, _, err = clean.Item(p, rawItem(t, "temp", `25.4`, "", atOK)); err != nil {
		t.Fatalf("JSON number 25.4: 期望成功，实际 %v", err)
	}
	if out.Number == nil || *out.Number != 25.4 {
		t.Fatalf("JSON number 25.4: 期望 Number=25.4，实际 %+v", out)
	}
	if out.Unit != "C" {
		t.Fatalf("unit 缺省: 期望取物模型 C，实际 %q", out.Unit)
	}

	for _, bad := range []string{`"NaN"`, `"Inf"`} {
		if _, _, err := clean.Item(p, rawItem(t, "temp", bad, "C", atOK)); !errors.Is(err, clean.ErrConvert) {
			t.Fatalf("%s: 期望 ErrConvert，实际 %v", bad, err)
		}
	}

	// R3：value 不接受 null。validate.ParseTerminal 已在入口拒收，clean 侧再兜一层。
	if _, _, err := clean.Item(p, rawItem(t, "temp", `null`, "C", atOK)); !errors.Is(err, clean.ErrConvert) {
		t.Fatalf("value=null: 期望 ErrConvert，实际 %v", err)
	}
}

// 17) 物模型 min/max 越界 → RuleRange Violation，且 Number 仍是被写入的值。
func TestItemModelRangeViolation(t *testing.T) {
	p := validate.Property{ItemCode: "temp", ValueType: "number", Unit: "C", Min: f64(0), Max: f64(100)}
	cases := []struct {
		value   string
		wantNum float64
		detail  string
	}{
		{`120`, 120, "高于物模型上限"},
		{`-5`, -5, "低于物模型下限"},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			out, vs, err := clean.Item(p, rawItem(t, "temp", c.value, "C", atOK))
			if err != nil {
				t.Fatalf("期望不返回 error，实际 %v", err)
			}
			if len(vs) != 1 || vs[0].RuleType != clean.RuleRange || !strings.Contains(vs[0].Detail, c.detail) {
				t.Fatalf("violation: 期望 1 条 RuleRange(%s)，实际 %v", c.detail, vs)
			}
			if out.Number == nil || *out.Number != c.wantNum {
				t.Fatalf("Number: 期望仍写入 %v，实际 %+v", c.wantNum, out.Number)
			}
			if v, _ := out.Value.(float64); v != c.wantNum {
				t.Fatalf("Value: 期望 %v，实际 %v", c.wantNum, out.Value)
			}
		})
	}
}

// 18) 五类规则各命中一条，且只返回 Violation（不返回 error）。
func TestCheckRuleTypes(t *testing.T) {
	n5, n2546 := 5.0, 25.46
	cases := []struct {
		name       string
		rule       clean.Rule
		out        clean.Out
		wantType   clean.RuleType
		wantDetail string
	}{
		{
			name:       "range",
			rule:       clean.Rule{ItemCode: "temp", Type: clean.RuleRange, Params: json.RawMessage(`{"min":10,"max":20}`)},
			out:        clean.Out{Code: "temp", Number: &n5},
			wantType:   clean.RuleRange,
			wantDetail: "低于下限",
		},
		{
			name:       "enum",
			rule:       clean.Rule{ItemCode: "mode", Type: clean.RuleEnum, Params: json.RawMessage(`{"values":["auto","manual"]}`)},
			out:        clean.Out{Code: "mode", Value: "other"},
			wantType:   clean.RuleEnum,
			wantDetail: "取值不在允许集合",
		},
		{
			name:       "precision",
			rule:       clean.Rule{ItemCode: "temp", Type: clean.RulePrecision, Params: json.RawMessage(`{"scale":1}`)},
			out:        clean.Out{Code: "temp", Number: &n2546},
			wantType:   clean.RulePrecision,
			wantDetail: "小数位超过 1",
		},
		{
			name:       "unit_whitelist",
			rule:       clean.Rule{ItemCode: "temp", Type: clean.RuleUnitWhitelist, Params: json.RawMessage(`{"units":["C"]}`)},
			out:        clean.Out{Code: "temp", Number: &n5, Unit: "F"},
			wantType:   clean.RuleUnitWhitelist,
			wantDetail: "单位不在白名单",
		},
		{
			name:       "not_null",
			rule:       clean.Rule{ItemCode: "temp", Type: clean.RuleNotNull},
			out:        clean.Out{Code: "temp"},
			wantType:   clean.RuleNotNull,
			wantDetail: "空值",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vs := clean.Check(map[string][]clean.Rule{c.out.Code: {c.rule}}, c.out)
			if len(vs) != 1 || vs[0].RuleType != c.wantType || !strings.Contains(vs[0].Detail, c.wantDetail) {
				t.Fatalf("期望 1 条 %s(%s)，实际 %v", c.wantType, c.wantDetail, vs)
			}
			if direct := clean.Verify([]clean.Rule{c.rule}, c.out); len(direct) != 1 {
				t.Fatalf("Verify 应与 Check 一致（1 条），实际 %v", direct)
			}
		})
	}
}

// 19) 网关侧 unit 一律忽略：单位取物模型声明，数值不换算、不判 UnitUnknown。
func TestItemUnitIgnored(t *testing.T) {
	p := validate.Property{ItemCode: "temp", ValueType: "number", Unit: "C", Precision: intp(1)}
	out, vs, err := clean.Item(p, rawItem(t, "temp", `300`, "K", atOK))
	if err != nil {
		t.Fatalf("期望不返回 error，实际 %v", err)
	}
	if out.Unit != "C" {
		t.Fatalf("Unit: 期望取物模型 C，实际 %q", out.Unit)
	}
	if out.Number == nil {
		t.Fatalf("Number 不应为 nil")
	}
	approx(t, "不换算", *out.Number, 300, 1e-9)
	if len(vs) != 0 {
		t.Fatalf("单位忽略时期望无 violation，实际 %v", vs)
	}
}

// 20) precision 取整：声明 1 位小数的 25.46 → 25.5。
func TestItemPrecision(t *testing.T) {
	p := validate.Property{ItemCode: "temp", ValueType: "number", Unit: "C", Precision: intp(1)}
	out, vs, err := clean.Item(p, rawItem(t, "temp", `25.46`, "C", atOK))
	if err != nil {
		t.Fatalf("期望成功，实际 %v", err)
	}
	if len(vs) != 0 {
		t.Fatalf("期望无 violation，实际 %v", vs)
	}
	approx(t, "precision=1", *out.Number, 25.5, 1e-9)
	if v, _ := out.Value.(float64); math.Abs(v-25.5) > 1e-9 {
		t.Fatalf("Value 应同步取整为 25.5，实际 %v", out.Value)
	}

	// 网关单位被忽略：F 不换算，按原值 100.04 取整为 100.0，单位取物模型 C。
	pf := validate.Property{ItemCode: "temp", ValueType: "number", Unit: "C", Precision: intp(1)}
	outF, _, err := clean.Item(pf, rawItem(t, "temp", `100.04`, "F", atOK))
	if err != nil {
		t.Fatalf("原值取整: 期望成功，实际 %v", err)
	}
	approx(t, "原值取整", *outF.Number, 100.0, 1e-9)
	if outF.Unit != "C" {
		t.Fatalf("单位: 期望物模型 C，实际 %q", outF.Unit)
	}
	if !outF.Time.Equal(mustTime(t, atOK)) {
		t.Fatalf("Time 应透传 ParsedTime，实际 %s", outF.Time)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := validate.ParseCollectedAt(s)
	if err != nil {
		t.Fatalf("ParseCollectedAt(%q): %v", s, err)
	}
	return parsed
}
