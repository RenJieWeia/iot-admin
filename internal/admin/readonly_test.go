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

// 只读页定义：slot/表/列/分页方式固定，且不进 admin.Entities。
func TestReadonlyPagesShape(t *testing.T) {
	pages := ReadonlyPages()
	if len(pages) != 3 {
		t.Fatalf("只读页数 = %d，期望 3（最新值 + 小时暂存 + 小时快照）", len(pages))
	}
	want := []struct {
		slot, name, table, pagination string
		cols                          []string
	}{
		{"latest-values", "latest-values", "latest_value", PaginationOffset,
			[]string{"terminal_id", "item_code", "value_num", "value_text", "unit", "value_time", "quality", "quality_reason"}},
		{"hourly-staging", "hourly-staging", "hourly_staging", PaginationOffset,
			[]string{"terminal_id", "item_code", "hour_start", "value_num", "value_text", "unit", "value_time", "quality", "quality_reason"}},
		{"hourly-snapshot", "hourly-snapshot", "hourly_snapshot", PaginationCursor,
			[]string{"terminal_id", "item_code", "hour_start", "value_num", "value_text", "unit", "value_time",
				"quality", "quality_reason", "revision", "settled_at"}},
	}
	for i, w := range want {
		p := pages[i]
		if p.Slot != w.slot || p.Entity.Name != w.name || p.Entity.Table != w.table || p.Pagination != w.pagination {
			t.Fatalf("第 %d 页定义不符: slot=%q name=%q table=%q pagination=%q",
				i, p.Slot, p.Entity.Name, p.Entity.Table, p.Pagination)
		}
		if len(p.Entity.Cols) != len(w.cols) {
			t.Fatalf("%s 列数 = %d，期望 %d", w.slot, len(p.Entity.Cols), len(w.cols))
		}
		for j, c := range w.cols {
			if p.Entity.Cols[j] != c {
				t.Fatalf("%s 第 %d 列 = %q，期望 %q", w.slot, j, p.Entity.Cols[j], c)
			}
		}
		if _, ok := ReadonlyBySlot(w.slot); !ok {
			t.Fatalf("%s 应可解析", w.slot)
		}
	}
	// 快照页默认排序必须是 settled_at（命中 settled_idx），不能被字段顺序推导成 hour_start。
	if sort := defaultSortOf("latest-values"); sort != "value_time" {
		t.Fatalf("latest-values 默认排序 = %q，期望 value_time", sort)
	}
	if p, _ := ReadonlyPageBySlot("hourly-snapshot"); p.DefaultSort != "settled_at" {
		t.Fatalf("hourly-snapshot 默认排序覆盖 = %q，期望 settled_at", p.DefaultSort)
	}
	if _, ok := ReadonlyBySlot("nope"); ok {
		t.Fatal("未知 slot 不应解析成功")
	}
	// 只读源不得混进管理实体（nongoal_test 断言实体数为 6）。
	if len(Entities) != 6 {
		t.Fatalf("admin.Entities = %d，期望 6", len(Entities))
	}
	for _, e := range Entities {
		if e.Name == "latest-values" {
			t.Fatal("只读源不得进入 admin.Entities")
		}
	}
}

// 未知 slot 404；只接受 GET。
func TestReadonlyRouteWhitelist(t *testing.T) {
	handler := New(nil).Handler() // nil pool：404 分支在碰数据库之前返回
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/readonly/nope", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown_slot") {
		t.Fatalf("未知 slot = %d %s，期望 404 unknown_slot", rec.Code, rec.Body.String())
	}
	// 只有一段时落回 {entity} 白名单 → 404 unknown_entity。
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/readonly", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/admin/v1/readonly = %d，期望 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/v1/readonly/latest-values", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("POST 只读源不应成功: %s", rec.Body.String())
	}
}

// _meta.readonly：1 项 8 字段，quality 带 8 种组合；exceptions 段保持 2 项（防回归）。
func TestMetaReadonlySection(t *testing.T) {
	payload := BuildMeta()
	if len(payload.Readonly) != 3 {
		t.Fatalf("readonly 段 = %d 项，期望 3", len(payload.Readonly))
	}
	bySlot := map[string]MetaReadonly{}
	for _, p := range payload.Readonly {
		bySlot[p.Slot] = p
	}
	if p := bySlot["latest-values"]; p.Title != "最新值" || !p.ReadOnly ||
		p.Route != "/admin/v1/readonly/latest-values" || p.Pagination != PaginationOffset ||
		len(p.Fields) != 8 {
		t.Fatalf("latest-values 段不符: %+v", p)
	}
	if p := bySlot["hourly-staging"]; p.Title != "小时暂存" || p.Pagination != PaginationOffset ||
		p.DefaultSort != "hour_start" || len(p.Fields) != 9 {
		t.Fatalf("hourly-staging 段不符: %+v", p)
	}
	if p := bySlot["hourly-snapshot"]; p.Title != "小时快照" || p.Pagination != PaginationCursor ||
		p.DefaultSort != "settled_at" || len(p.Fields) != 11 {
		t.Fatalf("hourly-snapshot 段不符: %+v", p)
	}
	for _, key := range []string{"revision", "settled_at"} {
		found := false
		for _, f := range bySlot["hourly-snapshot"].Fields {
			if f.Key == key {
				found = true
			}
		}
		if !found {
			t.Fatalf("hourly-snapshot 缺少字段 %q", key)
		}
	}
	page := bySlot["latest-values"]
	for _, f := range page.Fields {
		if f.Editable || f.Required {
			t.Fatalf("只读页字段 %q 不应可编辑/必填: editable=%v required=%v", f.Key, f.Editable, f.Required)
		}
	}
	var quality *MetaField
	for i := range page.Fields {
		if page.Fields[i].Key == "quality" {
			quality = &page.Fields[i]
		}
	}
	if quality == nil {
		t.Fatal("缺少 quality 字段")
	}
	if len(quality.Options) != 8 {
		t.Fatalf("quality.options = %d 项，期望 8", len(quality.Options))
	}
	wantLabels := map[string]string{
		"0": "正常", "1": "越界", "2": "规则命中", "3": "越界+规则",
		"4": "单位闭集外", "5": "越界+单位闭集外", "6": "规则命中+单位闭集外", "7": "越界+规则+单位闭集外",
	}
	for _, o := range quality.Options {
		if wantLabels[o.Value] != o.Label {
			t.Fatalf("quality 取值 %q 的标签 = %q，期望 %q", o.Value, o.Label, wantLabels[o.Value])
		}
		if o.Value == "0" && o.Tone != "neutral" {
			t.Fatalf("quality=0 的 Tone = %q，期望 neutral", o.Tone)
		}
		if o.Value != "0" && o.Tone != "warning" {
			t.Fatalf("quality=%s 的 Tone = %q，期望 warning", o.Value, o.Tone)
		}
	}
	// 常驻筛选：quality 在位，value_num/value_text/unit/quality_reason 进高级筛选。
	resident := map[string]bool{}
	advanced := map[string]bool{}
	for _, f := range page.Fields {
		if !f.Filterable {
			continue
		}
		if f.AdvancedOnly {
			advanced[f.Key] = true
		} else {
			resident[f.Key] = true
		}
	}
	for _, key := range []string{"terminal_id", "item_code", "value_time", "quality"} {
		if !resident[key] {
			t.Fatalf("%q 应为常驻筛选，实际常驻=%v 高级=%v", key, resident, advanced)
		}
	}
	for _, key := range []string{"value_num", "value_text", "unit", "quality_reason"} {
		if !advanced[key] {
			t.Fatalf("%q 应为高级筛选，实际常驻=%v 高级=%v", key, resident, advanced)
		}
	}
	if payload.Exceptions == nil || len(payload.Exceptions) != 2 {
		t.Fatalf("exceptions 段应保持 2 项（契约不变），实际 %d", len(payload.Exceptions))
	}
}

// nil pool：_meta 仍 200 且带 readonly 段。
func TestMetaReadonlyWorksWithoutPool(t *testing.T) {
	rec := httptest.NewRecorder()
	New(nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/_meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil pool 下 _meta = %d，期望 200", rec.Code)
	}
	var resp struct {
		Data struct {
			Readonly []struct {
				Slot string `json:"slot"`
			} `json:"readonly"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(resp.Data.Readonly) != 3 || resp.Data.Readonly[0].Slot != "latest-values" {
		t.Fatalf("readonly 段不符: %+v", resp.Data.Readonly)
	}
}

// 需 DSN：列表 + 分页 + meta.total；filter.quality.gte=1 只返回 quality>=1。
func TestReadonlyListWithDB(t *testing.T) {
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
	prefix := "ro-" + t.Name() + "-"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from latest_value where terminal_id like $1`, prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	seed := []struct {
		code    string
		num     float64
		quality int16
		reason  string
	}{
		{"clean", 10.0, 0, ""},
		{"range", 20.0, 1, "source=thing_model item=range 越界"},
		{"rule", 30.0, 2, "source=rule item=rule 规则命中"},
		{"both", 40.0, 3, "source=thing_model item=both 越界; source=rule item=both 规则命中"},
	}
	for _, s := range seed {
		if _, err := pool.Exec(ctx, `
			insert into latest_value (terminal_id, item_code, value_num, unit, value_time, quality, quality_reason)
			values ($1, $2, $3, 'C', now(), $4, nullif($5, ''))`,
			prefix+"T1", s.code, s.num, s.quality, s.reason); err != nil {
			t.Fatalf("种入 latest_value: %v", err)
		}
	}

	handler := New(pool).Handler()
	fetch := func(rawQuery string) ([]map[string]any, ListMeta) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/admin/v1/readonly/latest-values?"+rawQuery, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", rawQuery, rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []map[string]any `json:"data"`
			Meta *ListMeta        `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if resp.Meta == nil {
			t.Fatalf("缺少 meta: %s", rec.Body.String())
		}
		return resp.Data, *resp.Meta
	}

	// 1) 分页：pageSize=2 时返回 2 行，meta.total 为全量。
	rows, meta := fetch("filter.terminal_id.eq=" + url.QueryEscape(prefix+"T1") + "&pageSize=2")
	if meta.Total != int64(len(seed)) || len(rows) != 2 {
		t.Fatalf("分页 total=%d rows=%d，期望 %d/2", meta.Total, len(rows), len(seed))
	}

	// 2) 质量筛选：gte=1 只返回 quality>=1。
	rows, meta = fetch("filter.terminal_id.eq=" + url.QueryEscape(prefix+"T1") + "&filter.quality.gte=1&pageSize=10")
	if meta.Total != 3 || len(rows) != 3 {
		t.Fatalf("quality>=1 total=%d rows=%d，期望 3/3", meta.Total, len(rows))
	}
	for _, r := range rows {
		if q, _ := r["quality"].(float64); q < 1 {
			t.Fatalf("quality 筛选返回了 quality=%v", r["quality"])
		}
	}
	// gte=2 时只剩 2、3 两行。
	rows, _ = fetch("filter.terminal_id.eq=" + url.QueryEscape(prefix+"T1") + "&filter.quality.gte=2&pageSize=10")
	if len(rows) != 2 {
		t.Fatalf("quality>=2 rows=%d，期望 2", len(rows))
	}

	// 3) 行里带 quality/quality_reason（读路径确实暴露两列）。
	rows, _ = fetch("filter.terminal_id.eq=" + url.QueryEscape(prefix+"T1") + "&filter.quality.gte=3&pageSize=10")
	if len(rows) != 1 {
		t.Fatalf("quality>=3 rows=%d，期望 1", len(rows))
	}
	if reason, _ := rows[0]["quality_reason"].(string); reason == "" {
		t.Fatalf("quality_reason 应为非空: %+v", rows[0])
	}
}

// 游标页参数校验：非法 cursor / 不支持的 sort、order 都在碰数据库之前 400。
func TestReadonlyCursorValidation(t *testing.T) {
	handler := New(nil).Handler()
	cases := []struct {
		path string
		want string
	}{
		{"/admin/v1/readonly/hourly-snapshot?cursor=@@@", "cursor 非法"},
		{"/admin/v1/readonly/hourly-snapshot?sort=hour_start", "不支持 sort/order"},
		{"/admin/v1/readonly/hourly-snapshot?order=asc", "不支持 sort/order"},
		{"/admin/v1/readonly/hourly-snapshot?limit=x", "limit 不是整数"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d，期望 400: %s", c.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Fatalf("%s 的响应应包含 %q: %s", c.path, c.want, rec.Body.String())
		}
	}
	// offset 页不受"固定排序"限制：sort 走既有白名单校验（未知字段仍 400，但理由不同）。
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/readonly/latest-values?sort=nope", nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "未知排序字段") {
		t.Fatalf("offset 页 sort 应走白名单校验，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

// 需 DSN：快照游标分页（不重不漏、nextCursor 可回放、limit 收敛、revision 筛选）。
func TestReadonlyCursorWithDB(t *testing.T) {
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
	hour := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	if err := settle.EnsurePartitions(ctx, pool, hour.Add(24*time.Hour)); err != nil {
		t.Fatalf("建分区: %v", err)
	}
	prefix := fmt.Sprintf("cur-%d-", os.Getpid())
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from hourly_snapshot where terminal_id like $1`, prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	upsert := func(rows ...staging.Row) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := settle.UpsertSnapshot(ctx, tx, rows)
			return err
		}); err != nil {
			t.Fatalf("写快照: %v", err)
		}
	}
	num := func(v float64) *float64 { return &v }
	// 5 行，逐条写入以获得不同的 settled_at（游标分页的排序键）。
	for i := 0; i < 5; i++ {
		upsert(staging.Row{
			Value: store.Value{
				TerminalID: fmt.Sprintf("%s%d", prefix, i%2), ItemCode: fmt.Sprintf("item%d", i),
				ValueNum: num(float64(i)), Unit: "C", ValueTime: hour.Add(time.Duration(i) * time.Minute),
			},
			HourStart: hour,
		})
	}
	// 同一格再写更晚的 value_time → revision=1（给 revision.gte=1 用例准备样本）。
	upsert(staging.Row{
		Value: store.Value{
			TerminalID: prefix + "0", ItemCode: "item0", ValueNum: num(99),
			Unit: "C", ValueTime: hour.Add(30 * time.Minute),
		},
		HourStart: hour,
	})

	handler := New(pool).Handler()
	type cursorPage struct {
		Rows []map[string]any `json:"data"`
		Meta *struct {
			HasMore    bool   `json:"hasMore"`
			NextCursor string `json:"nextCursor"`
			Limit      int    `json:"limit"`
		} `json:"meta"`
		Raw map[string]json.RawMessage `json:"-"`
	}
	fetch := func(rawQuery string) (cursorPage, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/admin/v1/readonly/hourly-snapshot?"+rawQuery, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", rawQuery, rec.Code, rec.Body.String())
		}
		var page cursorPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if page.Meta == nil {
			t.Fatalf("缺少 meta: %s", rec.Body.String())
		}
		return page, rec.Body.String()
	}
	mine := func(rows []map[string]any) []string {
		out := []string{}
		for _, r := range rows {
			if t, _ := r["terminal_id"].(string); strings.HasPrefix(t, prefix) {
				out = append(out, fmt.Sprintf("%s/%s", t, r["item_code"]))
			}
		}
		return out
	}
	scope := "filter.terminal_id.contains=" + url.QueryEscape(prefix)

	// 1) 单页：meta 只有 hasMore/nextCursor/limit，没有 total。
	page, raw := fetch(scope + "&limit=100")
	if _, hasTotal := strings.Contains(raw, `"total"`), false; hasTotal {
		t.Fatalf("游标页不应返回 total: %s", raw)
	}
	if page.Meta.Limit != 100 || page.Meta.HasMore {
		t.Fatalf("单页 meta = %+v，期望 limit=100 hasMore=false", page.Meta)
	}
	if got := len(mine(page.Rows)); got != 5 {
		t.Fatalf("本次写入可见行数 = %d，期望 5", got)
	}

	// 2) limit=2 逐页翻：不重不漏，最后一页 hasMore=false。
	seen := map[string]bool{}
	var cursor string
	pages := 0
	for i := 0; i < 10; i++ {
		q := scope + "&limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		p, _ := fetch(q)
		pages++
		if len(p.Rows) > 2 {
			t.Fatalf("单页行数 = %d，超过 limit", len(p.Rows))
		}
		for _, k := range mine(p.Rows) {
			if seen[k] {
				t.Fatalf("分页出现重复行 %s", k)
			}
			seen[k] = true
		}
		if !p.Meta.HasMore {
			break
		}
		cursor = p.Meta.NextCursor
		if cursor == "" {
			t.Fatal("hasMore=true 时必须给 nextCursor")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("分页累计行数 = %d，期望 5（不重不漏）", len(seen))
	}
	if pages < 3 {
		t.Fatalf("5 行按 limit=2 至少 3 页，实际 %d 页", pages)
	}

	// 3) nextCursor 可回放：同一 cursor 连续取两次结果完全一致（幂等）。
	first, _ := fetch(scope + "&limit=2")
	if !first.Meta.HasMore {
		t.Fatal("首屏应还有下一页")
	}
	replayA, _ := fetch(scope + "&limit=2&cursor=" + url.QueryEscape(first.Meta.NextCursor))
	replayB, _ := fetch(scope + "&limit=2&cursor=" + url.QueryEscape(first.Meta.NextCursor))
	if fmt.Sprint(mine(replayA.Rows)) != fmt.Sprint(mine(replayB.Rows)) {
		t.Fatalf("游标回放不一致: %v vs %v", mine(replayA.Rows), mine(replayB.Rows))
	}
	for _, k := range mine(replayA.Rows) {
		for _, k0 := range mine(first.Rows) {
			if k == k0 {
				t.Fatalf("第 2 页与第 1 页重叠: %s", k)
			}
		}
	}

	// 4) limit>1000 收敛到 1000。
	big, _ := fetch(scope + "&limit=5000")
	if big.Meta.Limit != ChangesMaxLimit {
		t.Fatalf("limit 上限 = %d，期望 %d", big.Meta.Limit, ChangesMaxLimit)
	}

	// 5) revision.gte=1 只返回被修正过的行。
	rev, _ := fetch(scope + "&filter.revision.gte=1&limit=100")
	rows := mine(rev.Rows)
	if len(rows) != 1 || rows[0] != prefix+"0/item0" {
		t.Fatalf("revision>=1 行 = %v，期望 [%s]", rows, prefix+"0/item0")
	}
	if v, _ := rev.Rows[0]["revision"].(float64); v < 1 {
		t.Fatalf("revision 字段 = %v，期望 >= 1", rev.Rows[0]["revision"])
	}
}

// 需 DSN：staging 页仍是 offset 分页（meta.total 在）。
func TestReadonlyStagingOffsetWithDB(t *testing.T) {
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
	prefix := fmt.Sprintf("stg-%d-", os.Getpid())
	hour := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from hourly_staging where terminal_id like $1`, prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)
	num := func(v float64) *float64 { return &v }
	rows := make([]staging.Row, 0, 3)
	for i := 0; i < 3; i++ {
		rows = append(rows, staging.Row{
			Value: store.Value{
				TerminalID: prefix + "T", ItemCode: fmt.Sprintf("it%d", i), ValueNum: num(float64(i)),
				Unit: "C", ValueTime: hour.Add(time.Duration(i) * time.Minute),
			},
			HourStart: hour,
		})
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return staging.Upsert(ctx, tx, rows)
	}); err != nil {
		t.Fatalf("写暂存: %v", err)
	}

	handler := New(pool).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/v1/readonly/hourly-staging?filter.terminal_id.eq="+url.QueryEscape(prefix+"T")+"&pageSize=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("staging 页 = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []map[string]any `json:"data"`
		Meta *ListMeta        `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.Meta == nil || resp.Meta.Total != 3 {
		t.Fatalf("staging meta = %+v，期望 total=3", resp.Meta)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("staging 首页行数 = %d，期望 2", len(resp.Data))
	}
	if resp.Meta.Sort != "hour_start" {
		t.Fatalf("staging 默认排序 = %q，期望 hour_start", resp.Meta.Sort)
	}
}
