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

	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 异常只读页的定义必须固定且不混进 admin.Entities（实体数恒为 6）。
func TestExceptionPagesShape(t *testing.T) {
	pages := ExceptionPages()
	if len(pages) != 1 {
		t.Fatalf("异常页数 = %d，期望 1（异常区间已下线）", len(pages))
	}
	wantSlots := []string{"events"}
	wantNames := []string{"exception-events"}
	for i, p := range pages {
		if p.Slot != wantSlots[i] || p.Entity.Name != wantNames[i] {
			t.Fatalf("异常页[%d] = %s/%s，期望 %s/%s", i, p.Slot, p.Entity.Name, wantSlots[i], wantNames[i])
		}
		if len(p.Entity.Cols) == 0 {
			t.Fatalf("异常页 %q 没有列白名单", p.Slot)
		}
	}

	if ev, ok := ExceptionBySlot("events"); !ok || ev.Table != "exception_count" {
		t.Fatalf("events → %+v/%v，期望 exception_count", ev, ok)
	}
	// 区间页已下线：上面的 len(pages)==1 与 slot/name 断言即覆盖，
	// 这里只保留"未知 slot / 普通实体名不被接受"的通用负向用例。
	if _, ok := ExceptionBySlot("nope"); ok {
		t.Fatal("未知 slot 不应解析成功")
	}
	if _, ok := ExceptionByEntity("terminals"); ok {
		t.Fatal("ExceptionByEntity 不应接受普通实体")
	}

	// 实体集合收敛为 5 个（去掉结算窗口），异常表不得进 Entities。
	if len(Entities) != 5 {
		t.Fatalf("admin.Entities = %d，期望 5", len(Entities))
	}
	for _, e := range Entities {
		if strings.HasPrefix(e.Name, "exception-") {
			t.Fatalf("异常表 %q 不得进入 admin.Entities", e.Name)
		}
	}
}

// 白名单：未知 slot → 404；不可筛选/不可排序列、未知字段与操作符 → 400（都在碰数据库之前拒绝）。
func TestExceptionRoutesWhitelist(t *testing.T) {
	handler := New(nil).Handler() // nil pool：凡是走到 SQL 的用例会 panic，借此证明拒绝发生在查询前
	cases := []struct {
		path   string
		status int
		want   string
	}{
		{"/admin/v1/exceptions/nope", http.StatusNotFound, "unknown_slot"},
		{"/admin/v1/exceptions/events?filter.period.gte=1", http.StatusBadRequest, "未知筛选字段"},
		{"/admin/v1/exceptions/events?filter.error_type.sounds=x", http.StatusBadRequest, "不支持操作符"},
		{"/admin/v1/exceptions/events?filter.count=x", http.StatusBadRequest, "需要数值"},
		{"/admin/v1/exceptions/events?order=sideways", http.StatusBadRequest, "asc 或 desc"},
		{"/admin/v1/exceptions/events?page=x", http.StatusBadRequest, "page 不是整数"},
		{"/admin/v1/exceptions/events?pageSize=x", http.StatusBadRequest, "pageSize 不是整数"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status {
			t.Fatalf("%s = %d，期望 %d: %s", c.path, rec.Code, c.status, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Fatalf("%s 的响应应包含 %q: %s", c.path, c.want, rec.Body.String())
		}
	}
}

// 合法查询状态：异常页与普通实体同一口径（q/sort/order/page/pageSize/filter.*）。
func TestExceptionQueriesParse(t *testing.T) {
	events, _ := ExceptionBySlot("events")
	qs, err := ParseQueryState(events, url.Values{
		"q":                     {"GWX"},
		"filter.error_type":     {"upload_timeout", "protocol_error"},
		"filter.count.gte":      {"2"},
		"filter.hour_start.lte": {"2026-01-02T00:00:00Z"},
		"sort":                  {"count"},
		"order":                 {"desc"},
		"page":                  {"2"},
		"pageSize":              {"20"},
	})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if qs.Page != 2 || qs.PageSize != 20 || qs.Sort != "count" || qs.Order != "desc" {
		t.Fatalf("查询状态不符: %+v", qs)
	}
	if len(qs.Filters) != 3 {
		t.Fatalf("筛选条件数 = %d，期望 3: %+v", len(qs.Filters), qs.Filters)
	}

	// 默认排序：事件表没有 id 列，取第一个可排序时间列 hour_start 倒序。
	qs, err = ParseQueryState(events, url.Values{})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if qs.Sort != "hour_start" || qs.Order != "desc" {
		t.Fatalf("异常事件默认排序 = %s %s，期望 hour_start desc", qs.Sort, qs.Order)
	}
	if _, ok := FieldMetaOf("exception-events", "id"); ok {
		t.Fatal("exception_count 没有 id 列，字段元数据不得虚拟 id")
	}
}

// 无 id 列的表靠全列 tie-breaker 保证分页不重不漏。
func TestExceptionListQueryShape(t *testing.T) {
	events, _ := ExceptionBySlot("events")
	qs, err := ParseQueryState(events, url.Values{"filter.error_type": {"upload_timeout"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	sql, args, err := BuildListQuery(events, qs)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.Contains(sql, "select * from exception_count") {
		t.Fatalf("列表 SQL 数据源不符: %s", sql)
	}
	if !strings.Contains(sql, "order by hour_start desc, terminal_id asc, gateway_id asc") {
		t.Fatalf("无 id 列时应用全列 tie-breaker: %s", sql)
	}
	for _, item := range strings.Split(orderClauseOf(t, sql), ", ") {
		if strings.Fields(item)[0] == "id" {
			t.Fatalf("exception_count 没有 id 列，tie-breaker 不应出现 id: %s", sql)
		}
	}
	if strings.Count(sql, "hour_start") != 1 {
		t.Fatalf("已排序字段不应在 tie-breaker 里重复出现: %s", sql)
	}
	if len(args) != 3 {
		t.Fatalf("参数 = %v，期望 [筛选值 pageSize offset]", args)
	}

	countSQL, countArgs, err := BuildCountQuery(events, qs)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.Contains(countSQL, "select count(*) from exception_count where error_type::text = any($1)") {
		t.Fatalf("总数 SQL 应与列表共用 where: %s", countSQL)
	}
	if len(countArgs) != 1 {
		t.Fatalf("总数参数 = %v，期望 1 个", countArgs)
	}
}

// 需 DSN：异常两表的真实查询（列表 + 总数 + 排序 + 分页）。
func TestExceptionRowsWithDB(t *testing.T) {
	dsn := testDSN(t) // 每包独立测试库（task-006 / R3），避免并行跑包互踩
	ctx := context.Background()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("连接 PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("执行迁移: %v", err)
	}
	// 每次运行用独立的终端前缀：同一 DSN 上并行跑同一用例（另一个进程/worktree）互不删数据。
	prefix := fmt.Sprintf("qtest-ex-%d-", os.Getpid())
	termA, termB := prefix+"a", prefix+"b"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from exception_count where terminal_id like $1`, prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	countRows := []struct {
		terminal, gateway, hour, kind string
		count                         int
	}{
		{termA, "GWX", "2026-01-01T00:00:00Z", "upload_timeout", 1},
		{termA, "GWX", "2026-01-01T01:00:00Z", "upload_timeout", 3},
		{termB, "GWX", "2026-01-01T00:00:00Z", "protocol_error", 2},
	}
	for _, r := range countRows {
		if _, err := pool.Exec(ctx,
			`insert into exception_count (terminal_id, gateway_id, hour_start, error_type, count, first_seen, last_seen)
			 values ($1, $2, $3::timestamptz, $4, $5, $3::timestamptz, $3::timestamptz)`,
			r.terminal, r.gateway, r.hour, r.kind, r.count); err != nil {
			t.Fatalf("建异常事件 %s: %v", r.kind, err)
		}
	}
	handler := New(pool).Handler()
	fetch := func(path string) ([]map[string]any, ListMeta) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", path, rec.Code, rec.Body.String())
		}
		var resp struct {
			Code int              `json:"code"`
			Data []map[string]any `json:"data"`
			Meta *ListMeta        `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s 响应不是合法 JSON: %v", path, err)
		}
		if resp.Data == nil || resp.Meta == nil {
			t.Fatalf("%s 响应缺少 data 数组或 meta: %s", path, rec.Body.String())
		}
		return resp.Data, *resp.Meta
	}

	// 1) 按终端筛选 + 时间升序：两行，首行 count=1。
	data, meta := fetch("/admin/v1/exceptions/events?filter.terminal_id.eq=" + termA + "&sort=hour_start&order=asc")
	if meta.Total != 2 || len(data) != 2 {
		t.Fatalf("filter.terminal_id=%s → total=%d rows=%d，期望 2/2", termA, meta.Total, len(data))
	}
	if got, _ := data[0]["count"].(float64); got != 1 {
		t.Fatalf("首行 count = %v，期望 1", data[0]["count"])
	}
	if got, _ := data[0]["error_type"].(string); got != "upload_timeout" {
		t.Fatalf("首行 error_type = %v", data[0]["error_type"])
	}
	// 时间列按 RFC3339 带时区偏移下发（会话时区可能是 +08:00），前端据此展示绝对时间。
	ts, _ := data[0]["hour_start"].(string)
	parsed, parseErr := time.Parse(time.RFC3339Nano, ts)
	if parseErr != nil {
		t.Fatalf("hour_start = %v，期望 RFC3339 时间串: %v", data[0]["hour_start"], parseErr)
	}
	if !parsed.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("hour_start = %s，期望 2026-01-01T00:00:00Z", ts)
	}

	// 2) 按 count 倒序 + 分页：第 2 页只剩一行。
	data, meta = fetch("/admin/v1/exceptions/events?filter.terminal_id.eq=" + termA + "&sort=count&order=desc&page=2&pageSize=1")
	if meta.Total != 2 || len(data) != 1 {
		t.Fatalf("第 2 页 total=%d rows=%d，期望 2/1", meta.Total, len(data))
	}
	if got, _ := data[0]["count"].(float64); got != 1 {
		t.Fatalf("第 2 页 count = %v，期望 1", data[0]["count"])
	}

	// 3) 枚举 in 筛选命中另一终端（同时用本次运行的前缀限域，避免并行运行互相看见）。
	data, meta = fetch("/admin/v1/exceptions/events?filter.terminal_id.eq=" + termB + "&filter.error_type=protocol_error")
	if meta.Total != 1 || len(data) != 1 {
		t.Fatalf("filter.error_type=protocol_error → total=%d rows=%d，期望 1/1", meta.Total, len(data))
	}
	if got, _ := data[0]["count"].(float64); got != 2 {
		t.Fatalf("count = %v，期望 2", data[0]["count"])
	}

}

// orderClauseOf 取出 SQL 里 order by 之后、limit 之前的部分，供逐项断言。
func orderClauseOf(t *testing.T, sql string) string {
	t.Helper()
	i := strings.Index(sql, " order by ")
	if i < 0 {
		t.Fatalf("SQL 缺少 order by: %s", sql)
	}
	rest := sql[i+len(" order by "):]
	if j := strings.Index(rest, " limit "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
