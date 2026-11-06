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

	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 只读页定义：slot/表/列固定，且不进 admin.Entities。
func TestReadonlyPagesShape(t *testing.T) {
	pages := ReadonlyPages()
	if len(pages) != 1 {
		t.Fatalf("只读页数 = %d，期望 1（本批只开 latest-values）", len(pages))
	}
	p := pages[0]
	if p.Slot != "latest-values" || p.Entity.Name != "latest-values" || p.Entity.Table != "latest_value" {
		t.Fatalf("只读页定义不符: slot=%q name=%q table=%q", p.Slot, p.Entity.Name, p.Entity.Table)
	}
	wantCols := []string{"terminal_id", "item_code", "value_num", "value_text", "unit", "value_time", "quality", "quality_reason"}
	if len(p.Entity.Cols) != len(wantCols) {
		t.Fatalf("列数 = %d，期望 %d", len(p.Entity.Cols), len(wantCols))
	}
	for i, c := range wantCols {
		if p.Entity.Cols[i] != c {
			t.Fatalf("第 %d 列 = %q，期望 %q", i, p.Entity.Cols[i], c)
		}
	}
	if _, ok := ReadonlyBySlot("latest-values"); !ok {
		t.Fatal("latest-values 应可解析")
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
	if len(payload.Readonly) != 1 {
		t.Fatalf("readonly 段 = %d 项，期望 1", len(payload.Readonly))
	}
	page := payload.Readonly[0]
	if page.Slot != "latest-values" || page.Title != "最新值" || !page.ReadOnly {
		t.Fatalf("readonly 段内容不符: %+v", page)
	}
	if page.Route != "/admin/v1/readonly/latest-values" {
		t.Fatalf("route = %q", page.Route)
	}
	if len(page.Fields) != 8 {
		t.Fatalf("字段数 = %d，期望 8", len(page.Fields))
	}
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
	if len(resp.Data.Readonly) != 1 || resp.Data.Readonly[0].Slot != "latest-values" {
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
