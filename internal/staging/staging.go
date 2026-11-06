// Package staging 只剩时间口径助手：Zone 与小时归属计算。
//
// 小时暂存表与结算链路已下线（0009 迁移），这里保留的是仍在生产路径上的两个符号：
// 异常计数按「终端 + 小时 + 类型」聚合，仍要用同一套上海时区与取整口径。
package staging

import "time"

// Zone 固定 Asia/Shanghai（无夏令时），不依赖运行环境是否装了 tzdata。
var Zone = time.FixedZone("Asia/Shanghai", 8*60*60)

// HourStart 把采集时间取整到所属自然小时（Asia/Shanghai）。
func HourStart(t time.Time) time.Time {
	t = t.In(Zone)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, Zone)
}
