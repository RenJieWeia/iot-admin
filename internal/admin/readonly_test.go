package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 只读页定义：slot/表/列/分页方式固定，且不进 admin.Entities。
// 小时级历史下线后（0009）只读源只剩「最新值」一页。
func TestReadonlyPagesShape(t *testing.T) {
	pages := ReadonlyPages()
	if len(pages) != 4 {
		t.Fatalf("只读页数 = %d，期望 4（最新值 + 三张快照）", len(pages))
	}
	p := pages[0]
	if p.Slot != "latest-values" || p.Entity.Name != "latest-values" ||
		p.Entity.Table != "latest_value" || p.Pagination != PaginationOffset {
		t.Fatalf("latest-values 页定义不符: slot=%q name=%q table=%q pagination=%q",
			p.Slot, p.Entity.Name, p.Entity.Table, p.Pagination)
	}
	wantCols := []string{"terminal_id", "item_code", "value_num", "value_text", "unit", "value_time", "quality", "quality_reason"}
	if len(p.Entity.Cols) != len(wantCols) {
		t.Fatalf("latest-values 列数 = %d，期望 %d", len(p.Entity.Cols), len(wantCols))
	}
	for i, c := range wantCols {
		if p.Entity.Cols[i] != c {
			t.Fatalf("latest-values 第 %d 列 = %q，期望 %q", i, p.Entity.Cols[i], c)
		}
	}
	// 三张快照页：游标分页，列与真实表一致（粗表多一列 granularity）。
	wantSnapshot := []struct {
		slot  string
		table string
		cols  []string
	}{
		{"snapshot-coarse", "snapshot_coarse", []string{
			"terminal_id", "item_code", "granularity", "segment_start", "value_num", "value_text",
			"unit", "value_time", "quality", "quality_reason", "revision", "updated_at"}},
		{"snapshot-minute", "snapshot_minute", []string{
			"terminal_id", "item_code", "segment_start", "value_num", "value_text",
			"unit", "value_time", "quality", "quality_reason", "revision", "updated_at"}},
		{"snapshot-second", "snapshot_second", []string{
			"terminal_id", "item_code", "segment_start", "value_num", "value_text",
			"unit", "value_time", "quality", "quality_reason", "revision", "updated_at"}},
	}
	for _, w := range wantSnapshot {
		pg, ok := ReadonlyPageBySlot(w.slot)
		if !ok {
			t.Fatalf("%s 应可解析", w.slot)
		}
		if pg.Entity.Table != w.table || pg.Pagination != PaginationCursor {
			t.Fatalf("%s 定义不符: table=%q pagination=%q", w.slot, pg.Entity.Table, pg.Pagination)
		}
		if strings.Join(pg.Entity.Cols, ",") != strings.Join(w.cols, ",") {
			t.Fatalf("%s 列 = %v，期望 %v", w.slot, pg.Entity.Cols, w.cols)
		}
	}

	if _, ok := ReadonlyBySlot("latest-values"); !ok {
		t.Fatal("latest-values 应可解析")
	}
	// 已下线的两个 slot 必须解析失败（路由层据此 404）。
	for _, retired := range []string{"hourly-staging", "hourly-snapshot"} {
		if _, ok := ReadonlyBySlot(retired); ok {
			t.Fatalf("%s 已下线，不应再解析成功", retired)
		}
	}
	if sort := defaultSortOf("latest-values"); sort != "value_time" {
		t.Fatalf("latest-values 默认排序 = %q，期望 value_time", sort)
	}
	if _, ok := ReadonlyBySlot("nope"); ok {
		t.Fatal("未知 slot 不应解析成功")
	}
	// 只读源不得混进管理实体（可 CRUD 5 / 可导航 4，见 nongoal_test）。
	if len(Entities) != 7 {
		t.Fatalf("admin.Entities = %d，期望 7", len(Entities))
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

// _meta.readonly：只剩 1 项 8 字段，quality 带 8 种组合；exceptions 段保持 2 项（防回归）。
func TestMetaReadonlySection(t *testing.T) {
	payload := BuildMeta()
	if len(payload.Readonly) != 4 {
		t.Fatalf("readonly 段 = %d 项，期望 4（最新值 + 三张快照）", len(payload.Readonly))
	}
	var latest MetaReadonly
	// 三张快照页必须是 cursor 分页（分区表不报总数）。
	cursors := 0
	for _, p := range payload.Readonly {
		if p.Slot == "latest-values" {
			latest = p
		}
		if strings.HasPrefix(p.Slot, "snapshot-") && p.Pagination == PaginationCursor {
			cursors++
		}
	}
	if cursors != 3 {
		t.Fatalf("快照页 cursor 分页数 = %d，期望 3", cursors)
	}
	page := latest
	if page.Slot != "latest-values" || page.Title != "最新值" || !page.ReadOnly ||
		page.Route != "/admin/v1/readonly/latest-values" || page.Pagination != PaginationOffset ||
		len(page.Fields) != 8 {
		t.Fatalf("latest-values 段不符: %+v", page)
	}
	for _, f := range page.Fields {
		if f.Editable || f.Required {
			t.Fatalf("只读页字段 %q 不应可编辑/必填: editable=%v required=%v", f.Key, f.Editable, f.Required)
		}
	}
	// quality 的 8 种组合由 _meta.options 下发，前端零硬编码。
	var qualityField *MetaField
	for i := range page.Fields {
		if page.Fields[i].Key == "quality" {
			qualityField = &page.Fields[i]
		}
	}
	if qualityField == nil || len(qualityField.Options) != 8 {
		t.Fatalf("quality 字段选项 = %+v，期望 8 种组合", qualityField)
	}
}

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
	if len(resp.Data.Readonly) != 4 || resp.Data.Readonly[0].Slot != "latest-values" {
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

// 游标编解码：粗表带 granularity、其余不带；非法输入必须报错而不是静默从头开始。
func TestSnapshotCursorRoundTrip(t *testing.T) {
	seg := time.Date(2026, 11, 6, 10, 0, 0, 0, staging.Zone)
	simple := snapshotCursor{SegmentStart: seg, TerminalID: "T1", ItemCode: "temp"}

	got, err := decodeSnapshotCursor(encodeSnapshotCursor(simple, false), false)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if !got.SegmentStart.Equal(seg) || got.TerminalID != "T1" || got.ItemCode != "temp" {
		t.Fatalf("往返不一致: %+v", got)
	}

	coarse := snapshotCursor{SegmentStart: seg, TerminalID: "T1", ItemCode: "temp", Granularity: "hour"}
	raw := encodeSnapshotCursor(coarse, true)
	got, err = decodeSnapshotCursor(raw, true)
	if err != nil || got.Granularity != "hour" {
		t.Fatalf("粗表游标往返不一致: %+v / %v", got, err)
	}
	if _, err := decodeSnapshotCursor(raw, false); err == nil {
		t.Fatal("字段数不符应报错（粗表游标不能当简单游标用）")
	}
	if _, err := decodeSnapshotCursor("!!!", false); err == nil {
		t.Fatal("非法 base64 应报错")
	}
}

// 需 DSN：三个快照 slot 的游标翻页不重不漏、粗表按档位筛选、空结果是正常空态。
func TestSnapshotReadonlyWithDB(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("连接 PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("执行迁移: %v", err)
	}

	prefix := "snap-ro-" + t.Name() + "-"
	cleanup := func() {
		for _, table := range []string{"snapshot_coarse", "snapshot_minute", "snapshot_second"} {
			_, _ = pool.Exec(ctx, "delete from "+table+" where terminal_id like $1", prefix+"%")
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().In(staging.Zone)
	hourStart := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, staging.Zone)
	insertCoarse := func(gran string, seg time.Time, num float64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			insert into snapshot_coarse (terminal_id, item_code, granularity, segment_start, value_num, unit, value_time, quality)
			values ($1, 'temp', $2, $3, $4, 'C', $3, 0)`,
			prefix+"T1", gran, seg, num); err != nil {
			t.Fatalf("种入 snapshot_coarse: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		insertCoarse("hour", hourStart.Add(-time.Duration(i)*time.Hour), float64(i))
	}
	insertCoarse("day", time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, staging.Zone), 10)
	insertCoarse("month", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, staging.Zone), 20)
	insertCoarse("year", time.Date(now.Year(), 1, 1, 0, 0, 0, 0, staging.Zone), 30)

	for _, table := range []string{"snapshot_minute", "snapshot_second"} {
		for i := 0; i < 2; i++ {
			if _, err := pool.Exec(ctx, "insert into "+table+
				` (terminal_id, item_code, segment_start, value_num, unit, value_time, quality)
				 values ($1, 'temp', $2, $3, 'C', $2, 0)`,
				prefix+"T1", hourStart.Add(-time.Duration(i)*time.Minute), float64(i)); err != nil {
				t.Fatalf("种入 %s: %v", table, err)
			}
		}
	}

	handler := New(pool).Handler()
	type page struct {
		Rows    []map[string]any `json:"rows"`
		Limit   int              `json:"limit"`
		HasMore bool             `json:"hasMore"`
		Cursor  string           `json:"cursor"`
	}
	fetch := func(slot, raw string) page {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/admin/v1/readonly/"+slot+"?"+raw, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s?%s = %d: %s", slot, raw, rec.Code, rec.Body.String())
		}
		var resp struct {
			Data page `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if resp.Data.Rows == nil {
			t.Fatalf("rows 必须是空数组而不是 null: %s", rec.Body.String())
		}
		return resp.Data
	}
	terminalFilter := "filter.terminal_id.eq=" + url.QueryEscape(prefix+"T1")

	// 1) 粗表游标翻页：5 行，limit=2 → 2/2/1，无重复且按 segment_start 倒序。
	seen := map[string]bool{}
	var firstSeg string
	cursor := ""
	for i := 0; i < 3; i++ {
		raw := terminalFilter + "&limit=2"
		if cursor != "" {
			raw += "&cursor=" + url.QueryEscape(cursor)
		}
		p := fetch("snapshot-coarse", raw)
		if len(p.Rows) == 0 {
			t.Fatalf("第 %d 页不应为空", i+1)
		}
		for _, row := range p.Rows {
			key := row["granularity"].(string) + "|" + row["segment_start"].(string)
			if seen[key] {
				t.Fatalf("游标翻页出现重复行: %s", key)
			}
			seen[key] = true
			seg := row["segment_start"].(string)
			if firstSeg != "" && seg > firstSeg {
				t.Fatalf("分页顺序应 segment_start 倒序: %s > %s", seg, firstSeg)
			}
			firstSeg = seg
		}
		if !p.HasMore {
			break
		}
		cursor = p.Cursor
	}
	if len(seen) != 6 {
		t.Fatalf("翻页共取到 %d 行，期望 6（3 个 hour + day/month/year，不重不漏）", len(seen))
	}

	// 2) 粗表按档位筛选：granularity=hour 只有 3 行。
	hours := fetch("snapshot-coarse", terminalFilter+"&filter.granularity=hour&limit=10")
	if len(hours.Rows) != 3 || hours.HasMore {
		t.Fatalf("granularity=hour 行数 = %d hasMore=%v，期望 3/false", len(hours.Rows), hours.HasMore)
	}
	for _, row := range hours.Rows {
		if row["granularity"] != "hour" {
			t.Fatalf("档位筛选返回了 %v", row["granularity"])
		}
	}

	// 3) 分表 / 秒表各 2 行，limit=1 时 hasMore=true。
	for _, slot := range []string{"snapshot-minute", "snapshot-second"} {
		p := fetch(slot, terminalFilter+"&limit=1")
		if len(p.Rows) != 1 || !p.HasMore || p.Cursor == "" {
			t.Fatalf("%s 首页 rows=%d hasMore=%v cursor=%q", slot, len(p.Rows), p.HasMore, p.Cursor)
		}
		next := fetch(slot, terminalFilter+"&limit=1&cursor="+url.QueryEscape(p.Cursor))
		if len(next.Rows) != 1 || next.HasMore {
			t.Fatalf("%s 第二页 rows=%d hasMore=%v", slot, len(next.Rows), next.HasMore)
		}
	}

	// 4) 空结果是正常空态：分区被保留期清掉后查不到数据不是错误。
	empty := fetch("snapshot-second", "filter.terminal_id.eq="+url.QueryEscape("no-such-terminal"))
	if len(empty.Rows) != 0 || empty.HasMore || empty.Cursor != "" {
		t.Fatalf("空结果应为空数组 + hasMore=false，实际 %+v", empty)
	}

	// 5) 非法游标 / 不支持的 sort 都是 400，不静默从头开始。
	for _, raw := range []string{"cursor=!!!", "sort=segment_start", "order=asc"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/admin/v1/readonly/snapshot-coarse?"+raw, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d，期望 400: %s", raw, rec.Code, rec.Body.String())
		}
	}
}
