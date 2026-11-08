package ingest_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/admin"
	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 用例 31：非目标项检查 —— 零外部依赖（不调用 newEnv，admin handler 用 nil pool 构造）。
func TestNoOutOfScopeArtifacts(t *testing.T) {
	// 迁移允许建的表：历史上的合法表集合（含后来下线的两张）。
	allowedTables := map[string]bool{
		"data_domain": true, "device_type": true, "thing_model_property": true, "terminal": true,
		"quality_rule": true, "settle_config": true,
		"latest_value": true, "hourly_staging": true, "hourly_snapshot": true,
		// B4 六档快照（迁移 0010）：三张快照表 + 表级保留期。
		"snapshot_coarse": true, "snapshot_minute": true, "snapshot_second": true,
		"snapshot_settings": true,
		// 0013：全局六档配置 + 两级绑定；snapshot_plan 由 0010 建、0013 drop（见 retiredTables）。
		"snapshot_slot_config": true, "snapshot_binding": true, "snapshot_plan": true,
		"exception_interval": true, "exception_count": true, "schema_migrations": true,
		// 0016：边缘网关注册表（接入鉴权，不属于已收敛的报表/权限类能力）。
		"gateway": true,
		// 0018：快照查询模板（展示配置，不涉及新的报表/权限能力）。
		"snapshot_query_template": true,
		// 0022：网关 MQTT 密码密文（凭据展示，不新增报表/权限能力）。
		"gateway_credential": true,
		// R-034（task-013）曾新增，收敛后由 0008 删除：补录留痕的版本记录表
		"snapshot_cell_history": true,
	}
	// 收敛后必须已经不存在的表：建过、但迁移里必须有一条 drop。
	// 只把它们从"允许建"里删掉是不够的——0001/0002/0003/0007 仍然建它们，否则这条守卫会误报。
	// 0008 删掉异常区间与补录留痕；0009 删掉小时级历史与结算窗口。
	retiredTables := map[string]bool{
		"exception_interval":    true,
		"snapshot_cell_history": true,
		"hourly_snapshot":       true,
		"hourly_staging":        true,
		"settle_config":         true,
		"snapshot_plan":         true, // 0013 用「全局六档 + 绑定」取代了按序列的计划表
	}
	forbiddenWords := []string{
		"consumer", "whitelist", "quota", "audit", "command",
		"aggregate", "report", "backfill", "version", "lineage", "retention_config", "subscription",
	}
	createTable := regexp.MustCompile(`(?i)create table if not exists\s+([a-z_]+)`)
	dropTable := regexp.MustCompile(`(?i)drop table if exists\s+([a-z_]+)`)
	dropped := map[string]bool{}
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("读取迁移目录: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("没有找到迁移文件")
	}
	for _, name := range files {
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("读取 %s: %v", name, err)
		}
		for _, m := range createTable.FindAllStringSubmatch(string(body), -1) {
			table := m[1]
			// 0010 用 DO 块动态建分区：模板里是 'snapshot_coarse_%s'，正则只抓到前缀
			// （形如 snapshot_coarse_），它不是真实表名，跳过。真实表由上面的白名单覆盖。
			if strings.HasSuffix(table, "_") {
				continue
			}
			if !allowedTables[table] {
				t.Fatalf("%s 建了非目标表 %q", name, table)
			}
			for _, bad := range forbiddenWords {
				if strings.Contains(table, bad) {
					t.Fatalf("表 %q 命中非目标关键字 %q", table, bad)
				}
			}
		}
		for _, m := range dropTable.FindAllStringSubmatch(string(body), -1) {
			dropped[m[1]] = true
		}
	}

	// 收敛边界：下线能力建过的表，必须在迁移里有对应的 drop（只从白名单删掉不算数）。
	for table := range retiredTables {
		if !dropped[table] {
			t.Fatalf("收敛后应下线的表 %q 没有任何迁移 drop 它", table)
		}
	}

	// 管理 API 只暴露数据源/网关注册实体（结算窗口随 0009 下线）；nil pool 即可验证 404 路径不碰数据库。
	wantEntities := []string{
		"data-domains", "thing-models", "thing-model-properties",
		"terminals", "gateways", "quality-rules", "snapshot-slots", "snapshot-queries",
		"snapshot-bindings", "snapshot-settings",
	}
	if len(admin.Entities) != len(wantEntities) {
		t.Fatalf("管理 API 实体数 = %d，期望 %d", len(admin.Entities), len(wantEntities))
	}
	got := map[string]bool{}
	for _, ent := range admin.Entities {
		got[ent.Name] = true
	}
	for _, name := range wantEntities {
		if !got[name] {
			t.Fatalf("缺少管理实体 %q", name)
		}
	}

	// 双口径：可 CRUD = 9（上面已断言）；可导航 = 6（属性 / 绑定 / 保留期是子资源，不进导航）。
	meta := admin.BuildMeta()
	if len(meta.Entities) != 7 {
		t.Fatalf("可导航实体 = %d，期望 7（可 CRUD 仍是 %d）", len(meta.Entities), len(admin.Entities))
	}
	for _, ent := range meta.Entities {
		if ent.Name == "thing-model-properties" {
			t.Fatal("thing-model-properties 可 CRUD 但不可导航，不应出现在 _meta.entities")
		}
	}
	// 不可导航不等于不可见：字段元数据必须仍从 subresources 下发，否则属性详情页拿不到列定义/必填。
	subs := map[string]bool{}
	for _, sub := range meta.Subresources {
		subs[sub.Name] = true
		if len(sub.Fields) == 0 {
			t.Fatalf("subresources 里的 %s 没有字段元数据", sub.Name)
		}
	}
	if len(meta.Subresources) != 3 || !subs["thing-model-properties"] ||
		!subs["snapshot-settings"] || !subs["snapshot-bindings"] {
		t.Fatalf("subresources = %v，期望 thing-model-properties + snapshot-settings + snapshot-bindings", subs)
	}
	handler := admin.New(nil).Handler()
	for _, bad := range []string{"consumers", "permissions", "commands", "reports",
		"snapshot-versions", "backfills", "aggregations", "audits"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/"+bad, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("非目标接口 %q 返回 %d，期望 404", bad, rec.Code)
		}
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/v1/"+bad, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("非目标接口 POST %q 返回 %d，期望 404", bad, rec.Code)
		}
	}

	// 异常枚举：8 种（B4 新增 snapshot_write_failed），无重复。
	if len(exception.EventTypes) != 9 {
		t.Fatalf("事件型异常类型数 = %d，期望 9", len(exception.EventTypes))
	}
	seen := map[exception.ErrorType]bool{}
	for _, kind := range exception.EventTypes {
		if seen[kind] {
			t.Fatalf("异常类型 %q 重复定义", kind)
		}
		seen[kind] = true
		if parsed, ok := exception.Parse(string(kind)); !ok || parsed != kind {
			t.Fatalf("异常类型 %q 未通过 Parse 校验", kind)
		}
	}

	// 只允许 PostgreSQL，不允许 Redis / 消息中间件。
	gomod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("读取 go.mod: %v", err)
	}
	for _, bad := range []string{"redis", "kafka", "rabbitmq", "nats", "sarama", "amqp"} {
		if strings.Contains(strings.ToLower(string(gomod)), bad) {
			t.Fatalf("go.mod 引入了中间件 %q", bad)
		}
	}

	// 源码级验收：不再有静默去重与截断读取；业务校验不写 auth_failed；
	// 实时报文只有 publishedAt / collectedAt。（关键字用拼接，避免本文件自己被 grep 命中）
	for _, f := range goFiles(t) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s: %v", f, err)
		}
		text := string(body)
		if strings.Contains(text, "de"+"dupe") {
			t.Fatalf("%s 仍存在去重逻辑", f)
		}
		if strings.Contains(text, "io.Limit"+"Reader") {
			t.Fatalf("%s 仍用静默截断的读取方式", f)
		}
	}
	for _, f := range goFiles(t) {
		if !strings.HasPrefix(f, filepath.Join("..", "ingest")) && !strings.HasPrefix(f, "internal/ingest") &&
			!strings.Contains(f, "internal/ingest") {
			continue
		}
		body := string(mustRead(t, f))
		if strings.Contains(body, "AuthFailed") {
			t.Fatalf("%s 的业务校验使用了 auth_failed", f)
		}
	}
	publishSrc := string(mustRead(t, filepath.Join("..", "publish", "publish.go")))
	for _, bad := range []string{`json:"ts"`, `json:"time"`} {
		if strings.Contains(publishSrc, bad) {
			t.Fatalf("实时报文仍存在时间字段 %s", bad)
		}
	}
	for _, want := range []string{`json:"publishedAt"`, `json:"collectedAt"`} {
		if !strings.Contains(publishSrc, want) {
			t.Fatalf("实时报文缺少时间字段 %s", want)
		}
	}
}

func goFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == ".dev" || base == ".devcache" || base == "docs" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历源码: %v", err)
	}
	return out
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	return body
}
