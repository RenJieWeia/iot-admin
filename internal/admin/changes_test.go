package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 参数校验：since 必填且必须是 RFC3339；limit 截断到上限；cursor 非法 400。
func TestParseChangesQuery(t *testing.T) {
	since := "2026-11-06T10:00:00+08:00"
	cases := []struct {
		name     string
		values   url.Values
		wantCode int
		want     changesQuery
	}{
		{"缺 since", url.Values{}, 400, changesQuery{}},
		{"since 非法", url.Values{"since": {"2026-11-06"}}, 400, changesQuery{}},
		{"默认 limit", url.Values{"since": {since}}, 200, changesQuery{Limit: ChangesDefaultLimit}},
		{"limit 上限截断", url.Values{"since": {since}, "limit": {"99999"}}, 200, changesQuery{Limit: ChangesMaxLimit}},
		{"limit 非整数", url.Values{"since": {since}, "limit": {"x"}}, 400, changesQuery{}},
		{"cursor 非法", url.Values{"since": {since}, "cursor": {"@@@"}}, 400, changesQuery{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseChangesQuery(c.values)
			if c.wantCode == 400 {
				if err == nil {
					t.Fatalf("期望报错，实际 %+v", got)
				}
				if !strings.Contains(err.Error(), "invalid_query") {
					t.Fatalf("错误应为 invalid_query，实际 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got.Limit != c.want.Limit {
				t.Fatalf("limit = %d，期望 %d", got.Limit, c.want.Limit)
			}
		})
	}

	// HTTP 层：缺 since / 非法 since → 400，不需要数据库。
	handler := New(nil).Handler()
	for _, path := range []string{
		"/admin/v1/snapshot/changes",
		"/admin/v1/snapshot/changes?since=yesterday",
		"/admin/v1/snapshot/changes?since=2026-11-06T10:00:00%2B08:00&cursor=@@@",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d，期望 400: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// 游标编解码：往返一致，且能拒绝被篡改的串。
func TestChangesCursorRoundTrip(t *testing.T) {
	in := changesCursor{
		SettledAt:  time.Date(2026, 11, 6, 10, 0, 0, 123456789, time.UTC),
		TerminalID: "T1",
		ItemCode:   "temp",
		HourStart:  time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC),
	}
	encoded := encodeChangesCursor(in)
	got, err := decodeChangesCursor(encoded)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if !got.SettledAt.Equal(in.SettledAt) || got.TerminalID != in.TerminalID ||
		got.ItemCode != in.ItemCode || !got.HourStart.Equal(in.HourStart) {
		t.Fatalf("往返不一致: %+v vs %+v", got, in)
	}
	if _, err := decodeChangesCursor(encoded + "x"); err == nil {
		t.Fatal("被篡改的游标应报错")
	}
}

// 需 DSN：分页不重不漏、since 过滤、revision 可见。
func TestChangesWithDB(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 IOT_TEST_DSN，跳过需要 PostgreSQL 的用例")
	}
	ctx := context.Background()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("连接 PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("执行迁移: %v", err)
	}
	prefix := fmt.Sprintf("chg-%d-", os.Getpid())
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from hourly_snapshot where terminal_id like $1`, prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	hour := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	rows := make([]staging.Row, 0, 5)
	for i := 0; i < 5; i++ {
		v := float64(i)
		rows = append(rows, staging.Row{
			Value: store.Value{
				TerminalID: fmt.Sprintf("%s%d", prefix, i%2), ItemCode: fmt.Sprintf("item%d", i),
				ValueNum: &v, Unit: "C", ValueTime: hour.Add(time.Duration(i) * time.Minute),
				Quality: int16(i % 3), QualityReason: fmt.Sprintf("reason-%d", i),
			},
			HourStart: hour,
		})
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := settle.UpsertSnapshot(ctx, tx, rows[len(rows)-1:])
			return err
		}); err != nil {
			t.Fatalf("写快照: %v", err)
		}
		time.Sleep(2 * time.Millisecond) // 让 settled_at 有先后，保证游标可分页
	}

	var earliest time.Time
	if err := pool.QueryRow(ctx, `select min(settled_at) from hourly_snapshot where terminal_id like $1`, prefix+"%").
		Scan(&earliest); err != nil {
		t.Fatalf("读取 settled_at: %v", err)
	}
	since := earliest.Add(-time.Second).Format(time.RFC3339Nano)

	handler := New(pool).Handler()
	fetch := func(rawQuery string) ChangesPage {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/snapshot/changes?"+rawQuery, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", rawQuery, rec.Code, rec.Body.String())
		}
		var resp struct {
			Data ChangesPage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		return resp.Data
	}
	key := func(r ChangeRow) string {
		return r.TerminalID + "/" + r.ItemCode + "@" + r.HourStart.UTC().Format(time.RFC3339)
	}

	// 1) 一次取完：返回行 settled_at 都 > since，且按游标键升序。
	all := fetch("since=" + url.QueryEscape(since) + "&limit=100")
	// 库里可能有其它测试留下的行，只筛本次前缀的。
	var mine []ChangeRow
	for _, r := range all.Rows {
		if strings.HasPrefix(r.TerminalID, prefix) {
			mine = append(mine, r)
		}
	}
	if len(mine) != 5 {
		t.Fatalf("本次写入可见行数 = %d，期望 5", len(mine))
	}
	for _, r := range all.Rows {
		if !r.SettledAt.After(earliest.Add(-time.Second)) {
			t.Fatalf("返回行 settled_at 未大于 since: %s", r.SettledAt)
		}
	}

	// 2) limit=2 逐页取，拼起来与一次取完的（本次前缀）集合一致且不重复。
	seen := map[string]bool{}
	var cursor string
	for page := 0; page < 10; page++ {
		q := "since=" + url.QueryEscape(since) + "&limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		p := fetch(q)
		if len(p.Rows) > 2 {
			t.Fatalf("单页行数 = %d，超过 limit", len(p.Rows))
		}
		for _, r := range p.Rows {
			if !strings.HasPrefix(r.TerminalID, prefix) {
				continue
			}
			k := key(r)
			if seen[k] {
				t.Fatalf("分页出现重复行 %s", k)
			}
			seen[k] = true
		}
		if !p.HasMore {
			break
		}
		cursor = p.Cursor
		if cursor == "" {
			t.Fatal("hasMore=true 时必须给 cursor")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("分页累计行数 = %d，期望 5（不重不漏）", len(seen))
	}

	// 3) revision 与 quality 可见（修正过的格子 revision > 0）。
	first := mine[0]
	var (
		revision int
		quality  int16
	)
	if err := pool.QueryRow(ctx, `select revision, quality from hourly_snapshot
		where terminal_id = $1 and item_code = $2 and hour_start = $3`,
		first.TerminalID, first.ItemCode, first.HourStart).Scan(&revision, &quality); err != nil {
		t.Fatalf("读取 revision: %v", err)
	}
	if revision != first.Revision || quality != first.Quality {
		t.Fatalf("行内 revision/quality 与库不一致: %d/%d vs %d/%d",
			first.Revision, first.Quality, revision, quality)
	}

	// 4) since 取未来时刻 → 空页、hasMore=false、rows 是空数组不是 null。
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano)
	empty := fetch("since=" + url.QueryEscape(future))
	if len(empty.Rows) != 0 || empty.HasMore || empty.Cursor != "" {
		t.Fatalf("未来 since 应返回空页: %+v", empty)
	}
}
