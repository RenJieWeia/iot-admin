package store

import "time"

// 行级质量标记的位定义（R-022，对应 migrations/0005_quality_flag.sql）。
// 可叠加：越界 + 规则命中 = QualityOutOfRange | QualityRuleHit。
const (
	// QualityOK 表示该行数据没有质量异常。
	QualityOK int16 = 0
	// QualityOutOfRange 表示触发了物模型 min/max 边界。
	QualityOutOfRange int16 = 1 << 0
	// QualityRuleHit 表示命中了终端质量规则。
	QualityRuleHit int16 = 1 << 1
	// QualityUnitUnknown 表示单位不在换算闭集内（原单位透传）。
	QualityUnitUnknown int16 = 1 << 2
)

// Value 是 latest_value 与 hourly_staging 共用的行形状：数值型存 value_num，文本/布尔存 value_text。
// Quality 是位掩码，QualityReason 是可读摘要；原值字段（ValueNum/ValueText/Unit）不因质量异常而改变。
type Value struct {
	TerminalID    string
	ItemCode      string
	ValueNum      *float64
	ValueText     *string
	Unit          string
	ValueTime     time.Time
	Quality       int16
	QualityReason string
}
