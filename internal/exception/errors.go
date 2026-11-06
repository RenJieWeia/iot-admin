// Package exception 唯一的 error_type 枚举定义处：异常类型统一写入 exception_count。
package exception

// ErrorType 是异常计数表的闭集，禁止在别处散落字符串。
type ErrorType string

const (
	// 事件型：写入 exception_count（同一 (终端, 小时, 类型) 聚合成一行）。
	ProtocolError     ErrorType = "protocol_error"
	MsgIncomplete     ErrorType = "msg_incomplete"
	CleanFailed       ErrorType = "clean_failed"
	CacheWriteFailed  ErrorType = "cache_write_failed"
	MQTTPublishFailed ErrorType = "mqtt_publish_failed"
	QualityViolation  ErrorType = "quality_violation"
	TerminalUnknown   ErrorType = "terminal_unknown"
	// SnapshotWriteFailed 是「六档快照写入失败」（平台故障，如缺分区/DB 不可用），
	// 与已下线的 snapshot_failed（旧结算任务失败）不是一回事：后者随结算链路一起删除了。
	SnapshotWriteFailed ErrorType = "snapshot_write_failed"
)

// EventTypes 事件型集合，与 migrations/0003 的 check 约束一致。
var EventTypes = []ErrorType{
	ProtocolError, MsgIncomplete, CleanFailed, CacheWriteFailed,
	MQTTPublishFailed, QualityViolation, TerminalUnknown, SnapshotWriteFailed,
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
