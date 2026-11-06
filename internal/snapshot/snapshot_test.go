package snapshot_test

import (
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/snapshot"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

func local(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, staging.Zone)
}

func mustEqual(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %s，期望 %s", what, got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// 段起点对齐：六档 × 边界（跨月、跨年、闰年 2 月）。全部按 Asia/Shanghai。
func TestSegmentStart(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		g    snapshot.Granularity
		step int
		want time.Time
	}{
		{"秒 step=10 向下取整", local(2026, 11, 5, 10, 0, 37), snapshot.Second, 10, local(2026, 11, 5, 10, 0, 30)},
		{"秒 step=10 边界不前进", local(2026, 11, 5, 10, 0, 30), snapshot.Second, 10, local(2026, 11, 5, 10, 0, 30)},
		{"秒 step=30 跨分", local(2026, 11, 5, 10, 0, 59), snapshot.Second, 30, local(2026, 11, 5, 10, 0, 30)},
		{"分 step=15", local(2026, 11, 5, 10, 37, 12), snapshot.Minute, 15, local(2026, 11, 5, 10, 30, 0)},
		{"分 step=2 偶数分", local(2026, 11, 5, 10, 37, 0), snapshot.Minute, 2, local(2026, 11, 5, 10, 36, 0)},
		{"分 step=30 跨小时", local(2026, 11, 5, 10, 59, 0), snapshot.Minute, 30, local(2026, 11, 5, 10, 30, 0)},
		{"时 step=2 偶数小时", local(2026, 11, 5, 13, 37, 0), snapshot.Hour, 2, local(2026, 11, 5, 12, 0, 0)},
		{"时 step=3 三小时档", local(2026, 11, 5, 13, 37, 0), snapshot.Hour, 3, local(2026, 11, 5, 12, 0, 0)},
		{"时 step=12 半日档", local(2026, 11, 5, 11, 0, 0), snapshot.Hour, 12, local(2026, 11, 5, 0, 0, 0)},
		{"日档取本地 0 点", local(2026, 11, 5, 10, 37, 0), snapshot.Day, 1, local(2026, 11, 5, 0, 0, 0)},
		{"月 step=1", local(2026, 11, 5, 10, 0, 0), snapshot.Month, 1, local(2026, 11, 1, 0, 0, 0)},
		{"月 step=3 落在自然季度", local(2026, 5, 17, 10, 0, 0), snapshot.Month, 3, local(2026, 4, 1, 0, 0, 0)},
		{"月 step=3 十二月归十月", local(2026, 12, 31, 23, 59, 0), snapshot.Month, 3, local(2026, 10, 1, 0, 0, 0)},
		{"月 step=2 单月起步", local(2026, 5, 9, 0, 0, 0), snapshot.Month, 2, local(2026, 5, 1, 0, 0, 0)},
		{"月 step=2 双月归到单月", local(2026, 6, 9, 0, 0, 0), snapshot.Month, 2, local(2026, 5, 1, 0, 0, 0)},
		{"月 step=6 半年档", local(2026, 8, 9, 0, 0, 0), snapshot.Month, 6, local(2026, 7, 1, 0, 0, 0)},
		{"月 step=12 年档", local(2026, 11, 5, 0, 0, 0), snapshot.Month, 12, local(2026, 1, 1, 0, 0, 0)},
		{"闰年 2 月 step=4", local(2028, 2, 29, 12, 0, 0), snapshot.Month, 4, local(2028, 1, 1, 0, 0, 0)},
		{"年档 1 月 1 日", local(2026, 11, 5, 10, 0, 0), snapshot.Year, 1, local(2026, 1, 1, 0, 0, 0)},
		// UTC 输入必须先换算到 +08：16:00Z = 次日 00:00 +08。
		{"UTC 跨日", time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC), snapshot.Day, 1, local(2026, 11, 6, 0, 0, 0)},
		{"UTC 跨日 + 2 小时档", time.Date(2026, 11, 5, 16, 30, 0, 0, time.UTC), snapshot.Hour, 2, local(2026, 11, 6, 0, 0, 0)},
	}
	for _, c := range cases {
		mustEqual(t, c.name, snapshot.SegmentStart(c.at, c.g, c.step), c.want)
	}
}

// 段结束 = 段起点 + step×单位（月/年按日历加法）。
func TestSegmentEnd(t *testing.T) {
	mustEqual(t, "小时 step=2", snapshot.SegmentEnd(local(2026, 11, 5, 12, 0, 0), snapshot.Hour, 2), local(2026, 11, 5, 14, 0, 0))
	mustEqual(t, "秒 step=30", snapshot.SegmentEnd(local(2026, 11, 5, 12, 0, 30), snapshot.Second, 30), local(2026, 11, 5, 12, 1, 0))
	mustEqual(t, "月 step=3 跨年", snapshot.SegmentEnd(local(2026, 11, 1, 0, 0, 0), snapshot.Month, 3), local(2027, 2, 1, 0, 0, 0))
	mustEqual(t, "年 step=1", snapshot.SegmentEnd(local(2026, 1, 1, 0, 0, 0), snapshot.Year, 1), local(2027, 1, 1, 0, 0, 0))
}

// 迟到判定：段结束 + 15 分钟，闭区间外即不可写（每档独立）。
func TestWritable(t *testing.T) {
	start := local(2026, 11, 5, 10, 0, 0) // 小时 step=1 → 段结束 11:00
	if !snapshot.Writable(local(2026, 11, 5, 11, 14, 59), start, snapshot.Hour, 1) {
		t.Fatal("11:14:59 仍在宽限内，应可写")
	}
	if snapshot.Writable(local(2026, 11, 5, 11, 15, 0), start, snapshot.Hour, 1) {
		t.Fatal("11:15:00 宽限已过，应不可写")
	}
}

// 步长白名单与 0010 的 CHECK 同值。
func TestValidate(t *testing.T) {
	ok := []struct {
		g    snapshot.Granularity
		step int
	}{
		{snapshot.Second, 1}, {snapshot.Second, 30},
		{snapshot.Minute, 20}, {snapshot.Hour, 8}, {snapshot.Day, 1},
		{snapshot.Month, 12}, {snapshot.Year, 1},
	}
	for _, c := range ok {
		if err := snapshot.Validate(c.step, c.g); err != nil {
			t.Fatalf("%s/%d 应通过白名单: %v", c.g, c.step, err)
		}
	}
	bad := []struct {
		g    snapshot.Granularity
		step int
	}{
		{snapshot.Second, 7}, {snapshot.Minute, 60}, {snapshot.Hour, 5},
		{snapshot.Day, 2}, {snapshot.Month, 5}, {snapshot.Year, 2},
		{snapshot.Granularity("week"), 1},
	}
	for _, c := range bad {
		if err := snapshot.Validate(c.step, c.g); err == nil {
			t.Fatalf("%s/%d 应被白名单拒绝", c.g, c.step)
		}
	}
}

// 每档独立判迟到：同一条数据进粗档、进不了秒/分档；未绑定则零写入（即使六档全开）。
func TestRowsLatePerGranularity(t *testing.T) {
	cfg := snapshot.Config{
		snapshot.Second: {Enabled: true, Step: 1},
		snapshot.Minute: {Enabled: true, Step: 1},
		snapshot.Hour:   {Enabled: true, Step: 1},
	}
	at := local(2026, 11, 5, 10, 0, 30)
	value := snapshot.Value{TerminalID: "T1", ItemCode: "temp", Time: at}

	// 20 分钟后：秒段(10:00:30+15m=10:15:31)、分段(10:01+15m=10:16)均过期，小时段(11:15)仍可写。
	rows, dropped := snapshot.Rows(cfg, true, at.Add(20*time.Minute), value)
	if len(rows) != 1 || rows[0].Granularity != snapshot.Hour {
		t.Fatalf("迟到分档应只剩小时档，实际 %+v", rows)
	}
	if len(dropped) != 2 || dropped[0].Granularity != snapshot.Second || dropped[1].Granularity != snapshot.Minute {
		t.Fatalf("应返回秒/分两档的丢弃明细，实际 %+v", dropped)
	}
	if dropped[0].Lateness <= 0 {
		t.Fatalf("Lateness 应为正（超过截止多久），实际 %s", dropped[0].Lateness)
	}

	// 10 秒后：三档都可写。
	rows, dropped = snapshot.Rows(cfg, true, at.Add(10*time.Second), value)
	if len(rows) != 3 || len(dropped) != 0 {
		t.Fatalf("宽限内应写三档且无丢弃，实际 %d 行 / %d 丢弃", len(rows), len(dropped))
	}

	// 未绑定：零行零丢弃（验收 7：六档全开但没绑定 = 不产生快照）。
	rows, dropped = snapshot.Rows(cfg, false, at, value)
	if len(rows) != 0 || len(dropped) != 0 {
		t.Fatalf("未绑定的序列应零写入，实际 %+v / %+v", rows, dropped)
	}

	// 六档全开但都未启用的档位不发散：只按 enabled 出档。
	partial := snapshot.Config{snapshot.Hour: {Enabled: false, Step: 1}}
	if rows, _ := snapshot.Rows(partial, true, at, value); len(rows) != 0 {
		t.Fatalf("未启用的档不应出��，实际 %+v", rows)
	}
	if got := cfg.Enabled(); len(got) != 3 {
		t.Fatalf("Enabled() = %v，期望 3 档", got)
	}
}

// 两级绑定：类型级 OR 实例级（并集），双命中仍只算一次。
func TestBindingsUnion(t *testing.T) {
	b := snapshot.Bindings{
		Types:     map[int64]map[string]struct{}{1: {"temp": {}}},
		Terminals: map[string]map[string]struct{}{"T9": {"hum": {}}},
	}
	cases := []struct {
		name     string
		terminal string
		typeID   int64
		item     string
		want     bool
	}{
		{"类型级命中该类型下任意终端", "T1", 1, "temp", true},
		{"类型级不命中别的类型", "T1", 2, "temp", false},
		{"实例级只命中那台终端", "T9", 2, "hum", true},
		{"实例级不命中别的终端", "T1", 2, "hum", false},
		{"双命中（类型级 + 实例级）仍为真", "T9", 1, "temp", true},
		{"都没绑定", "T1", 1, "hum", false},
	}
	for _, c := range cases {
		if got := b.Binds(c.terminal, c.typeID, c.item); got != c.want {
			t.Fatalf("%s: Binds(%q,%d,%q) = %v，期望 %v", c.name, c.terminal, c.typeID, c.item, got, c.want)
		}
	}
}
