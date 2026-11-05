package ingest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
	"github.com/RenJieWeia/iot-admin/migrations"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

const (
	testDomainCode   = "energy"
	testTypeCode     = "meter"
	testTerminalID   = "T1"
	testGatewayID    = "GW1"
	testTerminalAlt  = "T2"
	testGatewayAlt   = "GW2"
	defaultWindowSec = 900
)

// baseTime 固定在一个月中段，避免测试踩月末分区边界。
var baseTime = time.Date(2026, 11, 5, 10, 0, 0, 0, staging.Zone)

type testClock struct{ t time.Time }

func (c *testClock) Now() time.Time      { return c.t }
func (c *testClock) Set(t time.Time)     { c.t = t }
func (c *testClock) Add(d time.Duration) { c.t = c.t.Add(d) }

type published struct {
	topic   string
	payload []byte
}

type fakePublisher struct {
	mu   sync.Mutex
	sent []published
	err  error
}

func (f *fakePublisher) Publish(topic string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, published{topic: topic, payload: append([]byte(nil), payload...)})
	return nil
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakePublisher) last(t *testing.T) published {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("没有发布任何实时报文")
	}
	return f.sent[len(f.sent)-1]
}

type testEnv struct {
	t        *testing.T
	pool     *pgxpool.Pool
	proc     *ingest.Processor
	rec      *exception.Recorder
	pub      *fakePublisher
	clock    *testClock
	domainID int64
	typeID   int64
	settler  *settle.Settler
}

// newEnv 连接测试库、迁移、清库，并种入一套最小数据源。
func newEnv(t *testing.T) *testEnv {
	t.Helper()
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
	if _, err := pool.Exec(ctx, `
		truncate exception_count, exception_interval, latest_value, hourly_staging;
		truncate hourly_snapshot;
		truncate quality_rule, settle_config, terminal, thing_model_property, device_type, data_domain
			restart identity cascade`); err != nil {
		t.Fatalf("清库: %v", err)
	}

	// 默认平台时刻放在该小时第 55 分钟：既在窗口内，又让 10:00~10:55 的采集时间都不过超前限。
	e := &testEnv{t: t, pool: pool, clock: &testClock{t: baseTime.Add(55 * time.Minute)}, pub: &fakePublisher{}}
	e.seed()
	e.rec = exception.New(pool, e.clock.Now, quietLog)
	e.proc = &ingest.Processor{
		DB:    pool,
		Reg:   validate.NewRegistry(pool),
		Pub:   e.pub,
		Exc:   e.rec,
		Locks: store.NewShardedLock(16),
		Now:   e.clock.Now,
		Log:   quietLog,
	}
	e.settler = settle.New(pool, e.rec, e.clock.Now, quietLog)
	if err := settle.EnsurePartitions(ctx, pool, e.clock.Now()); err != nil {
		t.Fatalf("建分区: %v", err)
	}
	return e
}

func (e *testEnv) seed() {
	e.t.Helper()
	ctx := context.Background()
	must := func(err error) {
		e.t.Helper()
		if err != nil {
			e.t.Fatalf("种入夹具: %v", err)
		}
	}
	must(e.pool.QueryRow(ctx, `insert into data_domain (code, name) values ($1, '能源') returning id`,
		testDomainCode).Scan(&e.domainID))
	must(e.pool.QueryRow(ctx, `insert into device_type (code, name, domain_id) values ($1, '电表', $2) returning id`,
		testTypeCode, e.domainID).Scan(&e.typeID))
	props := []struct {
		code      string
		name      string
		valueTyp  string
		unit      string
		precision int
		required  bool
	}{
		{"temp", "温度", "number", "C", 1, true},
		{"power", "功率", "number", "kW", 2, true},
		{"status", "状态", "string", "", 0, false},
		{"online", "在线", "bool", "", 0, false},
	}
	for _, p := range props {
		_, err := e.pool.Exec(ctx, `
			insert into thing_model_property
			    (type_id, item_code, name, value_type, unit, precision, required)
			values ($1, $2, $3, $4, nullif($5, ''), nullif($6, 0), $7)`,
			e.typeID, p.code, p.name, p.valueTyp, p.unit, p.precision, p.required)
		must(err)
	}
	must(e.insertTerminal(testTerminalID, testGatewayID, true))
	must(e.insertTerminal(testTerminalAlt, testGatewayAlt, true))
	// 全局默认结算窗口 900 秒。
	_, err := e.pool.Exec(ctx, `insert into settle_config (domain_id, type_id, window_seconds) values (null, null, 900)`)
	must(err)
}

func (e *testEnv) insertTerminal(id, gateway string, enabled bool) error {
	_, err := e.pool.Exec(context.Background(),
		`insert into terminal (id, type_id, gateway_id, name, enabled) values ($1, $2, $3, $1, $4)`,
		id, e.typeID, gateway, enabled)
	return err
}

func (e *testEnv) addRule(code string, kind string, params string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		insert into quality_rule (terminal_id, item_code, rule_type, params) values ($1, $2, $3, $4::jsonb)`,
		testTerminalID, code, kind, params); err != nil {
		e.t.Fatalf("种入质量规则: %v", err)
	}
}

// item 是构造上报消息用的小结构。
type item struct {
	Code  string
	Value any
	Unit  string
	Time  time.Time
}

func (i item) marshal() map[string]any {
	m := map[string]any{"code": i.Code, "value": i.Value, "time": i.Time.In(staging.Zone).Format(time.RFC3339Nano)}
	if i.Unit != "" {
		m["unit"] = i.Unit
	}
	return m
}

// build 组一条上报消息；terminalID 为空表示用默认终端。
func build(terminalID, gatewayID string, items ...item) []byte {
	if terminalID == "" {
		terminalID = testTerminalID
	}
	if gatewayID == "" {
		gatewayID = testGatewayID
	}
	list := make([]any, 0, len(items))
	for _, it := range items {
		list = append(list, it.marshal())
	}
	b, err := json.Marshal(map[string]any{
		"messageId":  fmt.Sprintf("m-%d", time.Now().UnixNano()),
		"gatewayId":  gatewayID,
		"terminalId": terminalID,
		"items":      list,
	})
	if err != nil {
		panic(err)
	}
	return b
}

func (e *testEnv) handle(raw []byte) ingest.Result {
	e.t.Helper()
	return e.proc.Handle(context.Background(), testGatewayID, raw)
}

// latestOf 返回最新值缓存里的一行。
func (e *testEnv) latestOf(itemCode string) (num *float64, txt *string, unit string, valueTime time.Time, ok bool) {
	e.t.Helper()
	err := e.pool.QueryRow(context.Background(), `
		select value_num, value_text, coalesce(unit, ''), value_time from latest_value
		where terminal_id = $1 and item_code = $2`, testTerminalID, itemCode).
		Scan(&num, &txt, &unit, &valueTime)
	if err != nil {
		return nil, nil, "", time.Time{}, false
	}
	return num, txt, unit, valueTime, true
}

func (e *testEnv) requireLatestNum(itemCode string, want float64) time.Time {
	e.t.Helper()
	num, _, _, vt, ok := e.latestOf(itemCode)
	if !ok {
		e.t.Fatalf("latest_value 缺少 %s", itemCode)
	}
	if num == nil || *num != want {
		e.t.Fatalf("latest_value[%s] = %v，期望 %v", itemCode, num, want)
	}
	return vt
}

func (e *testEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatalf("查询计数失败 %q: %v", query, err)
	}
	return n
}

func (e *testEnv) stagingCount() int {
	return e.count(`select count(*) from hourly_staging`)
}

func (e *testEnv) snapshotCount() int {
	return e.count(`select count(*) from hourly_snapshot`)
}

// eventCount 返回某类事件型异常该小时内的总次数。
func (e *testEnv) eventCount(kind exception.ErrorType, hour time.Time) int {
	var total int
	err := e.pool.QueryRow(context.Background(), `
		select coalesce(sum(count), 0) from exception_count
		where terminal_id = $1 and error_type = $2 and hour_start = $3`,
		testTerminalID, string(kind), staging.HourStart(hour)).Scan(&total)
	if err != nil {
		e.t.Fatalf("查询异常计数: %v", err)
	}
	return total
}

// eventRows 返回某类事件型异常该小时的原始行数（应当恒为 0 或 1）。
func (e *testEnv) eventRows(kind exception.ErrorType, hour time.Time) int {
	return e.count(`select count(*) from exception_count
		where terminal_id = $1 and error_type = $2 and hour_start = $3`,
		testTerminalID, string(kind), staging.HourStart(hour))
}

func hourOf(t time.Time) time.Time { return staging.HourStart(t) }

func closeOf(hour time.Time) time.Time { return staging.CloseAt(hour, defaultWindowSec) }

// windowForType 走一次结算窗口解析（type_id 精确匹配 > domain_id > 全局默认）。
func windowForType(e *testEnv, typeID float64) (int, error) {
	return settle.WindowSeconds(context.Background(), e.pool, int64(typeID), e.domainID)
}
