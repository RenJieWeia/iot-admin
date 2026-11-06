package admin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

func mustEntity(t *testing.T, name string) Entity {
	t.Helper()
	e, ok := lookup(name)
	if !ok {
		t.Fatalf("实体 %q 不存在", name)
	}
	return e
}

// 验收 §9：未知筛选字段 → error。
func TestParseQueryStateRejectsUnknownFilterField(t *testing.T) {
	e := mustEntity(t, "terminals")
	if _, err := ParseQueryState(e, url.Values{"filter.nope": {"x"}}); err == nil {
		t.Fatal("未知筛选字段应报错")
	} else if !strings.Contains(err.Error(), "invalid_query") {
		t.Fatalf("错误应为 invalid_query，实际 %v", err)
	}
}

// 验收 §9：未知排序字段 → error。
func TestParseQueryStateRejectsUnknownSortField(t *testing.T) {
	e := mustEntity(t, "terminals")
	if _, err := ParseQueryState(e, url.Values{"sort": {"nope"}}); err == nil {
		t.Fatal("未知排序字段应报错")
	}
}

// 验收 §9：未知操作符 → error。
func TestParseQueryStateRejectsUnknownOperator(t *testing.T) {
	e := mustEntity(t, "terminals")
	if _, err := ParseQueryState(e, url.Values{"filter.enabled.sounds": {"true"}}); err == nil {
		t.Fatal("未知操作符应报错")
	}
	if _, err := ParseQueryState(e, url.Values{"filter.name.sounds": {"x"}}); err == nil {
		t.Fatal("未声明的操作符应报错")
	}
}

// 不可筛选字段也必须显式拒绝（filterable:false 不是"悄悄忽略"）。
func TestParseQueryStateRejectsNonFilterableField(t *testing.T) {
	e := mustEntity(t, "quality-rules")
	if _, err := ParseQueryState(e, url.Values{"filter.params": {`{"min":1}`}}); err == nil {
		t.Fatal("filterable:false 的字段应报错")
	}
}

// 验收 §9：q 里的 % _ \ 必须转义后才进 LIKE 参数。
func TestBuildListQueryEscapesLike(t *testing.T) {
	e := mustEntity(t, "terminals")
	qs, err := ParseQueryState(e, url.Values{"q": {`50%_off\`}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	sql, args, err := BuildListQuery(e, qs)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.Contains(sql, "ilike") {
		t.Fatalf("关键词查询应使用 ilike: %s", sql)
	}
	if len(args) == 0 {
		t.Fatal("缺少查询参数")
	}
	want := `%50\%\_off\\%`
	if got, _ := args[0].(string); got != want {
		t.Fatalf("LIKE 参数 = %q，期望 %q", got, want)
	}
}

// 验收 §9：page=0 → 1；pageSize=1000 → 50；limit/offset 映射到 pageSize/page。
func TestParseQueryStatePagination(t *testing.T) {
	e := mustEntity(t, "terminals")

	qs, err := ParseQueryState(e, url.Values{"page": {"0"}, "pageSize": {"1000"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if qs.Page != 1 {
		t.Fatalf("page = %d，期望 1", qs.Page)
	}
	if qs.PageSize != MaxPageSize {
		t.Fatalf("pageSize = %d，期望 %d", qs.PageSize, MaxPageSize)
	}

	// 默认值
	qs, err = ParseQueryState(e, url.Values{})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if qs.Page != 1 || qs.PageSize != DefaultPageSize || qs.Sort != "id" || qs.Order != "desc" {
		t.Fatalf("默认查询状态不符: %+v", qs)
	}

	// 旧参数兼容：limit=20&offset=40 → pageSize=20, page=3
	qs, err = ParseQueryState(e, url.Values{"limit": {"20"}, "offset": {"40"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if qs.PageSize != 20 || qs.Page != 3 {
		t.Fatalf("limit/offset 映射 = pageSize %d / page %d，期望 20 / 3", qs.PageSize, qs.Page)
	}

	// 新参数优先于旧参数
	qs, err = ParseQueryState(e, url.Values{"page": {"2"}, "pageSize": {"10"}, "limit": {"50"}, "offset": {"50"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if qs.Page != 2 || qs.PageSize != 10 {
		t.Fatalf("新参数应优先：page=%d pageSize=%d", qs.Page, qs.PageSize)
	}

	// 非法取值
	if _, err := ParseQueryState(e, url.Values{"page": {"x"}}); err == nil {
		t.Fatal("非法 page 应报错")
	}
	if _, err := ParseQueryState(e, url.Values{"pageSize": {"x"}}); err == nil {
		t.Fatal("非法 pageSize 应报错")
	}
	if _, err := ParseQueryState(e, url.Values{"order": {"sideways"}}); err == nil {
		t.Fatal("非法 order 应报错")
	}
}

// 验收 §9：BuildListQuery 输出白名单列 + 方向 + 唯一 tie-breaker。
func TestBuildListQueryOrderBy(t *testing.T) {
	e := mustEntity(t, "terminals")
	qs, err := ParseQueryState(e, url.Values{"sort": {"name"}, "order": {"desc"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	sql, _, err := BuildListQuery(e, qs)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.Contains(sql, "order by name desc, id asc") {
		t.Fatalf("排序子句不符: %s", sql)
	}

	// 按 id 排序时不重复追加 tie-breaker。
	qs, _ = ParseQueryState(e, url.Values{})
	sql, _, _ = BuildListQuery(e, qs)
	if !strings.Contains(sql, "order by id desc") || strings.Contains(sql, "id desc, id") {
		t.Fatalf("默认排序应为单个 id desc: %s", sql)
	}
}

// 筛选条件按字段声明的操作符落到 SQL；枚举/关系用 = any，数值用区间。
func TestBuildListQueryFilters(t *testing.T) {
	e := mustEntity(t, "terminals")
	qs, err := ParseQueryState(e, url.Values{
		"filter.enabled":  {"false"},
		"filter.type_id":  {"7", "8"},
		"filter.name.gte": {"x"},
	})
	if err == nil {
		t.Fatal("字段 name 未声明 gte，应报错")
	}

	qs, err = ParseQueryState(e, url.Values{
		"filter.enabled": {"false"},
		"filter.type_id": {"7", "8"},
	})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	sql, args, err := BuildListQuery(e, qs)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if !strings.Contains(sql, "enabled::text = $") {
		t.Fatalf("布尔筛选 SQL 不符: %s", sql)
	}
	if !strings.Contains(sql, "type_id::text = any($") {
		t.Fatalf("in 筛选 SQL 不符: %s", sql)
	}
	if len(args) < 2 {
		t.Fatalf("筛选参数不足: %v", args)
	}
	var sawIn bool
	for _, a := range args {
		if vals, ok := a.([]string); ok && len(vals) == 2 && vals[0] == "7" && vals[1] == "8" {
			sawIn = true
		}
	}
	if !sawIn {
		t.Fatalf("in 参数未按多值传入: %v", args)
	}
}

// 未知实体直接拒绝，不落到 SQL。
func TestParseQueryStateUnknownEntity(t *testing.T) {
	_, err := ParseQueryState(Entity{Name: "nope", Table: "t", Cols: []string{"id"}}, url.Values{})
	var qerr *QueryError
	if err == nil || !errors.As(err, &qerr) {
		t.Fatalf("未知实体应返回 QueryError，实际 %v", err)
	}
}

// 验收 §9（需 DSN）：三种组合的返回行序与 meta.total 正确。
func TestListRowsWithDB(t *testing.T) {
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
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from terminal where id like 'qtest-%'`)
		_, _ = pool.Exec(ctx, `delete from device_type where code = 'qtest'`)
		_, _ = pool.Exec(ctx, `delete from data_domain where code = 'qtest'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	var domainID, typeID int64
	if err := pool.QueryRow(ctx, `insert into data_domain (code, name) values ('qtest', '查询测试') returning id`).
		Scan(&domainID); err != nil {
		t.Fatalf("建数据域: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into device_type (code, name, domain_id) values ('qtest', '查询测试', $1) returning id`,
		domainID).Scan(&typeID); err != nil {
		t.Fatalf("建终端类型: %v", err)
	}
	rows := []struct {
		id      string
		gateway string
		enabled bool
		name    string
	}{
		{"qtest-1", "QGW1", true, "一号"},
		{"qtest-2", "QGW1", false, "二号"},
		{"qtest-3", "QGW2", false, "三号"},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx,
			`insert into terminal (id, type_id, gateway_id, name, enabled) values ($1, $2, $3, $4, $5)`,
			r.id, typeID, r.gateway, r.name, r.enabled); err != nil {
			t.Fatalf("建终端 %s: %v", r.id, err)
		}
	}

	e := mustEntity(t, "terminals")
	ids := func(res ListResult) []string {
		out := make([]string, 0, len(res.Data))
		for _, row := range res.Data {
			out = append(out, row["id"].(string))
		}
		return out
	}

	// 1) filter.enabled=false → 2 行
	qs, err := ParseQueryState(e, url.Values{"filter.enabled": {"false"}, "q": {"QGW"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	res, err := ListRows(ctx, pool, e, qs)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if res.Meta.Total != 2 || len(res.Data) != 2 {
		t.Fatalf("filter.enabled=false → total=%d rows=%d，期望 2/2", res.Meta.Total, len(res.Data))
	}

	// 2) q=GW → 3 行（网关列命中）
	qs, _ = ParseQueryState(e, url.Values{"q": {"QGW"}, "pageSize": {"50"}})
	res, _ = ListRows(ctx, pool, e, qs)
	if res.Meta.Total != 3 {
		t.Fatalf("q=qtest- → total=%d，期望 3", res.Meta.Total)
	}

	// 3) sort=id&order=asc&page=2&pageSize=2 → 第 3 条
	qs, err = ParseQueryState(e, url.Values{
		"q": {"QGW"}, "filter.gateway_id": {"QGW"}, "sort": {"id"}, "order": {"asc"}, "page": {"2"}, "pageSize": {"2"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	res, err = ListRows(ctx, pool, e, qs)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if res.Meta.Total != 3 || len(res.Data) != 1 {
		t.Fatalf("第 2 页 total=%d rows=%d，期望 3/1", res.Meta.Total, len(res.Data))
	}
	if got := ids(res); got[0] != "qtest-3" {
		t.Fatalf("第 2 页首行 = %s，期望 qtest-3", got[0])
	}

	// 4) 超出范围 → 空页但 total 仍正确
	qs, _ = ParseQueryState(e, url.Values{"q": {"QGW"}, "page": {"9"}, "pageSize": {"10"}})
	res, _ = ListRows(ctx, pool, e, qs)
	if res.Meta.Total != 3 || len(res.Data) != 0 {
		t.Fatalf("超范围页 total=%d rows=%d，期望 3/0", res.Meta.Total, len(res.Data))
	}
}
