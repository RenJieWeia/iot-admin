package clean

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// ErrConvert 是"拒收 + clean_failed"的判据：类型转换失败、NaN、Infinity。
var ErrConvert = errors.New("clean_failed")

// Out 是清洗后的数据项：Number/Text 二选一入库，Value 用于实时发布。
type Out struct {
	Code        string
	Number      *float64
	Text        *string
	Value       any
	Unit string // 单位只取物模型声明（服务端事实源），网关侧 unit 一律忽略
	Time time.Time
}

// Item 按物模型声明做类型转换、单位换算、精度取整，并给出物模型边界违规。
func Item(prop validate.Property, raw validate.RawItem) (Out, []Violation, error) {
	out := Out{Code: prop.ItemCode, Time: raw.ParsedTime}
	var violations []Violation
	var num *float64

	switch prop.ValueType {
	case "number":
		v, err := parseNumber(raw.Value)
		if err != nil {
			return out, nil, fmt.Errorf("%w: items[%s] %v", ErrConvert, prop.ItemCode, err)
		}
		out.Value = v
		num = &v
	case "string":
		var s string
		if err := json.Unmarshal(raw.Value, &s); err != nil {
			return out, nil, fmt.Errorf("%w: items[%s] 值不是字符串: %v", ErrConvert, prop.ItemCode, err)
		}
		out.Text = &s
		out.Value = s
	case "bool":
		var b bool
		if err := json.Unmarshal(raw.Value, &b); err != nil {
			return out, nil, fmt.Errorf("%w: items[%s] 值不是布尔: %v", ErrConvert, prop.ItemCode, err)
		}
		s := strconv.FormatBool(b)
		out.Text = &s
		out.Value = b
	default:
		return out, nil, fmt.Errorf("%w: items[%s] 物模型 value_type 非法: %q", ErrConvert, prop.ItemCode, prop.ValueType)
	}

	// 单位只认服务端物模型：网关上传的 unit 不参与换算、不落库、不判质量。
	target := strings.TrimSpace(prop.Unit)
	out.Unit = target
	if num == nil {
		return out, nil, nil
	}

	v := *num
	if prop.Precision != nil {
		v = roundTo(v, *prop.Precision)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return out, nil, fmt.Errorf("%w: items[%s] 换算结果为非有限数值", ErrConvert, prop.ItemCode)
	}
	if prop.Min != nil && v < *prop.Min {
		violations = append(violations, Violation{RuleType: RuleRange, Detail: "低于物模型下限"})
	}
	if prop.Max != nil && v > *prop.Max {
		violations = append(violations, Violation{RuleType: RuleRange, Detail: "高于物模型上限"})
	}
	out.Number = &v
	out.Value = v
	return out, violations, nil
}

// Check 执行终端绑定的质量规则（含单位白名单等），命中只计数不拒收。
func Check(rules map[string][]Rule, out Out) []Violation {
	return Verify(rules[out.Code], out)
}

func parseNumber(raw json.RawMessage) (float64, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return 0, errors.New("值为 null")
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errors.New("值为 NaN/Infinity")
		}
		return f, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		f, perr := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if perr != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("值 %q 不是合法数值", s)
		}
		return f, nil
	}
	return 0, fmt.Errorf("值 %s 不是合法数值", string(raw))
}

func roundTo(v float64, digits int) float64 {
	p := pow10(digits)
	return math.Round(v*p) / p
}

func pow10(n int) float64     { return math.Pow(10, float64(n)) }
func round(v float64) float64 { return math.Round(v) }
func abs(v float64) float64   { return math.Abs(v) }
