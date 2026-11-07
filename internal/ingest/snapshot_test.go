package ingest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/snapshot"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// setupSnapshot 清空快照表、把六档配置复位为「全关」、清空绑定，并把配置缓存挂到 Processor 上。
func setupSnapshot(t *testing.T, e *testEnv) *snapshot.Store {
	t.Helper()
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `truncate snapshot_coarse, snapshot_minute, snapshot_second`); err != nil {
		t.Fatalf("清空快照表: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `delete from snapshot_binding`); err != nil {
		t.Fatalf("清空绑定: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `update snapshot_slot_config set enabled = false, step = null`); err != nil {
		t.Fatalf("复位六档配置: %v", err)
	}
	store := snapshot.NewStore(snapshot.NewPGLoader(e.pool), snapshot.DefaultTTL, e.clock.Now)
	e.proc.Snap = store
	return store
}

// enableSlots 打开指定档位（granularity → step），并立即失效缓存（等价于管理面写完后的钩子）。
func enableSlots(t *testing.T, e *testEnv, slots map[string]int) {
	t.Helper()
	ctx := context.Background()
	for g, step := range slots {
		if _, err := e.pool.Exec(ctx,
			`update snapshot_slot_config set enabled = true, step = $2 where granularity = $1`, g, step); err != nil {
			t.Fatalf("启用 %s 档: %v", g, err)
		}
	}
	if e.proc.Snap != nil {
		e.proc.Snap.Invalidate()
	}
}

// bind 写一条绑定（一条 = 一个目标 + 一个数据项数组）：
// terminalID 为空 = 类型级（该类型下所有终端），否则 = 实例级。
func bind(t *testing.T, e *testEnv, terminalID string, itemCodes ...string) {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(itemCodes)
	if err != nil {
		t.Fatalf("序列化 item_codes: %v", err)
	}
	if terminalID == "" {
		_, err = e.pool.Exec(ctx,
			`insert into snapshot_binding (device_type_id, item_codes) values ($1, $2::jsonb)`, e.typeID, raw)
	} else {
		_, err = e.pool.Exec(ctx,
			`insert into snapshot_binding (terminal_id, item_codes) values ($1, $2::jsonb)`, terminalID, raw)
	}
	if err != nil {
		t.Fatalf("写绑定: %v", err)
	}
	if e.proc.Snap != nil {
		e.proc.Snap.Invalidate()
	}
}

// 六档全开 + 类型级绑定：一条数据写进粗表 4 行（时/日/月/年）+ 分表 1 + 秒表 1；未绑定的 item 零写入。
func TestSnapshotWritesAllGranularities(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"second": 1, "minute": 1, "hour": 1, "day": 1, "month": 1, "year": 1})
	bind(t, e, "", "temp")

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
	// power 没绑定 → 零写入（绑定是逐序列的，不是整终端开关）。
	if n := e.count(`select count(*) from snapshot_coarse where item_code = 'power'`); n != 0 {
		t.Fatalf("未绑定的 item_code 不应写入，实际 %d 行", n)
	}
}

// 覆盖语义：只有更新的 value_time 才覆盖并 revision+1；同值重发与更早的回拨都不动行。
func TestSnapshotRevisionOnlyOnNewerValue(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"hour": 1})
	bind(t, e, "", "temp")

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
	enableSlots(t, e, map[string]int{"second": 1, "hour": 1})
	bind(t, e, "", "temp")

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

// 配置缓存：未绑定 → 零写入；绑定并 Invalidate 后，下一条消息立即生效（不等 TTL）。
func TestSnapshotInvalidateTakesEffectImmediately(t *testing.T) {
	e := newEnv(t)
	store := setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"hour": 1})

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	e.handle(single(testTerminalID, it("temp", 20.0, "C", at)))
	if n := e.count(`select count(*) from snapshot_coarse`); n != 0 {
		t.Fatalf("未绑定时不应写入快照，实际 %d 行", n)
	}

	bind(t, e, "", "temp")
	store.Invalidate()

	e.clock.Add(time.Minute)
	e.handle(single(testTerminalID, it("temp", 21.0, "C", at.Add(time.Minute))))
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1`, testTerminalID); n != 1 {
		t.Fatalf("Invalidate 后应立即按新绑定写入，实际 %d 行", n)
	}
}

// 类型级绑定覆盖该类型下所有终端；实例级只覆盖那一台。
func TestSnapshotBindingScope(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"hour": 1})
	bind(t, e, "", "temp") // 类型级

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	e.handle(single(testTerminalID, it("temp", 20.0, "C", at)))
	e.handle(single(testTerminalB, it("temp", 20.0, "C", at)))
	if n := e.count(`select count(*) from snapshot_coarse where granularity = 'hour'`); n != 2 {
		t.Fatalf("类型级绑定应对该类型下两台终端都产生行，实际 %d 行", n)
	}

	// 换成实例级：只保留 testTerminalID 的绑定（temp 是夹具里就有的点位）。
	if _, err := e.pool.Exec(context.Background(), `delete from snapshot_binding`); err != nil {
		t.Fatalf("清空绑定: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(), `truncate snapshot_coarse, snapshot_minute, snapshot_second`); err != nil {
		t.Fatalf("清空快照表: %v", err)
	}
	bind(t, e, testTerminalID, "temp")
	e.proc.Snap.Invalidate()
	e.handle(single(testTerminalID, it("temp", 60.0, "C", at)))
	e.handle(single(testTerminalB, it("temp", 60.0, "C", at)))
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1`, testTerminalID); n != 1 {
		t.Fatalf("实例级绑定应命中该终端，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1`, testTerminalB); n != 0 {
		t.Fatalf("实例级绑定不应命中其它终端，实际 %d 行", n)
	}
}

// 双命中（类型级 + 实例级同一序列）：只写一次，revision 不因并集多 +1。
func TestSnapshotDoubleBindingWritesOnce(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"hour": 1})
	bind(t, e, "", "temp")
	bind(t, e, testTerminalID, "temp")

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	if r := resultOf(t, e.handle(single(testTerminalID, it("temp", 20.0, "C", at))), testTerminalID); !r.Accepted {
		t.Fatalf("报文应被接收: %+v", r)
	}
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1 and item_code = 'temp'`, testTerminalID); n != 1 {
		t.Fatalf("双命中也只应写一行，实际 %d 行", n)
	}
	var rev int
	if err := e.pool.QueryRow(context.Background(),
		`select revision from snapshot_coarse where terminal_id = $1 and item_code = 'temp' and granularity = 'hour'`,
		testTerminalID).Scan(&rev); err != nil {
		t.Fatalf("读 revision: %v", err)
	}
	if rev != 0 {
		t.Fatalf("首写 revision = %d，期望 0（并集命中不应多写多 +1）", rev)
	}

	// 再来一条更新的：revision 恰好 +1，不是 +2。
	e.clock.Add(time.Minute)
	e.handle(single(testTerminalID, it("temp", 21.0, "C", at.Add(time.Minute))))
	if err := e.pool.QueryRow(context.Background(),
		`select revision from snapshot_coarse where terminal_id = $1 and item_code = 'temp' and granularity = 'hour'`,
		testTerminalID).Scan(&rev); err != nil {
		t.Fatalf("读 revision: %v", err)
	}
	if rev != 1 {
		t.Fatalf("覆盖后 revision = %d，期望 1", rev)
	}
}

// 删终端 → 实例级绑定随 ON DELETE CASCADE 一起消失。
func TestSnapshotBindingCascadeOnTerminalDelete(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `insert into terminal (id, type_id, gateway_id, name, enabled) values ('TMP-CASCADE', $1, 'GW', 'x', true)`, e.typeID); err != nil {
		t.Fatalf("建临时终端: %v", err)
	}
	bind(t, e, "TMP-CASCADE", "temp")
	if n := e.count(`select count(*) from snapshot_binding where terminal_id = 'TMP-CASCADE'`); n != 1 {
		t.Fatalf("前置绑定未写入")
	}
	if _, err := e.pool.Exec(ctx, `delete from terminal where id = 'TMP-CASCADE'`); err != nil {
		t.Fatalf("删终端: %v", err)
	}
	if n := e.count(`select count(*) from snapshot_binding where terminal_id = 'TMP-CASCADE'`); n != 0 {
		t.Fatalf("删终端后绑定应级联删除，实际 %d 行", n)
	}
}

// 迟到丢弃：同一终端同一小时内多档被跳过 → 只记一条事件（count 累加、detail 列各档）。
func TestSnapshotLateDroppedEventOnce(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"second": 1, "minute": 1, "hour": 1})
	bind(t, e, "", "temp")

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

// 未绑定 / 档位全关 → 不记迟到丢弃事件。
func TestSnapshotLateDroppedNotRecordedWithoutBinding(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"second": 1})
	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(20 * time.Minute))
	e.handle(single(testTerminalID, it("temp", 20.0, "C", at)))
	if n := e.eventRows(testTerminalID, exception.SnapshotLateDropped, staging.HourStart(e.clock.Now())); n != 0 {
		t.Fatalf("未绑定不应记迟到丢弃，实际 %d 行", n)
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

// 一条绑定可含多个数据项：类型级绑定 ["temp","power"] → 两个序列都产生快照行。
func TestSnapshotBindingMultiCodesAreExpanded(t *testing.T) {
	e := newEnv(t)
	setupSnapshot(t, e)
	enableSlots(t, e, map[string]int{"hour": 1})
	bind(t, e, "", "temp", "power")

	at := baseTime.Add(5 * time.Minute)
	e.clock.Set(at.Add(10 * time.Second))
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 20.0, "C", at), it("power", 1.0, "kW", at))), testTerminalID); !r.Accepted {
		t.Fatalf("报文应被接收: %+v", r)
	}
	if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1`, testTerminalID); n != 2 {
		t.Fatalf("小时档行数 = %d，期望 2（temp 与 power 各一行）", n)
	}
	for _, code := range []string{"temp", "power"} {
		if n := e.count(`select count(*) from snapshot_coarse where terminal_id = $1 and item_code = $2`, testTerminalID, code); n != 1 {
			t.Fatalf("%s 的快照行数 = %d，期望 1", code, n)
		}
	}
}
