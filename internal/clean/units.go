package clean

import "strings"

// 单位换算闭集（R5）：只有四组，其余一律视为不可识别。
//   C  <-> F    温度（带偏移）
//   W  <-> kW   功率
//   Pa <-> kPa  压强
//   Wh <-> kWh  能量
// 闭集外单位：透传原单位 + clean_failed 计数，不拒收。

// unitDef 描述一个单位：所属量纲 + 到基准单位的双向换算。
type unitDef struct {
	dim  string
	to   func(float64) float64
	from func(float64) float64
}

func linear(dim string, factor float64) unitDef {
	return unitDef{
		dim:  dim,
		to:   func(v float64) float64 { return v * factor },
		from: func(v float64) float64 { return v / factor },
	}
}

var unitDefs = map[string]unitDef{
	// 温度（基准 C，F 带偏移）
	"C": {dim: "temperature", to: func(v float64) float64 { return v }, from: func(v float64) float64 { return v }},
	"F": {dim: "temperature", to: func(v float64) float64 { return (v - 32) * 5 / 9 }, from: func(v float64) float64 { return v*9/5 + 32 }},

	// 功率（基准 W）
	"W":  linear("power", 1),
	"kW": linear("power", 1000),

	// 压强（基准 Pa）
	"Pa":  linear("pressure", 1),
	"kPa": linear("pressure", 1000),

	// 能量（基准 Wh）
	"Wh":  linear("energy", 1),
	"kWh": linear("energy", 1000),
}

// Convert 把 v 从 from 换算到 to。任一单位不在闭集内或量纲不匹配时 ok=false（调用方透传原单位 + clean_failed）。
func Convert(v float64, from, to string) (float64, bool) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == to {
		_, known := unitDefs[from]
		return v, known
	}
	f, ok := unitDefs[from]
	if !ok {
		return v, false
	}
	t, ok := unitDefs[to]
	if !ok || f.dim != t.dim {
		return v, false
	}
	return t.from(f.to(v)), true
}
