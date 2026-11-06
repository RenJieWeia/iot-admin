package ingest_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/store"
)

// 用例 23：并发开同一区间 → EXCLUDE 拦截，不双开。
func TestConcurrentIntervalOpenStaysSingle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.rec.OpenInterval(ctx, exception.AuthFailed, testTerminalID, testGatewayID, nil); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发开启区间返回错误: %v", err)
	}
	if rows := e.count(`select count(*) from exception_interval`); rows != 1 {
		t.Fatalf("并发开启后区间行数 = %d，期望 1（EXCLUDE 拦截双开）", rows)
	}
}

// 用例 24：永不恢复 → upper_inf 持续为真，无后台任务。
func TestOpenIntervalStaysOpenWithoutBackgroundJob(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)
	if err := e.rec.OpenInterval(ctx, exception.ItemSilent, testTerminalID, testGatewayID, nil); err != nil {
		t.Fatalf("开启区间失败: %v", err)
	}
	e.clock.Set(baseTime.Add(30 * 24 * time.Hour))
	if got := e.count(`select count(*) from exception_interval where upper_inf(period)`); got != 1 {
		t.Fatalf("upper_inf 区间数 = %d，期望 1", got)
	}
	if got := e.count(`select count(*) from exception_interval`); got != 1 {
		t.Fatalf("区间行数 = %d，期望 1（不自动关闭也不新增）", got)
	}
}

// 用例 25：抖动合并 → 3 次 2 分钟断续合并为 1 条，raw_count = 3；间隔过长不合并。
func TestIntervalJitterMerge(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)

	open := func() {
		t.Helper()
		if err := e.rec.OpenInterval(ctx, exception.GatewayDisconnected, testTerminalID, testGatewayID, nil); err != nil {
			t.Fatalf("开启区间失败: %v", err)
		}
	}
	closeIt := func() {
		t.Helper()
		if err := e.rec.CloseInterval(ctx, exception.GatewayDisconnected, testTerminalID, nil); err != nil {
			t.Fatalf("关闭区间失败: %v", err)
		}
	}
	open()
	e.clock.Add(2 * time.Minute)
	closeIt()
	e.clock.Add(2 * time.Minute)
	open()
	e.clock.Add(2 * time.Minute)
	closeIt()
	e.clock.Add(2 * time.Minute)
	open()

	if rows := e.count(`select count(*) from exception_interval where error_type = 'gateway_disconnected'`); rows != 1 {
		t.Fatalf("抖动合并后区间行数 = %d，期望 1", rows)
	}
	var rawCount int
	if err := e.pool.QueryRow(ctx, `
		select raw_count from exception_interval where error_type = 'gateway_disconnected'`).Scan(&rawCount); err != nil {
		t.Fatalf("读取 raw_count: %v", err)
	}
	if rawCount != 3 {
		t.Fatalf("raw_count = %d，期望 3", rawCount)
	}
	closeIt()
	e.clock.Add(20 * time.Minute)
	open()
	if rows := e.count(`select count(*) from exception_interval where error_type = 'gateway_disconnected'`); rows != 2 {
		t.Fatalf("间隔超过 5 分钟后区间行数 = %d，期望 2", rows)
	}
}

// 用例 26：同小时 3000 次失败 → 1 行 count = 3000，摘要只存 hash+长度+截断样本。
func TestEventAggregationSingleRow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)
	for i := 0; i < 3000; i++ {
		e.rec.Event(ctx, exception.MQTTPublishFailed, testTerminalID, testGatewayID, baseTime, 1, "publish failed")
	}
	if rows := e.eventRows(testTerminalID, exception.MQTTPublishFailed, baseTime); rows != 1 {
		t.Fatalf("exception_count 行数 = %d，期望 1", rows)
	}
	if total := e.eventCount(testTerminalID, exception.MQTTPublishFailed, baseTime); total != 3000 {
		t.Fatalf("count = %d，期望 3000", total)
	}
	digest := e.digestOf(testTerminalID, exception.MQTTPublishFailed)
	if digest == "" || len(digest) > 256 {
		t.Fatalf("摘要应只存 hash+长度+截断样本，实际长度 %d: %q", len(digest), digest)
	}
}

// T2：平台故障（DB 不可用）→ 整包 503，results 里逐终端标 internal_error；数据问题仍是 200。
func TestPlatformFailureReturns503(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	body := batch(testGatewayID, termOf(testTerminalID,
		it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)))

	// 正常路径：200
	status, resp := e.post(testGatewayID, body)
	if status != 200 {
		t.Fatalf("正常批量应返回 200，实际 %d %v", status, resp["message"])
	}

	// 平台故障：关闭连接池后同一请求。上一条已把元数据读进缓存，
	// 需同时失效缓存，否则缓存命中会跳过故障库、推迟到写路径才暴露故障。
	e.pool.Close()
	e.proc.Meta.Flush()
	status, resp = e.post(testGatewayID, body)
	if status != 503 {
		t.Fatalf("平台故障应返回 503，实际 %d %v", status, resp["message"])
	}
	if resp["message"] != ingest.ReasonInternal {
		t.Fatalf("message = %v，期望 %s", resp["message"], ingest.ReasonInternal)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("503 响应缺少 data: %v", resp)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("data.results = %v，期望 1 条", data["results"])
	}
	first := results[0].(map[string]any)
	if first["reason"] != ingest.ReasonInternal {
		t.Fatalf("results[0].reason = %v，期望 %s", first["reason"], ingest.ReasonInternal)
	}
	if accepted, _ := first["accepted"].(bool); accepted {
		t.Fatalf("平台故障不应标记 accepted: %v", first)
	}
}

// T1：正常运行不产生任何区间异常（断连写入路径已移除）。
func TestNoIntervalAnomaliesInNormalFlow(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(batch(testGatewayID,
		termOf(testTerminalID, it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)),
		termOf("T-NOPE", it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)),
	))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("正常终端应入库: %+v", r)
	}
	if n := e.count(`select count(*) from exception_interval`); n != 0 {
		t.Fatalf("正常运行不应产生区间异常，实际 %d 行", n)
	}
	// 只有显式调用 OpenInterval 才会有区间行（保留机制，不接生产钩子）
	if err := e.rec.OpenInterval(context.Background(), exception.GatewayDisconnected, testGatewayID, testGatewayID, nil); err != nil {
		t.Fatalf("显式开启区间失败: %v", err)
	}
	if n := e.count(`select count(*) from exception_interval`); n != 1 {
		t.Fatalf("显式开启后区间行数 = %d，期望 1", n)
	}
}

// R-014：写路径平台故障 → 逐终端 internal_error + 整包 503；
// 异常仍记 cache_write_failed（可观测性不因返回 503 而丢）。
func TestWriteFailureReturns503(t *testing.T) {
	dsn := testDSN(t) // 每包独立测试库（task-006 / R3），避免并行跑包互踩
	e := newEnv(t)
	ctx := context.Background()
	at := baseTime.Add(5 * time.Minute)
	body := batch(testGatewayID,
		termOf(testTerminalID, it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)),
		termOf(testTerminalB, it("temp", 26.0, "C", at), it("power", 1.1, "kW", at)))

	// 正常路径先跑一次：元数据进缓存，后续故障只发生在写路径。
	if status, resp := e.post(testGatewayID, body); status != 200 {
		t.Fatalf("正常批量应返回 200，实际 %d %v", status, resp["message"])
	}

	// 只替换写路径的池：异常记录器继续用 e.pool，保证 cache_write_failed 能落库。
	writePool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("连接写路径连接池: %v", err)
	}
	e.proc.DB = writePool
	writePool.Close()

	status, resp := e.post(testGatewayID, body)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("写路径故障应返回 503，实际 %d %v", status, resp["message"])
	}
	if resp["message"] != ingest.ReasonInternal {
		t.Fatalf("message = %v，期望 %s", resp["message"], ingest.ReasonInternal)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("503 响应缺少 data: %v", resp)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("data.results = %v，期望 2 条", data["results"])
	}
	for i, raw := range results {
		r := raw.(map[string]any)
		if r["reason"] != ingest.ReasonInternal {
			t.Fatalf("results[%d].reason = %v，期望 %s", i, r["reason"], ingest.ReasonInternal)
		}
		if accepted, _ := r["accepted"].(bool); accepted {
			t.Fatalf("写失败不得标记 accepted: %v", r)
		}
	}

	// 可观测性：cache_write_failed 仍被记录（不计入整批失败而丢失）。
	total := e.eventCount(testTerminalID, exception.CacheWriteFailed, hourOf(at)) +
		e.eventCount(testTerminalB, exception.CacheWriteFailed, hourOf(at))
	if total < 1 {
		t.Fatalf("cache_write_failed 计数 = %d，期望 ≥1", total)
	}
}
