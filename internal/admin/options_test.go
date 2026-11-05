package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// white list：选项接口只认 6 个可 CRUD 实体；异常只读页与非实体路径一律 404。
func TestOptionsRouteWhitelist(t *testing.T) {
	handler := New(nil).Handler() // nil pool：404 分支在碰数据库之前返回
	cases := []struct {
		path string
		want string
	}{
		{"/admin/v1/options/nope", "unknown_entity"},
		{"/admin/v1/options/exception-events", "unknown_entity"},
		{"/admin/v1/options/exception-intervals", "unknown_entity"},
		{"/admin/v1/options", "unknown_entity"}, // 只有一段，落回 {entity} 白名单
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d，期望 404: %s", c.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Fatalf("%s 的响应应包含 %q: %s", c.path, c.want, rec.Body.String())
		}
	}

	// 选项接口不接受写入。
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/v1/options/terminals", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("POST /admin/v1/options/terminals 不应成功: %s", rec.Body.String())
	}
}

// 标签降级链：name → code → id，只用到实体真实存在的列。
func TestOptionLabelExpr(t *testing.T) {
	cases := map[string]string{
		"data-domains":  "coalesce(nullif(name::text, ''), nullif(code::text, ''), id::text)",
		"terminals":     "coalesce(nullif(name::text, ''), id::text)",
		"quality-rules": "id::text",
	}
	for entity, want := range cases {
		e := mustEntity(t, entity)
		if got := optionLabelExpr(e); got != want {
			t.Fatalf("%s 的标签表达式 = %q，期望 %q", entity, got, want)
		}
	}
}

// 需 DSN：真实取数（id→label、空 name 回退 code、上限 1000）。
func TestOptionsWithDB(t *testing.T) {
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
	// 并行运行隔离：本次运行独占 qtest-opt-<pid>- 前缀。
	prefix := fmt.Sprintf("qtest-opt-%d-", os.Getpid())
	cleanup := func() {
		_, _ = pool.Exec(ctx, `delete from device_type where code like $1`, prefix+"%")
		_, _ = pool.Exec(ctx, `delete from data_domain where code like $1`, prefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	var domainID int64
	if err := pool.QueryRow(ctx,
		`insert into data_domain (code, name) values ($1, '泵站') returning id`, prefix+"dom").Scan(&domainID); err != nil {
		t.Fatalf("建数据域: %v", err)
	}
	// name 为空 → 标签回退到 code。
	var blankID int64
	if err := pool.QueryRow(ctx,
		`insert into data_domain (code, name) values ($1, '') returning id`, prefix+"blank").Scan(&blankID); err != nil {
		t.Fatalf("建空名数据域: %v", err)
	}
	var typeID int64
	if err := pool.QueryRow(ctx,
		`insert into device_type (code, name, domain_id) values ($1, '泵站类型', $2) returning id`,
		prefix+"type", domainID).Scan(&typeID); err != nil {
		t.Fatalf("建终端类型: %v", err)
	}

	handler := New(pool).Handler()
	options := func(entity string) []OptionItem {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/options/"+entity, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("options/%s = %d: %s", entity, rec.Code, rec.Body.String())
		}
		var resp struct {
			Code int          `json:"code"`
			Data []OptionItem `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("options/%s 响应不是合法 JSON: %v", entity, err)
		}
		if resp.Data == nil {
			t.Fatalf("options/%s 的 data 必须是数组，实际 %s", entity, rec.Body.String())
		}
		return resp.Data
	}
	find := func(items []OptionItem, value string) (OptionItem, bool) {
		for _, it := range items {
			if it.Value == value {
				return it, true
			}
		}
		return OptionItem{}, false
	}

	// 1) data-domains：value 是 id 文本，label 取 name。
	items := options("data-domains")
	got, ok := find(items, fmt.Sprint(domainID))
	if !ok || got.Label != "泵站" {
		t.Fatalf("data-domain 选项 = %+v/%v，期望 label=泵站", got, ok)
	}

	// 2) name 为空 → 回退 code。
	got, ok = find(items, fmt.Sprint(blankID))
	if !ok || got.Label != prefix+"blank" {
		t.Fatalf("空 name 的选项 = %+v/%v，期望 label=%s", got, ok, prefix+"blank")
	}

	// 3) device-types：label 取 name；value 是 id 文本。
	got, ok = find(options("device-types"), fmt.Sprint(typeID))
	if !ok || got.Label != "泵站类型" {
		t.Fatalf("device-type 选项 = %+v/%v，期望 label=泵站类型", got, ok)
	}

	// 4) 上限 1000：插入 1001 行后仍只返回 1000 行。
	if _, err := pool.Exec(ctx,
		`insert into data_domain (code, name)
		 select $1 || 'bulk-' || g, '' from generate_series(1, 1001) as g`, prefix); err != nil {
		t.Fatalf("批量建数据域: %v", err)
	}
	bulk := options("data-domains")
	if len(bulk) != MaxOptionRows {
		t.Fatalf("选项行数 = %d，期望上限 %d", len(bulk), MaxOptionRows)
	}
}
