// Package exception 唯一的 error_type 枚举定义处：异常类型统一写入 exception_count。
package exception

// ErrorType 是异常计数表的闭集，禁止在别处散落字符串。
type ErrorType string

const (
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

// EventTypes 事件型集合，与 migrations/0003 的 check 约束一致。
var EventTypes = []ErrorType{
	UploadTimeout, ProtocolError, MsgIncomplete, CleanFailed, CacheWriteFailed,
	MQTTPublishFailed, SnapshotFailed, LateData, QualityViolation, TerminalUnknown,
}

// Parse 校验外部输入（如管理 API 参数）是否属于闭集。
func Parse(s string) (ErrorType, bool) {
	t := ErrorType(s)
	for _, v := range EventTypes {
		if v == t {
			return t, true
		}
	}
	return "", false
}
