package ingest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/snapshot"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// setupSnapshot 清空快照表与计划，并把计划缓存挂到 Processor 上（夹具本身不装配快照）。
func setupSnapshot(t *testing.T, e *testEnv) *snapshot.Store {
	t.Helper()
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `truncate snapshot_coarse, snapshot_minute, snapshot_second`); err != nil {
		t.Fatalf("清空快照表: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `delete from snapshot_plan where device_type_id = $1`, e.typeID); err != nil {
		t.Fatalf("清空计划: %v", err)
	}
	store := snapshot.NewStore(snapshot.NewPGLoader(e.pool), snapshot.DefaultTTL, e.clock.Now)
	e.proc.Snap = store
	return store
}

func execPlan(t *testing.T, e *testEnv, sql string, args ...any) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("写快照计划: %v", err)
	}
}

// 六档全开：一条数据写进粗表 4 行（时/日/月/年）+ 分表 1 + 秒表 1；未配置的 item 零写入。
func TestSnapshotWritesAllGranularities(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	execPlan(t, e, `
		insert into snapshot_plan (device_type_id, item_code,
			second_enabled, second_step, minute_enabled, minute_step,
			hour_enabled, hour_step, day_enabled, day_step,
			month_enabled, month_step, year_enabled, year_step)
		values ($1, 'temp', true, 1, true, 1, true, 1, true, 1, true, 1, true, 1)`, e.typeID)
	e.proc.Snap.Invalidate()

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 20.0, "C", at), it("power", 1.0, "kW", at))), testTerminalID); !r.Accepted {
		t.Fatalf("报文应被接收: %+v", r)
	}

	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1`, testTerminalID); n != 4 {
		t.Fatalf("粗表行数 = %d，期望 4（时/日/月/年各一行）", n)
	}
	if n := e.count(`select count(*) from snapshot_minute where terminal_id = $1`, testTerminalID); n != 1 {
		t.Fatalf("分表行数 = %d，期望 1", n)
	}
	if n := e.count(`select count(*) from snapshot_second where terminal_id = $1`, testTerminalID); n != 1 {
		t.Fatalf("秒表行数 = %d，期望 1", n)
	}
	// power 没在计划里 → 零写入（白名单是逐序列的）。
	if n := e.count(`select count(*) from snapshot_coarse where item_code = 'power'`); n != 0 {
		t.Fatalf("白名单外的 item_code 不应写入，实际 %d 行", n)
	}
}

// 覆盖语义：只有更新的 value_time 才覆盖并 revision+1；同值重发与更早的回拨都不动行。
func TestSnapshotRevisionOnlyOnNewerValue(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	execPlan(t, e, `
		insert into snapshot_plan (device_type_id, item_code, hour_enabled, hour_step)
		values ($1, 'temp', true, 1)`, e.typeID)
	e.proc.Snap.Invalidate()

	hourRow := func() (int, float64) {
		t.Helper()
		var rev int
		var num float64
		if err := e.pool.QueryRow(context.Background(), `
			select revision, value_num from snapshot_coarse
			where terminal_id = $1 and item_code = 'temp' and granularity = 'hour'`,
			testTerminalID).Scan(&rev, &num); err != nil {
			t.Fatalf("读小时档: %v", err)
		}
		return rev, num
	}

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	if r := resultOf(t, e.handle(single(testTerminalID, it("temp", 20.0, "C", at))), testTerminalID); !r.Accepted {
		t.Fatalf("首条应接收: %+v", r)
	}
	if rev, num := hourRow(); rev != 0 || num != 20 {
		t.Fatalf("首写 revision=%d num=%v，期望 0/20", rev, num)
	}

	// 更新的采集时间 → 覆盖并 revision+1
	e.clock.Add(time.Minute)
	if r := resultOf(t, e.handle(single(testTerminalID, it("temp", 25.0, "C", at.Add(time.Minute)))), testTerminalID); !r.Accepted {
		t.Fatalf("第二条应接收: %+v", r)
	}
	if rev, num := hourRow(); rev != 1 || num != 25 {
		t.Fatalf("覆盖后 revision=%d num=%v，期望 1/25", rev, num)
	}

	// 同值重发：守卫跳过，revision 不变
	e.clock.Add(time.Minute)
	e.handle(single(testTerminalID, it("temp", 25.0, "C", at.Add(time.Minute))))
	if rev, num := hourRow(); rev != 1 || num != 25 {
		t.Fatalf("同值重发后 revision=%d num=%v，期望仍为 1/25", rev, num)
	}

	// 更早的回拨：同样不覆盖
	e.clock.Add(time.Minute)
	e.handle(single(testTerminalID, it("temp", 30.0, "C", at)))
	if rev, num := hourRow(); rev != 1 || num != 25 {
		t.Fatalf("回拨后 revision=%d num=%v，期望仍为 1/25", rev, num)
	}
}

// 每档独立判迟到：二十多分钟后秒档已过期，小时档仍可写。
func TestSnapshotLateSkipsOnlyThatGranularity(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	execPlan(t, e, `
		insert into snapshot_plan (device_type_id, item_code, second_enabled, second_step, hour_enabled, hour_step)
		values ($1, 'temp', true, 1, true, 1)`, e.typeID)
	e.proc.Snap.Invalidate()

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(20 * time.Minute)) // 秒段（+15m 宽限）已过期，小时段仍可写
	if r := resultOf(t, e.handle(single(testTerminalID, it("temp", 20.0, "C", at))), testTerminalID); !r.Accepted {
		t.Fatalf("迟到不应拒收（快照只是跳过该档）: %+v", r)
	}
	if n := e.count(`select count(*) from snapshot_second where terminal_id = $1`, testTerminalID); n != 0 {
		t.Fatalf("秒档应被迟到判定跳过，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1 and granularity = 'hour'`, testTerminalID); n != 1 {
		t.Fatalf("小时档仍应写入，实际 %d 行", n)
	}
}

// 计划缓存：未配置 → 零写入；写入计划并 Invalidate 后，下一条消息立即生效。
func TestSnapshotPlanCacheInvalidate(t *testing.T) {
	e := newEnv(t)
	store := setupSnapshot(t, e)

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	e.handle(single(testTerminalID, it("temp", 20.0, "C", at)))
	if n := e.count(`select count(*) from snapshot_coarse`); n != 0 {
		t.Fatalf("未配置计划时不应写入快照，实际 %d 行", n)
	}

	execPlan(t, e, `
		insert into snapshot_plan (device_type_id, item_code, hour_enabled, hour_step)
		values ($1, 'temp', true, 1)`, e.typeID)
	store.Invalidate()

	e.clock.Add(time.Minute)
	e.handle(single(testTerminalID, it("temp", 21.0, "C", at.Add(time.Minute))))
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1`, testTerminalID); n != 1 {
		t.Fatalf("Invalidate 后应立即按新计划写入，实际 %d 行", n)
	}
}

// 分区维护：续建未来分区 + 按保留期清理整日/整月分区。
func TestSnapshotPartitionMaintenance(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	now := baseTime // 2026-11-05 10:00 +08

	if err := snapshot.EnsurePartitions(ctx, e.pool, now); err != nil {
		t.Fatalf("续建分区: %v", err)
	}
	exists := func(name string) bool {
		t.Helper()
		var oid *string
		if err := e.pool.QueryRow(ctx, `select to_regclass($1)::text`, name).Scan(&oid); err != nil {
			t.Fatalf("查询分区 %s: %v", name, err)
		}
		return oid != nil
	}
	for i := 0; i <= 3; i++ {
		if name := "snapshot_coarse_" + now.AddDate(0, i, 0).Format("200601"); !exists(name) {
			t.Fatalf("粗表缺少未来第 %d 个月的分区 %s", i, name)
		}
	}
	for i := 0; i <= 8; i++ {
		if name := "snapshot_second_" + now.AddDate(0, 0, i).Format("20060102"); !exists(name) {
			t.Fatalf("秒表缺少未来第 %d 天的分区 %s", i, name)
		}
	}

	// 保留期 1 天：早于 cutoff 的秒表日分区应被整日 DROP。
	// 造一个确定落在保留期外的历史分区（迁移铺的窗口是相对真实 now 的，不依赖它）。
	old := "snapshot_second_20260101"
	if _, err := e.pool.Exec(ctx, `create table if not exists `+old+
		` partition of snapshot_second for values from ('2026-01-01T00:00:00+08:00') to ('2026-01-02T00:00:00+08:00')`); err != nil {
		t.Fatalf("建历史分区: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `update snapshot_settings set retention_days = 1 where table_name = 'snapshot_second'`); err != nil {
		t.Fatalf("设置保留期: %v", err)
	}
	if err := snapshot.PurgeExpired(ctx, e.pool, now); err != nil {
		t.Fatalf("清理过期分区: %v", err)
	}
	if exists(old) {
		t.Fatalf("保留期 1 天时 %s 应被清理", old)
	}
	if !exists("snapshot_second_" + time.Now().In(staging.Zone).Format("20060102")) {
		t.Fatal("当天分区不应被清理")
	}
}

// 迟到丢弃：同一终端同一小时内多档被跳过 → 只记一条事件（count 累加、detail 列各档）。
func TestSnapshotLateDroppedEventOnce(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	execPlan(t, e, `
		insert into snapshot_plan (device_type_id, item_code,
			second_enabled, second_step, minute_enabled, minute_step, hour_enabled, hour_step)
		values ($1, 'temp', true, 1, true, 1, true, 1)`, e.typeID)
	e.proc.Snap.Invalidate()

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(20 * time.Minute)) // 秒/分档已过期，小时档仍可写
	eventHour := staging.HourStart(e.clock.Now())
	e.handle(single(testTerminalID, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at)))

	if n := e.eventRows(testTerminalID, exception.SnapshotLateDropped, eventHour); n != 1 {
		t.Fatalf("迟到丢弃行数 = %d，期望 1（多档合并成一条）", n)
	}
	if n := e.eventCount(testTerminalID, exception.SnapshotLateDropped, eventHour); n != 1 {
		t.Fatalf("迟到丢弃计数 = %d，期望 1", n)
	}
	if digest := e.digestOf(testTerminalID, exception.SnapshotLateDropped); !strings.Contains(digest, "granularity=second") ||
		!strings.Contains(digest, "late=") {
		t.Fatalf("detail 应含档位与迟到时长，实际 %q", digest)
	}
	// power 没在计划里：它的丢弃不该被算进来（detail 里只有 temp 的秒/分两档）。

	// 同一事件小时内的第二条消息：仍是一行，count 累加。
	e.clock.Add(time.Minute)
	e.handle(single(testTerminalID, it("temp", 21.0, "C", at.Add(time.Minute))))
	if n := e.eventRows(testTerminalID, exception.SnapshotLateDropped, eventHour); n != 1 {
		t.Fatalf("同一小时第二条消息后行数 = %d，期望仍为 1", n)
	}
	if n := e.eventCount(testTerminalID, exception.SnapshotLateDropped, eventHour); n != 2 {
		t.Fatalf("同一小时第二条消息后计数 = %d，期望 2", n)
	}
}

// 未配置计划 / 档位未启用 → 不记迟到丢弃事件。
func TestSnapshotLateDroppedNotRecordedWithoutPlan(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(20 * time.Minute))
	e.handle(single(testTerminalID, it("temp", 20.0, "C", at)))
	if n := e.eventRows(testTerminalID, exception.SnapshotLateDropped, staging.HourStart(e.clock.Now())); n != 0 {
		t.Fatalf("未配置计划不应记迟到丢弃，实际 %d 行", n)
	}
}
