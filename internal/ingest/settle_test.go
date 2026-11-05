package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// 用例 5：某小时无任何数据 -> 不生成快照行（不是 NULL 行）。
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

// 用例 6：同一小时重复结算 -> 不产生第二行。
func TestRepeatedSettleProducesNoSecondRow(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	if res := e.handle(build("", "",
		item{Code: "temp", Value: 12.5, Unit: "C", Time: baseTime.Add(10 * time.Minute)},
		item{Code: "power", Value: 0.5, Unit: "kW", Time: baseTime.Add(10 * time.Minute)})); !res.Accepted {
		t.Fatalf("消息应入库: %+v", res)
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
		t.Fatalf("快照行数 = %d，期望 2（ON CONFLICT DO NOTHING）", n)
	}
	// 新到的迟到值不能改写已冻结快照
	e.clock.Set(closeOf(hour).Add(2 * time.Minute))
	if res := e.handle(build("", "",
		item{Code: "temp", Value: 99.0, Unit: "C", Time: baseTime.Add(50 * time.Minute)},
		item{Code: "power", Value: 9.0, Unit: "kW", Time: baseTime.Add(50 * time.Minute)})); res.Accepted {
		t.Fatalf("冻结后到达的数据应按迟到处理，实际 %+v", res)
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

// 用例 10：暂存跨月无影响；月度分区切换后旧分区 DROP 成功、新分区可写。
func TestMonthlyPartitionsAndMonthBoundaryStaging(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	oct := time.Date(2026, 10, 31, 23, 0, 0, 0, staging.Zone)
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, staging.Zone)
	if err := settle.EnsurePartitions(ctx, e.pool, nov); err != nil {
		t.Fatalf("建分区: %v", err)
	}
	// 跨月两个小时的窗口互不重叠：先在 00:05 写入 10-31 23:00 的数据，再在 00:35 写入 11-01 00:00 的数据。
	e.clock.Set(nov.Add(5 * time.Minute))
	if res := e.handle(build("", "",
		item{Code: "temp", Value: 1.0, Unit: "C", Time: oct.Add(30 * time.Minute)},
		item{Code: "power", Value: 2.0, Unit: "kW", Time: oct.Add(30 * time.Minute)})); !res.Accepted {
		t.Fatalf("跨月小时 %s 写入失败: %+v", oct, res)
	}
	e.clock.Set(nov.Add(35 * time.Minute))
	if res := e.handle(build("", "",
		item{Code: "temp", Value: 3.0, Unit: "C", Time: nov.Add(30 * time.Minute)},
		item{Code: "power", Value: 4.0, Unit: "kW", Time: nov.Add(30 * time.Minute)})); !res.Accepted {
		t.Fatalf("跨月小时 %s 写入失败: %+v", nov, res)
	}
	e.clock.Set(nov.Add(time.Hour + 15*time.Minute + time.Second)) // 两个小时的关闭时刻都已过
	n, err := e.settler.Run(ctx)
	if err != nil {
		t.Fatalf("跨月结算失败: %v", err)
	}
	if n != 4 {
		t.Fatalf("跨月结算行数 = %d，期望 4", n)
	}
	var octRows, novRows int
	if err := e.pool.QueryRow(ctx, `select count(*) from hourly_snapshot_202610`).Scan(&octRows); err != nil {
		t.Fatalf("读取 2026-10 分区: %v", err)
	}
	if err := e.pool.QueryRow(ctx, `select count(*) from hourly_snapshot_202611`).Scan(&novRows); err != nil {
		t.Fatalf("读取 2026-11 分区: %v", err)
	}
	if octRows != 2 || novRows != 2 {
		t.Fatalf("分区行数 = (%d,%d)，期望 (2,2)", octRows, novRows)
	}

	// 24 个月后：旧分区被 DROP，新分区可写。
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
	if n := e.count(`select count(*) from hourly_snapshot`); n != 1 {
		t.Fatalf("DROP 后快照总行数 = %d，期望 1（旧分区物理消失）", n)
	}
}
