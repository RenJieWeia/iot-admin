package clean

import "strings"

// unitDef 描述一个单位：所属量纲 + 到基准单位的双向换算。温度有偏移，单独给函数。
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
	// 温度（基准 C）
	"C":  {dim: "temperature", to: func(v float64) float64 { return v }, from: func(v float64) float64 { return v }},
	"℃":  {dim: "temperature", to: func(v float64) float64 { return v }, from: func(v float64) float64 { return v }},
	"K":  {dim: "temperature", to: func(v float64) float64 { return v - 273.15 }, from: func(v float64) float64 { return v + 273.15 }},
	"F":  {dim: "temperature", to: func(v float64) float64 { return (v - 32) * 5 / 9 }, from: func(v float64) float64 { return v*9/5 + 32 }},
	"°F": {dim: "temperature", to: func(v float64) float64 { return (v - 32) * 5 / 9 }, from: func(v float64) float64 { return v*9/5 + 32 }},

	// 长度（基准 m）
	"mm": linear("length", 0.001),
	"cm": linear("length", 0.01),
	"m":  linear("length", 1),
	"km": linear("length", 1000),

	// 质量（基准 kg）
	"g":  linear("mass", 0.001),
	"kg": linear("mass", 1),
	"t":  linear("mass", 1000),

	// 功率（基准 W）
	"W":  linear("power", 1),
	"kW": linear("power", 1000),
	"MW": linear("power", 1e6),

	// 电压（基准 V）/ 电流（基准 A）
	"mV": linear("voltage", 0.001),
	"V":  linear("voltage", 1),
	"kV": linear("voltage", 1000),
	"mA": linear("current", 0.001),
	"A":  linear("current", 1),

	// 压强（基准 Pa）
	"Pa":  linear("pressure", 1),
	"kPa": linear("pressure", 1000),
	"MPa": linear("pressure", 1e6),
	"bar": linear("pressure", 1e5),

	// 能量（基准 J）/ 频率（基准 Hz）/ 时长（基准 s）
	"J":   linear("energy", 1),
	"kJ":  linear("energy", 1000),
	"kWh": linear("energy", 3.6e6),
	"Hz":  linear("frequency", 1),
	"kHz": linear("frequency", 1000),
	"s":   linear("time", 1),
	"ms":  linear("time", 0.001),
	"min": linear("time", 60),

	// 流速 / 流量 / 转速
	"m/s":     linear("speed", 1),
	"km/h":    linear("speed", 1/3.6),
	"m3/h":    linear("flow", 1),
	"L/h":     linear("flow", 0.001),
	"L/min":   linear("flow", 0.001/60),
	"rpm":     linear("rpm", 1),
	"r/min":   linear("rpm", 1),
	"percent": {dim: "ratio", to: func(v float64) float64 { return v }, from: func(v float64) float64 { return v }},
	"%":       {dim: "ratio", to: func(v float64) float64 { return v }, from: func(v float64) float64 { return v }},
}

// Convert 把 v 从 from 换算到 to。任一单位不可识别或量纲不匹配时 ok=false（调用方透传原单位）。
func Convert(v float64, from, to string) (float64, bool) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == to {
		return v, true
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
