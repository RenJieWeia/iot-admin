package snapshot

import (
	"testing"
	"time"
)

// 主键收敛：同一自然键（终端, 数据项, 档位, 段起点）只留一行，且保留采集时间最新的那条。
// 这是「类型级 + 实例级双命中只写一次」的最后一道保险：PG 在同一条 INSERT 里遇到重复主键
// 会直接报 cannot affect row a second time。
func TestCollapseRows(t *testing.T) {
	seg := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	num := 1.0
	newer := 2.0
	rows := []Row{
		{TerminalID: "T1", ItemCode: "temp", Granularity: Hour, SegmentStart: seg, ValueNum: &num, ValueTime: seg},
		{TerminalID: "T1", ItemCode: "temp", Granularity: Hour, SegmentStart: seg, ValueNum: &newer, ValueTime: seg.Add(time.Minute)},
		{TerminalID: "T1", ItemCode: "temp", Granularity: Day, SegmentStart: seg},
	}
	out := collapseRows(rows)
	if len(out) != 2 {
		t.Fatalf("收敛后行数 = %d，期望 2（小时档双命中收敛成一行 + 日档一行）", len(out))
	}
	if out[0].Granularity != Hour || out[0].ValueNum == nil || *out[0].ValueNum != newer {
		t.Fatalf("应保留 value_time 更新的那条，实际 %+v", out[0])
	}

	if got := collapseRows(nil); len(got) != 0 {
		t.Fatalf("nil 应返回空，实际 %+v", got)
	}
	if got := collapseRows(rows[:1]); len(got) != 1 {
		t.Fatalf("单行不应被改动，实际 %+v", got)
	}
}
