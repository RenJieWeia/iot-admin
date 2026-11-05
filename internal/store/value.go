package store

import "time"

// Value 是 latest_value 与 hourly_staging 共用的行形状：数值型存 value_num，文本/布尔存 value_text。
type Value struct {
	TerminalID string
	ItemCode   string
	ValueNum   *float64
	ValueText  *string
	Unit       string
	ValueTime  time.Time
}
