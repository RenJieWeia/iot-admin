package ingest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
)

// 用例 11：同设备同类异常并发开启 -> 被 EXCLUDE 约束拦截，不出现双开。
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
	rows := e.count(`select count(*) from exception_interval where terminal_id = $1 and error_type = 'auth_failed'`, testTerminalID)
	if rows != 1 {
		t.Fatalf("并发开启后区间行数 = %d，期望 1（EXCLUDE 拦截双开）", rows)
	}
	if got := e.count(`select count(*) from exception_interval
		where upper_inf(period) and terminal_id = $1 and error_type = 'auth_failed'`, testTerminalID); got != 1 {
		t.Fatalf("未结束区间数 = %d，期望 1", got)
	}
}

// 用例 12：设备永不恢复 -> upper_inf 持续为真，无需任何后台任务。
func TestOpenIntervalStaysOpenWithoutBackgroundJob(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)
	if err := e.rec.OpenInterval(ctx, exception.GatewayDisconnected, testTerminalID, testGatewayID, nil); err != nil {
		t.Fatalf("开启区间失败: %v", err)
	}
	// 时间推进 30 天，平台没有任何"恢复信号"，区间仍为未结束。
	e.clock.Set(baseTime.Add(30 * 24 * time.Hour))
	if got := e.count(`select count(*) from exception_interval
		where terminal_id = $1 and error_type = 'gateway_disconnected' and upper_inf(period)`, testTerminalID); got != 1 {
		t.Fatalf("upper_inf 区间数 = %d，期望 1", got)
	}
	if got := e.count(`select count(*) from exception_interval where terminal_id = $1`, testTerminalID); got != 1 {
		t.Fatalf("区间行数 = %d，期望 1（不自动关闭也不新增）", got)
	}
}

// 用例 13：3 次 2 分钟断续 -> 合并为 1 条，raw_count = 3；间隔超过 5 分钟则不合并。
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
	open()                       // t0
	e.clock.Add(2 * time.Minute) // t0+2m
	closeIt()
	e.clock.Add(2 * time.Minute) // t0+4m
	open()
	e.clock.Add(2 * time.Minute) // t0+6m
	closeIt()
	e.clock.Add(2 * time.Minute) // t0+8m
	open()

	rows := e.count(`select count(*) from exception_interval where terminal_id = $1 and error_type = 'gateway_disconnected'`, testTerminalID)
	if rows != 1 {
		t.Fatalf("抖动合并后区间行数 = %d，期望 1", rows)
	}
	var rawCount int
	if err := e.pool.QueryRow(ctx, `
		select raw_count from exception_interval where terminal_id = $1 and error_type = 'gateway_disconnected'`,
		testTerminalID).Scan(&rawCount); err != nil {
		t.Fatalf("读取 raw_count: %v", err)
	}
	if rawCount != 3 {
		t.Fatalf("raw_count = %d，期望 3", rawCount)
	}
	if got := e.count(`select count(*) from exception_interval
		where terminal_id = $1 and error_type = 'gateway_disconnected' and upper_inf(period)`, testTerminalID); got != 1 {
		t.Fatalf("合并后应为未结束区间，实际未结束数 = %d", got)
	}

	// 间隔 20 分钟：不合并，产生第二条区间。
	closeIt()                     // t0+8m 结束
	e.clock.Add(20 * time.Minute) // t0+28m
	open()
	if rows := e.count(`select count(*) from exception_interval where terminal_id = $1 and error_type = 'gateway_disconnected'`, testTerminalID); rows != 2 {
		t.Fatalf("间隔超过 5 分钟后区间行数 = %d，期望 2", rows)
	}
}

// 用例 14：同一小时内 3000 次失败 -> exception_count 只有 1 行 count=3000。
func TestEventAggregationSingleRow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.clock.Set(baseTime)
	for i := 0; i < 3000; i++ {
		e.rec.Event(ctx, exception.MQTTPublishFailed, testTerminalID, testGatewayID, baseTime, 1, "publish failed")
	}
	if rows := e.eventRows(exception.MQTTPublishFailed, baseTime); rows != 1 {
		t.Fatalf("exception_count 行数 = %d，期望 1", rows)
	}
	if total := e.eventCount(exception.MQTTPublishFailed, baseTime); total != 3000 {
		t.Fatalf("count = %d，期望 3000", total)
	}
	var digest string
	if err := e.pool.QueryRow(ctx, `
		select coalesce(sample_digest, '') from exception_count
		where terminal_id = $1 and error_type = 'mqtt_publish_failed' and hour_start = $2`,
		testTerminalID, hourOf(baseTime)).Scan(&digest); err != nil {
		t.Fatalf("读取摘要: %v", err)
	}
	if digest == "" || len(digest) > 256 {
		t.Fatalf("摘要应只存 hash+长度+截断样本，实际长度 %d: %q", len(digest), digest)
	}
}
