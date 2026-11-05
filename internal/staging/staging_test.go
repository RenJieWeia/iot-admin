package staging_test

import (
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// mustParse 解析固定 case 用时间戳，失败直接终止。
func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", s, err)
	}
	return parsed
}

// 21) HourStart 归到 Asia/Shanghai 整点（含 UTC 输入换算）。
func TestHourStart(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2026-11-05T10:59:59+08:00", "2026-11-05T10:00:00+08:00"},
		{"2026-11-05T02:00:00Z", "2026-11-05T10:00:00+08:00"},
		{"2026-11-04T16:30:00Z", "2026-11-05T00:00:00+08:00"}, // 跨日边界
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := staging.HourStart(mustParse(t, c.in))
			if !got.Equal(mustParse(t, c.want)) {
				t.Fatalf("HourStart(%s): 期望瞬间 %s，实际 %s", c.in, c.want, got)
			}
			if text := got.Format(time.RFC3339); text != c.want {
				t.Fatalf("HourStart(%s): 期望 %s（+08:00 整点），实际 %s", c.in, c.want, text)
			}
			if got.Location() != staging.Zone {
				t.Fatalf("HourStart(%s): 期望 Location 为 %s，实际 %s", c.in, staging.Zone, got.Location())
			}
		})
	}

	// 10:59:59+08 必须正好落在 10:00，且小时数为 10。
	h := staging.HourStart(mustParse(t, "2026-11-05T10:59:59+08:00"))
	if h.Hour() != 10 || !h.Equal(time.Date(2026, 11, 5, 10, 0, 0, 0, staging.Zone)) {
		t.Fatalf("HourStart 10:59:59+08: 期望 2026-11-05 10:00+08，实际 %s", h)
	}
}

// 22) CloseAt(window=900) = hourStart + 1h15m。
func TestCloseAt(t *testing.T) {
	hour := staging.HourStart(mustParse(t, "2026-11-05T10:00:00+08:00"))
	got := staging.CloseAt(hour, 900)
	want := time.Date(2026, 11, 5, 11, 15, 0, 0, staging.Zone)
	if !got.Equal(want) {
		t.Fatalf("CloseAt: 期望 %s，实际 %s", want, got)
	}
	if got.Sub(hour) != time.Hour+15*time.Minute {
		t.Fatalf("CloseAt 偏移: 期望 1h15m，实际 %s", got.Sub(hour))
	}
	if want.Sub(hour) != time.Hour+time.Duration(staging.DefaultWindowSeconds)*time.Second {
		t.Fatalf("DefaultWindowSeconds: 期望 900，实际 %d", staging.DefaultWindowSeconds)
	}
}

// 23) InWindow 边界：now=close 为 false，close±1s 分别为 true/false。
func TestInWindow(t *testing.T) {
	hour := staging.HourStart(mustParse(t, "2026-11-05T10:00:00+08:00"))
	close := staging.CloseAt(hour, staging.DefaultWindowSeconds)

	if staging.InWindow(hour, staging.DefaultWindowSeconds, close) {
		t.Fatalf("now=close: 期望 false，实际 true")
	}
	if !staging.InWindow(hour, staging.DefaultWindowSeconds, close.Add(-time.Second)) {
		t.Fatalf("now=close-1s: 期望 true，实际 false")
	}
	if staging.InWindow(hour, staging.DefaultWindowSeconds, close.Add(time.Second)) {
		t.Fatalf("now=close+1s: 期望 false，实际 true")
	}
}
