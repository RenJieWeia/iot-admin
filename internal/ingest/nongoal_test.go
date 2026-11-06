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
	// 迁移允许建的表：历史上的合法表集合（含后已下线的两张）。
	allowedTables := map[string]bool{
		"data_domain": true, "device_type": true, "thing_model_property": true, "terminal": true,
		"quality_rule": true, "settle_config": true,
		"latest_value": true, "hourly_staging": true, "hourly_snapshot": true,
		"exception_interval": true, "exception_count": true, "schema_migrations": true,
		// R-034（task-013）曾新增，收敛后由 0008 删除：补录留痕的版本记录表
		"snapshot_cell_history": true,
	}
	// 收敛（0008）后必须已经不存在的表：建过、但迁移里必须有一条 drop。
	// 只把它们从"允许建"里删掉是不够的——0003/0007 仍然建它们，否则这条守卫会误报。
	retiredTables := map[string]bool{
		"exception_interval":    true,
		"snapshot_cell_history": true,
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

	// 管理 API 只暴露 6 个数据源实体；nil pool 即可验证 404 路径不碰数据库。
	wantEntities := []string{
		"data-domains", "device-types", "thing-model-properties",
		"terminals", "quality-rules", "settle-configs",
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

	// 异常枚举：收敛后只剩事件型 10 种（状态型区间已下线），无重复。
	if len(exception.EventTypes) != 10 {
		t.Fatalf("事件型异常类型数 = %d，期望 10", len(exception.EventTypes))
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
			if base == ".git" || base == ".devcache" || base == "docs" || base == "node_modules" {
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
