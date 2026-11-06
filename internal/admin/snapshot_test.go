package admin

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 六档步长白名单必须与 0010 的 CHECK 约束同源：改迁移不改这里（或反之）测试会红。
func TestSnapshotStepOptionsMatchDDL(t *testing.T) {
	ddl := string(mustReadMigrations(t, "0010_snapshot_six_granularities.sql"))
	// RE2 没有反向引用，逐档拼正则。
	found := 0
	for _, g := range []string{"second", "minute", "hour", "month"} {
		re := regexp.MustCompile(`check \(` + g + `_step\s+is null or ` + g + `_step\s+in \(([^)]*)\)\)`)
		m := re.FindStringSubmatch(ddl)
		if m == nil {
			t.Fatalf("0010 里没有 %s_step 的白名单 CHECK", g)
		}
		found++
		have := optionValues(stepOptionsOf(g + "_step"))
		for _, token := range strings.Split(m[1], ",") {
			token = strings.TrimSpace(token)
			if !contains(have, token) {
				t.Fatalf("%s_step 的 DDL 白名单包含 %q，但后台下拉没有该选项", g, token)
			}
		}
	}
	if found != 4 {
		t.Fatalf("解析到 %d 组白名单，期望 4（秒/分/时/月）", found)
	}
	// 日/年档在 DDL 里是 = 1，下拉只允许一个值。
	for _, key := range []string{"day_step", "year_step"} {
		if v := optionValues(stepOptionsOf(key)); len(v) != 1 || v[0] != "1" {
			t.Fatalf("%s 选项 = %v，期望 [1]", key, v)
		}
	}
}

// 档位是列不是行：六档各一组 enabled/step；派生列只展示，不可排序/筛选/编辑。
func TestSnapshotPlanFieldSet(t *testing.T) {
	fields, ok := FieldMetas("snapshot-plans")
	if !ok {
		t.Fatal("snapshot-plans 没有字段元数据")
	}
	byKey := map[string]FieldMeta{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for _, g := range []string{"second", "minute", "hour", "day", "month", "year"} {
		enabled, ok := byKey[g+"_enabled"]
		if !ok || enabled.Type != "boolean" {
			t.Fatalf("缺少 %s_enabled 或类型不是 boolean: %+v", g, enabled)
		}
		step, ok := byKey[g+"_step"]
		if !ok || step.Type != "enum" || step.OptionsSource != "static" || len(step.Options) == 0 {
			t.Fatalf("%s_step 不是带静态选项的枚举: %+v", g, step)
		}
	}
	derived, ok := byKey["enabled_steps"]
	if !ok || !derived.Derived {
		t.Fatalf("enabled_steps 应为派生列: %+v", derived)
	}
	if derived.Sortable || derived.Filterable || derived.Editable {
		t.Fatalf("派生列不得可排序/筛选/编辑: %+v", derived)
	}
	// 可写列与 db 白名单一致：六档 12 列 + device_type_id + item_code。
	if len(mustEntity(t, "snapshot-plans").Cols) != 14 {
		t.Fatalf("snapshot-plans 可写列 = %d，期望 14", len(mustEntity(t, "snapshot-plans").Cols))
	}
}

// 列表 SQL 必须带上派生列表达式，且只出现在 SELECT 里。
func TestSnapshotPlanDerivedColumnInListSQL(t *testing.T) {
	e := mustEntity(t, "snapshot-plans")
	sql, _, err := BuildListQuery(e, QueryState{Sort: "id", Order: "desc", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("构造列表 SQL: %v", err)
	}
	for _, want := range []string{"select *", "concat_ws(", "as enabled_steps", "from snapshot_plan"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("列表 SQL 缺少 %q：%s", want, sql)
		}
	}
}

// 提交体里出现六档之外的键 = 想新增档位 → 400（不静默丢字段）。
func TestSnapshotPlanRejectsUnknownGranularity(t *testing.T) {
	handler := New(nil).Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/snapshot-plans",
		strings.NewReader(`{"device_type_id":"1","item_code":"temp","week_enabled":true}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不能新增档位") {
		t.Fatalf("未知档位应 400，实际 %d %s", rec.Code, rec.Body.String())
	}
}

// 保留期行集固定：POST / DELETE 一律 400，只有 PUT 允许改 retention_days。
func TestSnapshotSettingsRejectsCreateAndDelete(t *testing.T) {
	handler := New(nil).Handler()
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/admin/v1/snapshot-settings", `{"retention_days":30}`},
		{http.MethodDelete, "/admin/v1/snapshot-settings/snapshot_coarse", ""},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "fixed_rows") {
			t.Fatalf("%s %s 应 400 fixed_rows，实际 %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// 保留期实体：主键是 table_name，可写列只有 retention_days（否则就是允许改行集本身）。
func TestSnapshotSettingsEntityShape(t *testing.T) {
	e := mustEntity(t, "snapshot-settings")
	if !e.Hidden || !e.FixedRows || e.pk() != "table_name" {
		t.Fatalf("snapshot-settings 属性不符: %+v", e)
	}
	if len(e.Cols) != 1 || e.Cols[0] != "retention_days" {
		t.Fatalf("可写列 = %v，期望 [retention_days]", e.Cols)
	}
}

// 六档步长字典必须下发到 _meta.enums（key 与字段名一致）。
func TestSnapshotStepDictionaries(t *testing.T) {
	dicts := enumDictionaries()
	for _, key := range []string{"second_step", "minute_step", "hour_step", "day_step", "month_step", "year_step"} {
		dict, ok := dicts[key]
		if !ok || len(dict) == 0 {
			t.Fatalf("_meta.enums 缺少 %s", key)
		}
		for value, label := range dict {
			if value == "" || label == "" {
				t.Fatalf("%s 字典含空值: %v", key, dict)
			}
		}
	}
}

// ---------- 小工具 ----------


func stepOptionsOf(field string) []Option {
	fields, _ := FieldMetas("snapshot-plans")
	for _, f := range fields {
		if f.Key == field {
			return f.Options
		}
	}
	return nil
}

func optionValues(options []Option) []string {
	out := make([]string, 0, len(options))
	for _, o := range options {
		out = append(out, o.Value)
	}
	sort.Strings(out)
	return out
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
