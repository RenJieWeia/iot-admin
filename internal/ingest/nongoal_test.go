package ingest_test

import (
	"io/fs"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/admin"
	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// 用例 15：非目标项检查 —— 仓库中不存在权限、消费方、命令、聚合查询、补录、版本化相关表与接口。
func TestNoOutOfScopeArtifacts(t *testing.T) {
	e := newEnv(t)

	allowedTables := map[string]bool{
		"data_domain": true, "device_type": true, "thing_model_property": true, "terminal": true,
		"quality_rule": true, "settle_config": true,
		"latest_value": true, "hourly_staging": true, "hourly_snapshot": true,
		"exception_interval": true, "exception_count": true, "schema_migrations": true,
	}
	forbiddenSubstrings := []string{
		"consumer", "permission", "whitelist", "quota", "audit", "command",
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
			for _, bad := range forbiddenSubstrings {
				if strings.Contains(table, bad) {
					t.Fatalf("表 %q 命中非目标关键字 %q", table, bad)
				}
			}
		}
	}

	// 管理 API 只暴露 6 个数据源实体，非目标实体一律 404。
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
	handler := admin.New(e.pool).Handler()
	for _, bad := range []string{"consumers", "permissions", "commands", "reports",
		"snapshot-versions", "backfills", "aggregations", "audits"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/v1/"+bad, nil))
		if rec.Code != 404 {
			t.Fatalf("非目标接口 %q 返回 %d，期望 404", bad, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/admin/v1/commands", nil))
	if rec.Code != 404 {
		t.Fatalf("命令服务接口不应存在，实际返回 %d", rec.Code)
	}

	// 异常枚举：状态型 3 种、事件型 9 种，无重复。
	if len(exception.IntervalTypes) != 3 {
		t.Fatalf("状态型异常类型数 = %d，期望 3", len(exception.IntervalTypes))
	}
	if len(exception.EventTypes) != 9 {
		t.Fatalf("事件型异常类型数 = %d，期望 9", len(exception.EventTypes))
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
	if !strings.Contains(string(gomod), "github.com/jackc/pgx/v5") {
		t.Fatal("go.mod 应只依赖 PostgreSQL 驱动")
	}
}
