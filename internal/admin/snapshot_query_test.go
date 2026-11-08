package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 参数校验先于查库：nil pool 也能覆盖这些 400 分支。
func TestSnapshotQueryValidation(t *testing.T) {
	handler := New(nil).Handler()
	from := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	longAgo := time.Now().Add(-400 * 24 * time.Hour).UTC().Format(time.RFC3339)

	cases := []struct {
		name string
		url  string
	}{
		{"缺 template", "/admin/v1/snapshot-query?granularity=hour&from=" + from + "&to=" + to},
		{"template 非整数", "/admin/v1/snapshot-query?template=x&granularity=hour&from=" + from + "&to=" + to},
		{"档位非法", "/admin/v1/snapshot-query?template=1&granularity=second&from=" + from + "&to=" + to},
		{"from 非法", "/admin/v1/snapshot-query?template=1&granularity=hour&from=yesterday&to=" + to},
		{"to 早于 from", "/admin/v1/snapshot-query?template=1&granularity=hour&from=" + to + "&to=" + from},
		{"小时档跨度超 7 天", "/admin/v1/snapshot-query?template=1&granularity=hour&from=" + longAgo + "&to=" + to},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// 四档的跨度上限必须按档位区分：小时 7 天、日 31 天、月 366 天、年 3650 天。
func TestSnapshotQuerySpans(t *testing.T) {
	for granularity, want := range map[string]time.Duration{
		"hour":  7 * 24 * time.Hour,
		"day":   31 * 24 * time.Hour,
		"month": 366 * 24 * time.Hour,
		"year":  3650 * 24 * time.Hour,
	} {
		got, ok := snapshotQuerySpan(granularity)
		if !ok || got != want {
			t.Fatalf("%s 跨度 = %v/%v，期望 %v", granularity, got, ok, want)
		}
	}
	if _, ok := snapshotQuerySpan("minute"); ok {
		t.Fatal("分钟档不应进入快照查询")
	}
}
