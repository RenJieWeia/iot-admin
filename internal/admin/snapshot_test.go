package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 六档步长白名单必须与 0013 的 CHECK 约束同源：改迁移不改这里（或反之）测试会红。
func TestSnapshotStepOptionsMatchDDL(t *testing.T) {
	ddl := string(mustReadMigrations(t, "0013_snapshot_slots_and_bindings.sql"))
	// 0013 是单列 step + CASE：when '<档>' then step is null or step in (...)，日/年为 step = 1。
	found := 0
	for _, g := range []string{"second", "minute", "hour", "month"} {
		re := regexp.MustCompile(`when '` + g + `'\s+then step is null or step in \(([^)]*)\)`)
		m := re.FindStringSubmatch(ddl)
		if m == nil {
			t.Fatalf("0013 里没有 %s 档的步长白名单 CHECK", g)
		}
		found++
		have := optionValues(stepOptionsOf(g + "_step"))
		for _, token := range strings.Split(m[1], ",") {
			token = strings.TrimSpace(token)
			if !contains(have, token) {
				t.Fatalf("%s 档的 DDL 白名单包含 %q，但后台下拉没有该选项", g, token)
			}
		}
	}
	if found != 4 {
		t.Fatalf("解析到 %d 组白名单，期望 4（秒/分/时/月）", found)
	}
	for _, g := range []string{"day", "year"} {
		re := regexp.MustCompile(`when '` + g + `'\s+then step is null or step = 1`)
		if !re.MatchString(ddl) {
			t.Fatalf("0013 里 %s 档的 CHECK 不是 step = 1", g)
		}
		if v := optionValues(stepOptionsOf(g + "_step")); len(v) != 1 || v[0] != "1" {
			t.Fatalf("%s_step 选项 = %v，期望 [1]", g, v)
		}
	}
}

// 六档配置实体：行集固定（granularity 主键），只许改 enabled/step。
func TestSnapshotSlotEntityShape(t *testing.T) {
	e := mustEntity(t, "snapshot-slots")
	if e.Hidden || !e.FixedRows || e.pk() != "granularity" {
		t.Fatalf("snapshot-slots 属性不符（应可导航 + 行集固定 + 主键 granularity）: %+v", e)
	}
	if e.Table != "snapshot_slot_config" {
		t.Fatalf("表名 = %q，期望 snapshot_slot_config", e.Table)
	}
	if strings.Join(e.Cols, ",") != "enabled,step" {
		t.Fatalf("可写列 = %v，期望 [enabled step]（档位本身不可改）", e.Cols)
	}

	fields, ok := FieldMetas("snapshot-slots")
	if !ok {
		t.Fatal("snapshot-slots 没有字段元数据")
	}
	byKey := map[string]FieldMeta{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	if g := byKey["granularity"]; g.Type != "enum" || !g.Required || g.Editable || len(g.Options) != 4 {
		t.Fatalf("granularity 字段不符（应为不可编辑的 4 值枚举）: %+v", g)
	}
	if en := byKey["enabled"]; en.Type != "boolean" || !en.Editable {
		t.Fatalf("enabled 字段不符: %+v", en)
	}
	if st := byKey["step"]; st.Type != "number" || !st.Editable {
		t.Fatalf("step 字段不符: %+v", st)
	}
	if up := byKey["updated_at"]; up.Editable {
		t.Fatalf("updated_at 不应可编辑: %+v", up)
	}
}

// 六档配置的写入体只接受 enabled/step：出现 granularity 之类的键就是「想改主键/加档」→ 400。
func TestSnapshotSlotRejectsUnknownField(t *testing.T) {
	handler := New(nil).Handler()
	for _, tc := range []struct{ method, path, body, want string }{
		{http.MethodPut, "/admin/v1/snapshot-slots/hour", `{"granularity":"week"}`, "只允许改 enabled / step"},
		{http.MethodPost, "/admin/v1/snapshot-slots", `{"enabled":true}`, "fixed_rows"},
		{http.MethodDelete, "/admin/v1/snapshot-slots/hour", ``, "fixed_rows"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s %s 应 400 且含 %q，实际 %d %s", tc.method, tc.path, tc.want, rec.Code, rec.Body.String())
		}
	}
}

// 步长白名单在 Go 侧先拦（比 DB CHECK 的报错好读），档位取自 URL 的 {granularity}。
func TestSnapshotSlotStepWhitelist(t *testing.T) {
	handler := New(nil).Handler() // nil pool：非法步长在碰库之前就被拒
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
		"/admin/v1/snapshot-slots/hour", strings.NewReader(`{"step":5}`)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "步长") {
		t.Fatalf("hour 档 step=5 应 400，实际 %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
		"/admin/v1/snapshot-slots/day", strings.NewReader(`{"step":"2"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("day 档 step=2 应 400，实际 %d %s", rec.Code, rec.Body.String())
	}
}

// 绑定实体：不进导航（Hidden），可增删，created_at 只读。
func TestSnapshotBindingEntityShape(t *testing.T) {
	e := mustEntity(t, "snapshot-bindings")
	if !e.Hidden || e.FixedRows {
		t.Fatalf("snapshot-bindings 应 Hidden 且可增删: %+v", e)
	}
	if e.pk() != "id" || e.Table != "snapshot_binding" {
		t.Fatalf("表/主键不符: %+v", e)
	}
	if strings.Join(e.Cols, ",") != "device_type_id,terminal_id,item_code" {
		t.Fatalf("可写列 = %v，期望 [device_type_id terminal_id item_code]", e.Cols)
	}
	fields, ok := FieldMetas("snapshot-bindings")
	if !ok {
		t.Fatal("snapshot-bindings 没有字段元数据")
	}
	byKey := map[string]FieldMeta{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	if f := byKey["created_at"]; f.Editable {
		t.Fatalf("created_at 不应可编辑: %+v", f)
	}
	for _, key := range []string{"device_type_id", "terminal_id"} {
		if f := byKey[key]; f.Type != "relation" || !f.Editable {
			t.Fatalf("%s 应为可编辑的关联字段: %+v", key, f)
		}
	}
	if target, ok := RelationTarget("snapshot-bindings", "terminal_id"); !ok || target != "terminals" {
		t.Fatalf("terminal_id 的关联目标 = %q，期望 terminals", target)
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

// stepOptionsOf 从 _meta.enums 取某档步长字典（values 顺序不稳定，只用于集合比对）。
func stepOptionsOf(field string) []Option {
	dict, ok := enumDictionaries()[field]
	if !ok {
		return nil
	}
	out := make([]Option, 0, len(dict))
	for value, label := range dict {
		out = append(out, Option{Value: value, Label: label})
	}
	return out
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

// TestUpdateSQLUsesEntityPKey 守住「非 id 主键的实体」不会在 UPDATE 里写死 id。
// 线上曾因此 500：PUT /admin/v1/snapshot-settings/snapshot_second → column "id" does not exist。
func TestUpdateSQLUsesEntityPKey(t *testing.T) {
	e, ok := lookup("snapshot-settings")
	if !ok {
		t.Fatal("snapshot-settings 实体缺失")
	}
	got := updateSQL(e, []string{"retention_days = $1"}, 2)
	want := `update snapshot_settings set retention_days = $1 where table_name::text = $2 returning *`
	if got != want {
		t.Fatalf("update SQL 不符：\n got  %s\n want %s", got, want)
	}
	if strings.Contains(got, "where id::text") {
		t.Fatal("UPDATE 仍在写死 id 列")
	}
}

// 绑定校验（验收 5）：作用域二选一 + item_code 必须属于目标的物模型，错误要能读懂。
func TestSnapshotBindingValidationWithDB(t *testing.T) {
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

	// 造一套独立数据：数据域 → 类型（含 temp 属性）→ 终端。
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from snapshot_binding where item_code like 'bd-%'`)
		_, _ = pool.Exec(ctx, `delete from thing_model_property where type_id in (select id from device_type where code = 'BD-TYPE')`)
		_, _ = pool.Exec(ctx, `delete from terminal where id = 'BD-T1'`)
		_, _ = pool.Exec(ctx, `delete from device_type where code = 'BD-TYPE'`)
		_, _ = pool.Exec(ctx, `delete from data_domain where code = 'BD-DOMAIN'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := pool.Exec(ctx, `insert into data_domain (code, name) values ('BD-DOMAIN', '绑定测试域') on conflict (code) do nothing`); err != nil {
		t.Fatalf("建数据域: %v", err)
	}
	var typeID int64
	if err := pool.QueryRow(ctx, `
		insert into device_type (code, name, domain_id)
		select 'BD-TYPE', '绑定测试类型', id from data_domain where code = 'BD-DOMAIN'
		returning id`).Scan(&typeID); err != nil {
		t.Fatalf("建类型: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into thing_model_property (type_id, item_code, name, value_type)
		values ($1, 'bd-temp', '温度', 'number')`, typeID); err != nil {
		t.Fatalf("建物模型属性: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into terminal (id, type_id, gateway_id, name, enabled) values ('BD-T1', $1, 'BD-GW', '绑定测试终端', true)`,
		typeID); err != nil {
		t.Fatalf("建终端: %v", err)
	}

	handler := New(pool).Handler()
	post := func(body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/v1/snapshot-bindings", strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}

	// 1) 作用域二选一
	if code, body := post(`{"device_type_id":` + strconv.FormatInt(typeID, 10) + `,"terminal_id":"BD-T1","item_code":"bd-temp"}`); code != http.StatusBadRequest ||
		!strings.Contains(body, "二选一") {
		t.Fatalf("两个作用域都给应 400，实际 %d %s", code, body)
	}
	if code, body := post(`{"item_code":"bd-temp"}`); code != http.StatusBadRequest || !strings.Contains(body, "二选一") {
		t.Fatalf("两个作用域都不给应 400，实际 %d %s", code, body)
	}

	// 2) item_code 不属于目标物模型 → 400 且说清「哪个类型下没有哪个数据项」
	code, body := post(`{"device_type_id":` + strconv.FormatInt(typeID, 10) + `,"item_code":"not-declared"}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "not-declared") || !strings.Contains(body, "物模型") {
		t.Fatalf("未声明的数据项应 400 并说明原因，实际 %d %s", code, body)
	}

	// 3) 终端不存在
	if code, body := post(`{"terminal_id":"BD-NOPE","item_code":"bd-temp"}`); code != http.StatusBadRequest ||
		!strings.Contains(body, "不存在") {
		t.Fatalf("不存在的终端应 400，实际 %d %s", code, body)
	}

	// 4) 合法：类型级 + 实例级各一条；重复的类型级绑定被部分唯一索引拦下（400）
	if code, body := post(`{"device_type_id":` + strconv.FormatInt(typeID, 10) + `,"item_code":"bd-temp"}`); code != http.StatusOK {
		t.Fatalf("合法类型级绑定应 200，实际 %d %s", code, body)
	}
	if code, body := post(`{"terminal_id":"BD-T1","item_code":"bd-temp"}`); code != http.StatusOK {
		t.Fatalf("合法实例级绑定应 200，实际 %d %s", code, body)
	}
	if code, body := post(`{"device_type_id":` + strconv.FormatInt(typeID, 10) + `,"item_code":"bd-temp"}`); code != http.StatusBadRequest {
		t.Fatalf("重复的类型级绑定应 400，实际 %d %s", code, body)
	}
}
