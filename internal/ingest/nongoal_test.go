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
	allowedTables := map[string]bool{
		"data_domain": true, "device_type": true, "thing_model_property": true, "terminal": true,
		"quality_rule": true, "settle_config": true,
		"latest_value": true, "hourly_staging": true, "hourly_snapshot": true,
		"exception_interval": true, "exception_count": true, "schema_migrations": true,
		// R-034（task-013）明确新增：补录留痕的版本记录表
		"snapshot_cell_history": true,
	}
	forbiddenWords := []string{
		"consumer", "whitelist", "quota", "audit", "command",
		"aggregate", "report", "backfill", "version", "lineage", "retention_config", "subscription",
	}
	createTable := regexp.MustCompile(`(?i)create table if not exists\s+([a-z_]+)`)
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

	// 异常枚举：状态型 3 种、事件型 10 种（含本轮新增 terminal_unknown），无重复。
	if len(exception.IntervalTypes) != 3 {
		t.Fatalf("状态型异常类型数 = %d，期望 3", len(exception.IntervalTypes))
	}
	if len(exception.EventTypes) != 10 {
		t.Fatalf("事件型异常类型数 = %d，期望 10", len(exception.EventTypes))
	}
	seen := map[exception.ErrorType]bool{}
	for _, kind := range append(append([]exception.ErrorType{}, exception.IntervalTypes...), exception.EventTypes...) {
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
