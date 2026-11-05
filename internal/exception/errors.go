// Package exception 唯一的 error_type 枚举定义处：状态型异常进区间表，事件型异常进计数表。
package exception

// ErrorType 是两张异常表共用的闭集，禁止在别处散落字符串。
type ErrorType string

const (
	// 状态型：写入 exception_interval（区间 + 抖动合并 + upper_inf 表未结束）。
	GatewayDisconnected ErrorType = "gateway_disconnected"
	AuthFailed          ErrorType = "auth_failed"
	ItemSilent          ErrorType = "item_silent"

	// 事件型：写入 exception_count（同一 (终端, 小时, 类型) 聚合成一行）。
	UploadTimeout     ErrorType = "upload_timeout"
	ProtocolError     ErrorType = "protocol_error"
	MsgIncomplete     ErrorType = "msg_incomplete"
	CleanFailed       ErrorType = "clean_failed"
	CacheWriteFailed  ErrorType = "cache_write_failed"
	MQTTPublishFailed ErrorType = "mqtt_publish_failed"
	SnapshotFailed    ErrorType = "snapshot_failed"
	LateData          ErrorType = "late_data"
	QualityViolation  ErrorType = "quality_violation"
	TerminalUnknown   ErrorType = "terminal_unknown"
)

// IntervalTypes 状态型集合，与 migrations/0003 的 check 约束一致。
var IntervalTypes = []ErrorType{GatewayDisconnected, AuthFailed, ItemSilent}

// EventTypes 事件型集合，与 migrations/0003 的 check 约束一致。
var EventTypes = []ErrorType{
	UploadTimeout, ProtocolError, MsgIncomplete, CleanFailed, CacheWriteFailed,
	MQTTPublishFailed, SnapshotFailed, LateData, QualityViolation, TerminalUnknown,
}

func (t ErrorType) IsInterval() bool {
	for _, v := range IntervalTypes {
		if v == t {
			return true
		}
	}
	return false
}

// Parse 校验外部输入（如管理 API 参数）是否属于闭集。
func Parse(s string) (ErrorType, bool) {
	t := ErrorType(s)
	for _, v := range append(append([]ErrorType{}, IntervalTypes...), EventTypes...) {
		if v == t {
			return t, true
		}
	}
	return "", false
}
