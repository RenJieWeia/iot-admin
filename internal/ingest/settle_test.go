package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// 用例 18：窗口边界 —— 恰好等于关闭时刻判迟到；关闭前 1 秒在窗口内；关闭时刻可结算。
func TestWindowBoundaryIsClosedAtCloseTime(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	closeAt := closeOf(hour)
	at := baseTime.Add(30 * time.Minute)

	e.clock.Set(closeAt)
	res := e.handle(single(testTerminalID,
		it("temp", 30.0, "C", at), it("power", 1.0, "kW", at)))
	if r := resultOf(t, res, testTerminalID); r.Accepted || r.Reason != validateReasonLateData() {
		t.Fatalf("恰好关闭时刻应判迟到（闭区间）: %+v", r)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("关闭时刻的数据不应入暂存，实际 %d 行", n)
	}

	e.clock.Set(closeAt.Add(-time.Second))
	res = e.handle(single(testTerminalID,
		it("temp", 30.0, "C", at), it("power", 1.0, "kW", at)))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("关闭前 1 秒应仍在窗口内: %+v", r)
	}

	e.clock.Set(closeAt)
	n, err := e.settler.Run(context.Background())
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("关闭时刻结算行数 = %d，期望 2", n)
	}
}

// 用例 19：迟到数据 → 记 late_data，不写缓存，快照不含该值。
func TestLateDataOutsideWindow(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	at := baseTime.Add(30 * time.Minute)
	e.clock.Set(closeOf(hour).Add(time.Second))
	res := e.handle(single(testTerminalID,
		it("temp", 30.0, "C", at), it("power", 1.0, "kW", at)))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted || r.Reason != string(exception.LateData) {
		t.Fatalf("窗口外数据应记 late_data: %+v", r)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("迟到数据不应写缓存，实际 %d 行", n)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("迟到数据不应写暂存，实际 %d 行", n)
	}
	if n := e.eventCount(testTerminalID, exception.LateData, hour); n != 2 {
		t.Fatalf("late_data 计数 = %d，期望 2（两个数据项）", n)
	}
	if n := e.eventRows(testTerminalID, exception.LateData, hour); n != 1 {
		t.Fatalf("late_data 行数 = %d，期望 1（按终端/小时汇总）", n)
	}
	if n := e.pub.count(); n != 0 {
		t.Fatalf("迟到数据不应发布，实际 %d 条", n)
	}
	if _, err := e.settler.Run(context.Background()); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n := e.snapshotCount(); n != 0 {
		t.Fatalf("快照不应包含迟到值，实际 %d 行", n)
	}
}

// 用例 20：某小时无任何数据 → 不生成快照行（断言 count = 0）。
func TestHourWithoutDataProducesNoSnapshotRow(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(closeOf(hourOf(baseTime)).Add(2 * time.Hour))
	if _, err := e.settler.Run(context.Background()); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n := e.snapshotCount(); n != 0 {
		t.Fatalf("无数据小时不应生成快照，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from hourly_snapshot where value_num is null and value_text is null`); n != 0 {
		t.Fatalf("快照中不应出现空值行，实际 %d 行", n)
	}
}

// 用例 21：重复结算 → 不产生第二行；冻结后迟到值不能改写快照。
func TestRepeatedSettleProducesNoSecondRow(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 12.5, "C", baseTime.Add(10*time.Minute)),
		it("power", 0.5, "kW", baseTime.Add(10*time.Minute)))), testTerminalID); !r.Accepted {
		t.Fatalf("消息应入库: %+v", r)
	}
	e.clock.Set(closeOf(hour).Add(time.Second))
	n1, err := e.settler.Run(context.Background())
	if err != nil || n1 != 2 {
		t.Fatalf("首轮结算 rows=%d err=%v，期望 2", n1, err)
	}
	n2, err := e.settler.Run(context.Background())
	if err != nil {
		t.Fatalf("重复结算报错: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("重复结算新增行数 = %d，期望 0", n2)
	}
	if n := e.snapshotCount(); n != 2 {
		t.Fatalf("快照行数 = %d，期望 2", n)
	}
	e.clock.Set(closeOf(hour).Add(2 * time.Minute))
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 99.0, "C", baseTime.Add(50*time.Minute)),
		it("power", 9.0, "kW", baseTime.Add(50*time.Minute)))), testTerminalID); r.Accepted {
		t.Fatalf("冻结后到达的数据应按迟到处理: %+v", r)
	}
	var num float64
	if err := e.pool.QueryRow(context.Background(), `
		select value_num from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&num); err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if num != 12.5 {
		t.Fatalf("冻结快照被改写: %v", num)
	}
}

// 用例 22：月度分区切换 —— 月初边界落在新月份分区；24 个月后旧分区 DROP、新分区可写。
func TestMonthlyPartitionsAndMonthBoundaryRows(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	monthStart := time.Date(2026, 11, 1, 0, 0, 0, 0, staging.Zone)
	if err := settle.EnsurePartitions(ctx, e.pool, monthStart.Add(12*time.Hour)); err != nil {
		t.Fatalf("建分区: %v", err)
	}
	insert := func(hourStart time.Time, code string) {
		t.Helper()
		if _, err := e.pool.Exec(ctx, `
			insert into hourly_snapshot (terminal_id, item_code, hour_start, value_num, value_time)
			values ('T1', $1, $2, 1.0, $2)`, code, hourStart); err != nil {
			t.Fatalf("写入 %s: %v", hourStart, err)
		}
	}
	insert(monthStart.Add(-time.Second), "temp")                               // 2026-10-31 23:59:59+08
	insert(monthStart, "temp")                                                 // 2026-11-01 00:00:00+08
	insert(monthStart.Add(7*time.Hour+59*time.Minute+59*time.Second), "power") // 2026-11-01 07:59:59+08

	if n := e.count(`select count(*) from hourly_snapshot_202610`); n != 1 {
		t.Fatalf("2026-10 分区行数 = %d，期望 1", n)
	}
	if n := e.count(`select count(*) from hourly_snapshot_202611`); n != 2 {
		t.Fatalf("2026-11 分区行数 = %d，期望 2（月初 00:00:00 与 07:59:59 均落新月份）", n)
	}

	future := time.Date(2029, 2, 15, 0, 0, 0, 0, staging.Zone)
	if err := settle.EnsurePartitions(ctx, e.pool, future); err != nil {
		t.Fatalf("切换分区失败: %v", err)
	}
	var exists bool
	if err := e.pool.QueryRow(ctx, `select to_regclass('hourly_snapshot_202611') is not null`).Scan(&exists); err != nil {
		t.Fatalf("检查旧分区: %v", err)
	}
	if exists {
		t.Fatal("超过 24 个月保留期的 2026-11 分区应已被 DROP")
	}
	if _, err := e.pool.Exec(ctx, `
		insert into hourly_snapshot (terminal_id, item_code, hour_start, value_num, value_time)
		values ('T9', 'temp', $1, 5.0, $1)`, future.Add(10*time.Hour)); err != nil {
		t.Fatalf("新分区不可写: %v", err)
	}
	if n := e.snapshotCount(); n != 1 {
		t.Fatalf("DROP 后快照总行数 = %d，期望 1（旧分区物理消失）", n)
	}
}

func validateReasonLateData() string { return string(exception.LateData) }
