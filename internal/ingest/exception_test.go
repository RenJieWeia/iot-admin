package ingest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
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

// 用例 27：断连开区间、重连关闭同一行、不同网关互不干扰。
func TestGatewayDisconnectIntervalLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)

	if err := e.rec.MarkGatewaysDisconnected(ctx, e.pool); err != nil {
		t.Fatalf("记录断连失败: %v", err)
	}
	gateways, err := exception.RegisteredGateways(ctx, e.pool)
	if err != nil {
		t.Fatalf("读取网关清单: %v", err)
	}
	if len(gateways) != 2 {
		t.Fatalf("注册网关数 = %d，期望 2（GW1/GW2）", len(gateways))
	}
	if n := e.count(`select count(*) from exception_interval where error_type = 'gateway_disconnected' and upper_inf(period)`); n != 2 {
		t.Fatalf("未结束的断连区间数 = %d，期望 2（按网关各一条）", n)
	}
	// 对象是网关：terminal_id 与 gateway_id 都存网关标识，item_code 为空串
	if n := e.count(`select count(*) from exception_interval
		where error_type = 'gateway_disconnected' and terminal_id = gateway_id and coalesce(item_code,'') = ''`); n != 2 {
		t.Fatalf("断连区间应以网关为对象，实际匹配 %d 行", n)
	}

	e.clock.Add(5 * time.Minute)
	if err := e.rec.MarkGatewaysConnected(ctx); err != nil {
		t.Fatalf("记录重连失败: %v", err)
	}
	if n := e.count(`select count(*) from exception_interval where upper_inf(period)`); n != 0 {
		t.Fatalf("重连后不应留下未结束区间，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from exception_interval`); n != 2 {
		t.Fatalf("重连应关闭同一行而不是新增，实际 %d 行", n)
	}

	// 再次断连：间隔 > 5 分钟后不抖动合并，产生新的两条
	e.clock.Add(10 * time.Minute)
	if err := e.rec.MarkGatewaysDisconnected(ctx, e.pool); err != nil {
		t.Fatalf("再次记录断连失败: %v", err)
	}
	if n := e.count(`select count(*) from exception_interval`); n != 4 {
		t.Fatalf("第二次断连后区间行数 = %d，期望 4", n)
	}
	if n := e.count(`select count(*) from exception_interval where upper_inf(period)`); n != 2 {
		t.Fatalf("第二次断连的未结束区间数 = %d，期望 2", n)
	}
}
