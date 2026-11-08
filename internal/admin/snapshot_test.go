package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
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
	// 粗档只允许自然单位步长：hour=2、month=2 都必须拒绝。
	for _, tc := range []struct{ path, body string }{
		{"/admin/v1/snapshot-slots/hour", `{"step":2}`},
		{"/admin/v1/snapshot-slots/month", `{"step":2}`},
	} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "步长") {
			t.Fatalf("%s %s 应 400，实际 %d %s", tc.path, tc.body, rec.Code, rec.Body.String())
		}
	}
}

// 绑定实体：不进导航（Hidden），可增删，item_codes 是 jsonb 数组，列表带派生列「字段数」。
func TestSnapshotBindingEntityShape(t *testing.T) {
	e := mustEntity(t, "snapshot-bindings")
	if !e.Hidden || e.FixedRows {
		t.Fatalf("snapshot-bindings 应 Hidden 且可增删: %+v", e)
	}
	if e.pk() != "id" || e.Table != "snapshot_binding" {
		t.Fatalf("表/主键不符: %+v", e)
	}
	if strings.Join(e.Cols, ",") != "device_type_id,terminal_id,item_codes" {
		t.Fatalf("可写列 = %v，期望 [device_type_id terminal_id item_codes]", e.Cols)
	}
	if !e.isJSONB("item_codes") {
		t.Fatal("item_codes 必须登记为 JSONB（写库走 ::jsonb 转换）")
	}
	if len(e.ExtraSelect) != 1 || !strings.Contains(e.ExtraSelect[0], "jsonb_array_length(item_codes)") {
		t.Fatalf("派生列应为 jsonb_array_length(item_codes): %v", e.ExtraSelect)
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
	if f := byKey["item_codes"]; !f.Editable || f.Type != "json" {
		t.Fatalf("item_codes 应为可编辑的 json 字段: %+v", f)
	}
	derived, ok := byKey["fields_count"]
	if !ok || !derived.Derived {
		t.Fatalf("fields_count 应为派生列: %+v", derived)
	}
	if derived.Sortable || derived.Filterable || derived.Editable {
		t.Fatalf("派生列不得可排序/筛选/编辑: %+v", derived)
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

// 列表 SQL 必须带上派生列表达式（只出现在 SELECT，不进排序/筛选）。
func TestSnapshotBindingDerivedColumnInListSQL(t *testing.T) {
	e := mustEntity(t, "snapshot-bindings")
	sql, _, err := BuildListQuery(e, QueryState{Sort: "id", Order: "desc", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("构造列表 SQL: %v", err)
	}
	for _, want := range []string{"select *", "jsonb_array_length(item_codes) as fields_count", "from snapshot_binding"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("列表 SQL 缺少 %q：%s", want, sql)
		}
	}
}

// 绑定用例的公共夹具：造一个独立的域 / 类型 / 物模型 / 终端，用完整体清掉。
// 不依赖库里已有的 id（测试库的 identity 值随执行顺序漂移），只依赖自己造的数据。
type bindingFixture struct {
	domainCode string
	typeCode   string
	typeID     int64
	terminalID string
}

func newBindingFixture(t *testing.T, pool *pgxpool.Pool, ctx context.Context, tag string) bindingFixture {
	t.Helper()
	f := bindingFixture{
		domainCode: "FX-" + tag + "-DOMAIN",
		typeCode:   "FX-" + tag + "-TYPE",
		terminalID: "FX-" + tag + "-T1",
	}
	// 先清后建：重跑用例不会因为残留行失败。
	f.cleanup(ctx, pool)
	t.Cleanup(func() { f.cleanup(ctx, pool) })

	if _, err := pool.Exec(ctx, `insert into data_domain (code, name) values ($1, $2)`,
		f.domainCode, "绑定夹具域 "+tag); err != nil {
		t.Fatalf("建数据域: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into device_type (code, name, domain_id)
		select $1, $2, id from data_domain where code = $3
		returning id`, f.typeCode, "绑定夹具类型 "+tag, f.domainCode).Scan(&f.typeID); err != nil {
		t.Fatalf("建类型: %v", err)
	}
	for _, code := range []string{"temp", "hum"} {
		if _, err := pool.Exec(ctx, `
			insert into thing_model_property (type_id, item_code, name, value_type)
			values ($1, $2, $2, 'number')`, f.typeID, code); err != nil {
			t.Fatalf("建物模型属性: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
		insert into terminal (id, type_id, gateway_id, name, enabled)
		values ($1, $2, 'FX-GW', $1, true)`, f.terminalID, f.typeID); err != nil {
		t.Fatalf("建终端: %v", err)
	}
	return f
}

// cleanup 按外键方向反着删：绑定 → 终端 → 物模型 → 类型 → 域。
func (f bindingFixture) cleanup(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `delete from snapshot_binding where terminal_id = $1 or device_type_id in (select id from device_type where code = $2)`, f.terminalID, f.typeCode)
	_, _ = pool.Exec(ctx, `delete from terminal where id = $1`, f.terminalID)
	_, _ = pool.Exec(ctx, `delete from device_type where code = $1`, f.typeCode)
	_, _ = pool.Exec(ctx, `delete from data_domain where code = $1`, f.domainCode)
}

// 列表响应里必须真的出现 fields_count（前端读的就是这个键）。
// 只断言 SQL 不够——别名写错时 SQL 里看着对、返回的行里却是另一个键。
func TestSnapshotBindingListReturnsFieldsCountWithDB(t *testing.T) {
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

	f := newBindingFixture(t, pool, ctx, "FC")
	if _, err := pool.Exec(ctx, `
		insert into snapshot_binding (device_type_id, item_codes)
		values ($1, '["temp","hum"]'::jsonb)`, f.typeID); err != nil {
		t.Fatalf("建绑定: %v", err)
	}

	api := New(pool)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/snapshot-bindings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表返回 %d：%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(payload.Data) == 0 {
		t.Fatal("列表为空，无法验证 fields_count")
	}
	row := payload.Data[0]
	got, ok := row["fields_count"]
	if !ok {
		t.Fatalf("列表行里没有 fields_count，实际键：%v", keysOf(row))
	}
	if n, _ := got.(float64); int(n) != 2 {
		t.Fatalf("fields_count = %v，期望 2", got)
	}
	if _, hasOld := row["item_count"]; hasOld {
		t.Fatal("旧别名 item_count 不应再出现")
	}
}

// 编辑切作用域必须真的把另一列写成 NULL（真库）。
//
// 前端用例只能证明"提交体两列带齐、另一列为 null"，证明不了服务端照做：
// 后端 update 会把库里旧作用域列合并进校验视图，如果 columns() 把 null 丢掉，
// 校验能过但库里两列仍同时非空——这条用例正是钉住那一步。
func TestSnapshotBindingScopeSwitchWithDB(t *testing.T) {
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

	f := newBindingFixture(t, pool, ctx, "SC")
	api := New(pool)
	handler := api.Handler()
	put := func(id int64, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
			"/admin/v1/snapshot-bindings/"+strconv.FormatInt(id, 10), strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}

	// 起始：类型级绑定。
	var id int64
	if err := pool.QueryRow(ctx, `
		insert into snapshot_binding (device_type_id, item_codes)
		values ($1, '["temp"]'::jsonb) returning id`, f.typeID).Scan(&id); err != nil {
		t.Fatalf("建类型级绑定: %v", err)
	}

	// ① 类型级 → 实例级：前端现在发的形状（两列带齐，type 侧 null）。
	if code, body := put(id, fmt.Sprintf(
		`{"device_type_id":null,"terminal_id":%q,"item_codes":["temp"]}`, f.terminalID)); code != http.StatusOK {
		t.Fatalf("类型级→实例级应 200，实际 %d %s", code, body)
	}
	var typeID *int64
	var terminalID *string
	if err := pool.QueryRow(ctx, `select device_type_id, terminal_id from snapshot_binding where id = $1`, id).
		Scan(&typeID, &terminalID); err != nil {
		t.Fatalf("回查绑定: %v", err)
	}
	if typeID != nil {
		t.Fatalf("切到实例级后 device_type_id 应为 NULL，实际 %d", *typeID)
	}
	if terminalID == nil || *terminalID != f.terminalID {
		t.Fatalf("terminal_id 应为 %q，实际 %v", f.terminalID, terminalID)
	}

	// ② 反向：实例级 → 类型级。
	if code, body := put(id, fmt.Sprintf(
		`{"device_type_id":%d,"terminal_id":null,"item_codes":["temp"]}`, f.typeID)); code != http.StatusOK {
		t.Fatalf("实例级→类型级应 200，实际 %d %s", code, body)
	}
	if err := pool.QueryRow(ctx, `select device_type_id, terminal_id from snapshot_binding where id = $1`, id).
		Scan(&typeID, &terminalID); err != nil {
		t.Fatalf("回查绑定: %v", err)
	}
	if terminalID != nil {
		t.Fatalf("切到类型级后 terminal_id 应为 NULL，实际 %q", *terminalID)
	}
	if typeID == nil || *typeID != f.typeID {
		t.Fatalf("device_type_id 应为 %d，实际 %v", f.typeID, typeID)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// 绑定校验（真库）：多字段、全部错码一次列出、目标重复统一文案、部分更新合并、换目标。
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

	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from snapshot_binding`)
		_, _ = pool.Exec(ctx, `delete from thing_model_property where type_id in (select id from device_type where code = 'BD-TYPE')`)
		_, _ = pool.Exec(ctx, `delete from terminal where id in ('BD-T1', 'BD-T2')`)
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
	for _, code := range []string{"bd-temp", "bd-power"} {
		if _, err := pool.Exec(ctx, `
			insert into thing_model_property (type_id, item_code, name, value_type)
			values ($1, $2, $2, 'number')`, typeID, code); err != nil {
			t.Fatalf("建物模型属性: %v", err)
		}
	}
	for _, id := range []string{"BD-T1", "BD-T2"} {
		if _, err := pool.Exec(ctx, `
			insert into terminal (id, type_id, gateway_id, name, enabled) values ($1, $2, 'BD-GW', $1, true)`,
			id, typeID); err != nil {
			t.Fatalf("建终端: %v", err)
		}
	}

	handler := New(pool).Handler()
	do := func(method, path, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	typeStr := strconv.FormatInt(typeID, 10)
	typePath := "/admin/v1/snapshot-bindings"

	// 1) 作用域二选一 / 空数组 / 非数组
	if code, body := do(http.MethodPost, typePath, `{"device_type_id":`+typeStr+`,"terminal_id":"BD-T1","item_codes":["bd-temp"]}`); code != http.StatusBadRequest || !strings.Contains(body, "二选一") {
		t.Fatalf("两个作用域都给应 400，实际 %d %s", code, body)
	}
	if code, body := do(http.MethodPost, typePath, `{"item_codes":["bd-temp"]}`); code != http.StatusBadRequest || !strings.Contains(body, "二选一") {
		t.Fatalf("两个作用域都不给应 400，实际 %d %s", code, body)
	}
	if code, body := do(http.MethodPost, typePath, `{"device_type_id":`+typeStr+`,"item_codes":[]}`); code != http.StatusBadRequest {
		t.Fatalf("空数组应 400，实际 %d %s", code, body)
	}

	// 2) 多个坏 code 一次列全
	code, body := do(http.MethodPost, typePath, `{"device_type_id":`+typeStr+`,"item_codes":["bd-temp","nope-1","nope-2"]}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "nope-1") || !strings.Contains(body, "nope-2") {
		t.Fatalf("应一次列出全部缺失 code，实际 %d %s", code, body)
	}

	// 3) 合法：一条绑定存多个字段（去重后入库）
	code, body = do(http.MethodPost, typePath, `{"device_type_id":`+typeStr+`,"item_codes":["bd-temp","bd-power","bd-temp"]}`)
	if code != http.StatusOK {
		t.Fatalf("合法绑定应 200，实际 %d %s", code, body)
	}
	var bindingID int64
	if err := pool.QueryRow(ctx, `select id from snapshot_binding where device_type_id = $1`, typeID).Scan(&bindingID); err != nil {
		t.Fatalf("读绑定: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select jsonb_array_length(item_codes) from snapshot_binding where id = $1`, bindingID).Scan(&count); err != nil {
		t.Fatalf("读字段数: %v", err)
	}
	if count != 2 {
		t.Fatalf("入库字段数 = %d，期望 2（重复 code 已收敛）", count)
	}

	// 4) 同一目标建第二条 → 400，文案与校验阶段一致
	code, body = do(http.MethodPost, typePath, `{"device_type_id":`+typeStr+`,"item_codes":["bd-temp"]}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "该目标已有一条绑定，请编辑那一条") {
		t.Fatalf("重复目标应 400 统一文案，实际 %d %s", code, body)
	}

	// 5) 部分更新：只传 item_codes（body 里没有作用域）→ 合并库里现有值后通过
	idPath := typePath + "/" + strconv.FormatInt(bindingID, 10)
	if code, body := do(http.MethodPut, idPath, `{"item_codes":["bd-power"]}`); code != http.StatusOK {
		t.Fatalf("只改 item_codes 应 200，实际 %d %s", code, body)
	}
	var codes []string
	if err := pool.QueryRow(ctx, `select array(select jsonb_array_elements_text(item_codes)) from snapshot_binding where id = $1`, bindingID).Scan(&codes); err != nil {
		t.Fatalf("读更新后的 item_codes: %v", err)
	}
	if len(codes) != 1 || codes[0] != "bd-power" {
		t.Fatalf("item_codes = %v，期望 [bd-power]", codes)
	}

	// 6) 部分更新里的坏 code 也要一次列出
	if code, body := do(http.MethodPut, idPath, `{"item_codes":["bad-x","bad-y"]}`); code != http.StatusBadRequest ||
		!strings.Contains(body, "bad-x") || !strings.Contains(body, "bad-y") {
		t.Fatalf("更新的坏 code 应一次列出，实际 %d %s", code, body)
	}

	// 7) 编辑换目标：把类型级改成实例级（BD-T1）→ 通过；再换成已占用的 BD-T2 时先建一条再试
	if code, body := do(http.MethodPut, idPath, `{"device_type_id":null,"terminal_id":"BD-T1"}`); code != http.StatusOK {
		t.Fatalf("换目标应 200，实际 %d %s", code, body)
	}
	var targetTerminal *string
	var targetType *int64
	if err := pool.QueryRow(ctx, `select device_type_id, terminal_id from snapshot_binding where id = $1`, bindingID).
		Scan(&targetType, &targetTerminal); err != nil {
		t.Fatalf("读目标: %v", err)
	}
	if targetType != nil || targetTerminal == nil || *targetTerminal != "BD-T1" {
		t.Fatalf("换目标未生效: type=%v terminal=%v", targetType, targetTerminal)
	}
	if code, body := do(http.MethodPost, typePath, `{"terminal_id":"BD-T2","item_codes":["bd-temp"]}`); code != http.StatusOK {
		t.Fatalf("给 BD-T2 建绑定应 200，实际 %d %s", code, body)
	}
	if code, body := do(http.MethodPut, idPath, `{"terminal_id":"BD-T2"}`); code != http.StatusBadRequest ||
		!strings.Contains(body, "该目标已有一条绑定，请编辑那一条") {
		t.Fatalf("换成已占用的目标应 400 统一文案，实际 %d %s", code, body)
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

// thing-model-properties 的 ID 列必须隐藏：该实体没有独立列表页，唯一消费方是终端类型详情页的
// 属性表；7 列挤在 1024~1366 视口里，自增主键占的 80px 换不回任何信息量。
func TestThingModelPropertyIDColumnHidden(t *testing.T) {
	fields, ok := FieldMetas("thing-model-properties")
	if !ok {
		t.Fatal("thing-model-properties 字段集缺失")
	}
	var idField *FieldMeta
	for i := range fields {
		if fields[i].Key == "id" {
			idField = &fields[i]
			break
		}
	}
	if idField == nil {
		t.Fatal("thing-model-properties 缺少 id 字段定义")
	}
	if idField.Visible {
		t.Fatal("ID 列应隐藏（Visible=false）")
	}
	// 其它实体的 ID 列不受影响。
	other, _ := FieldMetas("terminals")
	for _, f := range other {
		if f.Key == "id" && !f.Visible {
			t.Fatal("terminals 的 ID 列应保持可见")
		}
	}
}
