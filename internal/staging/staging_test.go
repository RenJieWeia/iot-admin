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
