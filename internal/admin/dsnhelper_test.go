package admin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// 测试库隔离（task-006 / R3）：每个需要 DB 的测试包用自己的库，避免并行跑包时
// internal/ingest 的 truncate 清掉本包正在用的行（admin 的 cleanup 也不再是别人的噪声）。
//
// 规则：把 IOT_TEST_DSN 的库名加后缀（iot → iot_admin），库不存在就用维护库 postgres 建，
// 幂等且并发安全；测试用户没有建库权限时跳过并说明原因，不静默用错库。

const testDBSuffix = "admin"

// errNoCreateDatabase 表示当前测试用户没有建库权限（调用方据此 Skip 而不是 Fail）。
var errNoCreateDatabase = errors.New("测试用户没有建库权限")

// testDSN 返回本包专用测试库的 DSN；未配置 IOT_TEST_DSN 时跳过需要 PG 的用例。
func testDSN(t *testing.T) string {
	t.Helper()
	base := os.Getenv("IOT_TEST_DSN")
	if base == "" {
		t.Skip("未设置 IOT_TEST_DSN，跳过需要 PostgreSQL 的用例")
	}
	dsn, err := deriveDSN(base, testDBSuffix)
	if err != nil {
		t.Fatalf("派生测试库 DSN: %v", err)
	}
	if err := ensureDatabase(dsn); err != nil {
		if errors.Is(err, errNoCreateDatabase) {
			t.Skipf("跳过需要 PostgreSQL 的用例：%v", err)
		}
		t.Fatalf("准备测试库: %v", err)
	}
	return dsn
}

// deriveDSN 把 DSN 的库名换成 <原名>_<suffix>，其余（host/端口/凭据/参数）原样保留。
// 错误信息只描述原因，不回显 DSN，避免口令进测试日志。
func deriveDSN(base, suffix string) (string, error) {
	if suffix == "" {
		return "", errors.New("派生的库名后缀不能为空")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("解析 DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("DSN 必须是 postgres:// 或 postgresql://，实际 scheme=%q", u.Scheme)
	}
	name := databaseName(u)
	if name == "" {
		return "", errors.New("DSN 缺少库名（形如 postgres://host:port/iot）")
	}
	u.Path = "/" + name + "_" + suffix
	return u.String(), nil
}

// databaseName 取 URL 里的库名（去掉前导 "/"）。
func databaseName(u *url.URL) string {
	return strings.TrimPrefix(u.Path, "/")
}

// ensureDatabase 幂等建库：已存在直接返回；不存在则连维护库 postgres 建。
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("解析 DSN: %w", err)
	}
	name := databaseName(u)
	if name == "" {
		return errors.New("DSN 缺少库名")
	}
	maintenance := *u
	maintenance.Path = "/postgres"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, maintenance.String())
	if err != nil {
		return fmt.Errorf("连接维护库 postgres: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx,
		`select exists(select 1 from pg_database where datname = $1)`, name).Scan(&exists); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42501" {
			return fmt.Errorf("%w: 无法查询 pg_database: %s", errNoCreateDatabase, pgErr.Message)
		}
		return fmt.Errorf("查询 pg_database: %w", err)
	}
	if exists {
		return nil
	}
	// 库名不可参数化，用 pgx.Identifier 做标识符转义。
	if _, err := conn.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "42501": // insufficient_privilege：无建库权限
				return fmt.Errorf("%w: 测试库 %s 不存在且无法创建: %s", errNoCreateDatabase, name, pgErr.Message)
			case "42P04": // duplicate_database：并发建库的兜底，视为已存在
				return nil
			}
		}
		return fmt.Errorf("创建测试库 %s: %w", name, err)
	}
	return nil
}

// 派生规则锚点：库名替换、其余原样；非法输入给出清晰错误。
func TestDeriveDSN(t *testing.T) {
	cases := []struct {
		name string
		base string
		suf  string
		want string
	}{
		{"admin", "postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable", "admin",
			"postgres://postgres:pg@127.0.0.1:55432/iot_admin?sslmode=disable"},
		{"ingest", "postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable", "ingest",
			"postgres://postgres:pg@127.0.0.1:55432/iot_ingest?sslmode=disable"},
		{"无参数", "postgresql://u:p@db:5432/iot", "admin", "postgresql://u:p@db:5432/iot_admin"},
		{"原库带下划线", "postgres://u:p@h:5432/iot_test", "admin", "postgres://u:p@h:5432/iot_test_admin"},
	}
	for _, c := range cases {
		got, err := deriveDSN(c.base, c.suf)
		if err != nil {
			t.Fatalf("%s: 派生失败: %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: 派生结果 = %q，期望 %q", c.name, got, c.want)
		}
	}

	bad := []struct {
		name string
		base string
		suf  string
	}{
		{"空后缀", "postgres://u:p@h:5432/iot", ""},
		{"缺库名", "postgres://u:p@h:5432", "admin"},
		{"缺库名带斜杠", "postgres://u:p@h:5432/", "admin"},
		{"非 postgres scheme", "mysql://u:p@h:3306/iot", "admin"},
	}
	for _, c := range bad {
		if _, err := deriveDSN(c.base, c.suf); err == nil {
			t.Fatalf("%s: 期望报错，实际成功", c.name)
		}
	}
}
