package ingest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/migrations"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

const (
	testDomainCode   = "energy"
	testTypeCode     = "meter"
	testGatewayID    = "GW1"
	testGatewayAlt   = "GW2"
	testTerminalID   = "T1" // GW1
	testTerminalB    = "T2" // GW1
	testTerminalC    = "T4" // GW1
	testTerminalAlt  = "T3" // GW2
	defaultWindowSec = 900
)

// baseTime 固定在一个月中段，避免月末分区边界干扰。
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

func (f *fakePublisher) all() []published {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]published(nil), f.sent...)
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

// newEnv 连接测试库、迁移、清库并种入一套最小数据源。
func newEnv(t *testing.T) *testEnv {
	t.Helper()
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
	if _, err := pool.Exec(ctx, `
		truncate exception_count, exception_interval, latest_value, hourly_staging, snapshot_cell_history;
		truncate hourly_snapshot;
		truncate quality_rule, settle_config, terminal, thing_model_property, device_type, data_domain
			restart identity cascade`); err != nil {
		t.Fatalf("清库: %v", err)
	}

	// 默认平台时刻放在该小时第 55 分钟：窗口内，且 10:00~10:55 的采集时间都不过超前限。
	e := &testEnv{t: t, pool: pool, clock: &testClock{t: baseTime.Add(55 * time.Minute)}, pub: &fakePublisher{}}
	e.seed()
	e.rec = exception.New(pool, e.clock.Now, quietLog)
	e.proc = &ingest.Processor{
		DB:    pool,
		Meta:  meta.NewStore(meta.NewPGLoader(pool), meta.DefaultTTL, e.clock.Now),
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
	// 默认只声明 temp/power：R-021 后"声明的全部字段必填"，收窄声明才能让只发两项的
	// 用例仍然整包通过；需要更多点位的用例用 addProps 补声明。
	props := []struct {
		code      string
		valueType string
		unit      string
		precision int
		required  bool
	}{
		{"temp", "number", "C", 1, true},
		{"power", "number", "kW", 2, true},
	}
	for _, p := range props {
		_, err := e.pool.Exec(ctx, `
			insert into thing_model_property
			    (type_id, item_code, name, value_type, unit, precision, required)
			values ($1, $2, $2, $3, nullif($4, ''), nullif($5, 0), $6)`,
			e.typeID, p.code, p.valueType, p.unit, p.precision, p.required)
		must(err)
	}
	must(e.insertTerminal(testTerminalID, testGatewayID, true))
	must(e.insertTerminal(testTerminalB, testGatewayID, true))
	must(e.insertTerminal(testTerminalC, testGatewayID, true))
	must(e.insertTerminal(testTerminalAlt, testGatewayAlt, true))
	_, err := e.pool.Exec(ctx, `insert into settle_config (domain_id, type_id, window_seconds) values (null, null, 900)`)
	must(err)
}

func (e *testEnv) insertTerminal(id, gateway string, enabled bool) error {
	_, err := e.pool.Exec(context.Background(),
		`insert into terminal (id, type_id, gateway_id, name, enabled) values ($1, $2, $3, $1, $4)`,
		id, e.typeID, gateway, enabled)
	return err
}

// addStdProps 给该终端类型追加 hum/status/online 三个点位。
// R-021 后"声明的全部字段必填"，追加声明即新增必填项——要用它们就必须在报文里发齐。
func (e *testEnv) addStdProps() {
	e.t.Helper()
	ctx := context.Background()
	for _, p := range []struct{ code, valueType string }{
		{"hum", "number"}, {"status", "string"}, {"online", "bool"},
	} {
		if _, err := e.pool.Exec(ctx, `
			insert into thing_model_property (type_id, item_code, name, value_type)
			values ($1, $2, $2, $3)`, e.typeID, p.code, p.valueType); err != nil {
			e.t.Fatalf("追加物模型属性 %s: %v", p.code, err)
		}
	}
}

func (e *testEnv) addRule(terminalID, code, kind, params string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		insert into quality_rule (terminal_id, item_code, rule_type, params) values ($1, $2, $3, $4::jsonb)`,
		terminalID, code, kind, params); err != nil {
		e.t.Fatalf("种入质量规则: %v", err)
	}
}

// ---------- 报文构造 ----------

type item struct {
	Code  string
	Value any
	Unit  string
	At    string // collectedAt 原样字符串；空表示不带该字段
}

func it(code string, v any, unit string, at time.Time) item {
	return item{Code: code, Value: v, Unit: unit, At: at.In(staging.Zone).Format(time.RFC3339Nano)}
}

func rawIt(code string, v any, unit, rawAt string) item {
	return item{Code: code, Value: v, Unit: unit, At: rawAt}
}

// allItems 返回 R-021 之后"一份完整正常报文"需要覆盖的全部点位（需先 addStdProps）。
// 值取常见量级，不带质量违规，用于"只关心其它维度"的用例。
func allItems(at time.Time) []item {
	return []item{
		it("temp", 25.0, "C", at),
		it("power", 1.0, "kW", at),
		it("hum", 55.0, "%", at),
		it("status", "on", "", at),
		it("online", true, "", at),
	}
}

// withItems 以 base 为底，按 code 覆盖同名项、追加新项（保持 base 原有顺序）。
func withItems(base []item, overrides ...item) []item {
	out := make([]item, len(base), len(base)+len(overrides))
	copy(out, base)
	index := make(map[string]int, len(base))
	for i, it := range out {
		index[it.Code] = i
	}
	for _, o := range overrides {
		if i, ok := index[o.Code]; ok {
			out[i] = o
			continue
		}
		index[o.Code] = len(out)
		out = append(out, o)
	}
	return out
}

type term struct {
	ID    string
	Items []item
}

func termOf(id string, items ...item) term { return term{ID: id, Items: items} }

func (t term) marshal() map[string]any {
	items := make([]map[string]any, 0, len(t.Items))
	for _, i := range t.Items {
		m := map[string]any{"code": i.Code, "value": i.Value}
		if i.Unit != "" {
			m["unit"] = i.Unit
		}
		if i.At != "" {
			m["collectedAt"] = i.At
		}
		items = append(items, m)
	}
	return map[string]any{"terminalId": t.ID, "items": items}
}

// batch 构造 v2 批量信封（ver=1）。
func batch(gatewayID string, terms ...term) []byte { return batchVer(1, gatewayID, terms...) }

// batchWithMsgType 构造带可选 msgType 的信封（R-031：仅来源标记）。
func batchWithMsgType(msgType, gatewayID string, terms ...term) []byte {
	return batchWithMeta(msgType, "1", gatewayID, terms...)
}

func batchWithMeta(msgType, ver, gatewayID string, terms ...term) []byte {
	body := map[string]any{
		"messageId": fmt.Sprintf("m-%d", time.Now().UnixNano()),
		"gatewayId": gatewayID,
	}
	if msgType != "" {
		if n, err := strconv.Atoi(msgType); err == nil {
			body["msgType"] = n
		}
	}
	if ver != "" {
		if n, err := strconv.Atoi(ver); err == nil {
			body["ver"] = n
		}
	}
	list := make([]map[string]any, 0, len(terms))
	for _, t := range terms {
		list = append(list, t.marshal())
	}
	body["terminals"] = list
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

// batchVer 指定 ver；ver<=0 表示不带 ver 字段。
func batchVer(ver int, gatewayID string, terms ...term) []byte {
	body := map[string]any{
		"messageId": fmt.Sprintf("m-%d", time.Now().UnixNano()),
		"gatewayId": gatewayID,
	}
	if ver > 0 {
		body["ver"] = ver
	}
	list := make([]map[string]any, 0, len(terms))
	for _, t := range terms {
		list = append(list, t.marshal())
	}
	body["terminals"] = list
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

// single 构造只有一个终端的批量信封。
func single(terminalID string, items ...item) []byte {
	return batch(testGatewayID, termOf(terminalID, items...))
}

func (e *testEnv) handle(raw []byte) ingest.BatchResult {
	e.t.Helper()
	res, err := e.proc.Handle(context.Background(), testGatewayID, raw)
	if err != nil {
		e.t.Fatalf("整包处理失败: %v", err)
	}
	return res
}

func (e *testEnv) handleGateway(ingressGateway string, raw []byte) ingest.BatchResult {
	e.t.Helper()
	res, err := e.proc.Handle(context.Background(), ingressGateway, raw)
	if err != nil {
		e.t.Fatalf("整包处理失败: %v", err)
	}
	return res
}

func resultOf(t *testing.T, res ingest.BatchResult, terminalID string) ingest.Result {
	t.Helper()
	for _, r := range res.Results {
		if r.TerminalID == terminalID {
			return r
		}
	}
	t.Fatalf("结果中没有终端 %s: %+v", terminalID, res.Results)
	return ingest.Result{}
}

// post 走 HTTP handler，返回状态码与解析后的响应体。
func (e *testEnv) post(gatewayID string, body []byte) (int, map[string]any) {
	e.t.Helper()
	h := &ingest.HTTPHandler{P: e.proc}
	req := httptest.NewRequest(http.MethodPost, "/ingest/v1/messages", strings.NewReader(string(body)))
	req.Header.Set(ingest.GatewayHeader, gatewayID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		e.t.Fatalf("响应不是 JSON: %s", rec.Body.String())
	}
	return rec.Code, resp
}

// ---------- 查询辅助 ----------

func (e *testEnv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatalf("查询计数失败 %q: %v", query, err)
	}
	return n
}

func (e *testEnv) latestOf(terminalID, itemCode string) (num *float64, txt *string, unit string, valueTime time.Time, ok bool) {
	e.t.Helper()
	err := e.pool.QueryRow(context.Background(), `
		select value_num, value_text, coalesce(unit, ''), value_time from latest_value
		where terminal_id = $1 and item_code = $2`, terminalID, itemCode).
		Scan(&num, &txt, &unit, &valueTime)
	if err != nil {
		return nil, nil, "", time.Time{}, false
	}
	return num, txt, unit, valueTime, true
}

func (e *testEnv) requireLatestNum(terminalID, itemCode string, want float64) time.Time {
	e.t.Helper()
	num, _, _, vt, ok := e.latestOf(terminalID, itemCode)
	if !ok {
		e.t.Fatalf("latest_value 缺少 %s/%s", terminalID, itemCode)
	}
	if num == nil || *num != want {
		e.t.Fatalf("latest_value[%s/%s] = %v，期望 %v", terminalID, itemCode, num, want)
	}
	return vt
}

// count2 供并发等待使用：查询失败直接返回 0，不打断测试。
func (e *testEnv) count2(query string, args ...any) int {
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

func (e *testEnv) stagingCount() int  { return e.count(`select count(*) from hourly_staging`) }
func (e *testEnv) snapshotCount() int { return e.count(`select count(*) from hourly_snapshot`) }

func (e *testEnv) eventCount(terminalID string, kind exception.ErrorType, hour time.Time) int {
	var total int
	err := e.pool.QueryRow(context.Background(), `
		select coalesce(sum(count), 0) from exception_count
		where terminal_id = $1 and error_type = $2 and hour_start = $3`,
		terminalID, string(kind), staging.HourStart(hour)).Scan(&total)
	if err != nil {
		e.t.Fatalf("查询异常计数: %v", err)
	}
	return total
}

func (e *testEnv) eventRows(terminalID string, kind exception.ErrorType, hour time.Time) int {
	return e.count(`select count(*) from exception_count
		where terminal_id = $1 and error_type = $2 and hour_start = $3`,
		terminalID, string(kind), staging.HourStart(hour))
}

func (e *testEnv) digestOf(terminalID string, kind exception.ErrorType) string {
	e.t.Helper()
	var digest string
	if err := e.pool.QueryRow(context.Background(), `
		select coalesce(sample_digest, '') from exception_count
		where terminal_id = $1 and error_type = $2 order by hour_start desc limit 1`,
		terminalID, string(kind)).Scan(&digest); err != nil {
		e.t.Fatalf("读取摘要: %v", err)
	}
	return digest
}

func hourOf(t time.Time) time.Time     { return staging.HourStart(t) }
func closeOf(hour time.Time) time.Time { return staging.CloseAt(hour, defaultWindowSec) }

// windowForType 走一次结算窗口解析（type_id 精确匹配 > domain_id > 全局默认）。
func windowForType(e *testEnv, typeID int64) (int, error) {
	cfg, err := settle.LoadWindowConfig(context.Background(), e.pool)
	if err != nil {
		return 0, err
	}
	return cfg.Resolve(typeID, e.domainID), nil
}
