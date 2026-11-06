package ingest_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/store"
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
	// R-035：窗口外数据现在会直写快照，对外回到 accepted=true / reason=""。
	if r := resultOf(t, res, testTerminalID); !r.Accepted || r.Reason != "" {
		t.Fatalf("关闭时刻的数据应被受理（窗口外直写快照）: %+v", r)
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

// 用例 19（R-030 重写）：窗口外数据**不再丢弃**——latest_value 照常写、直写快照、
// 不发布实时、仍记 late_data；对外结果口径保持 accepted=false + reason=late_data。
func TestLateDataOutsideWindow(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	at := baseTime.Add(30 * time.Minute)
	e.clock.Set(closeOf(hour).Add(time.Second))
	res := e.handle(single(testTerminalID,
		it("temp", 30.0, "C", at), it("power", 1.0, "kW", at)))
	r := resultOf(t, res, testTerminalID)
	// R-035：数据已落 latest + 快照，对外不再报迟到；late_data 仍以事件形式保留。
	if !r.Accepted || r.Reason != "" {
		t.Fatalf("窗口外数据应被受理（accepted=true / reason=\"\"）: %+v", r)
	}
	if r.Written != 2 {
		t.Fatalf("written = %d，期望 2（staging+snapshot 都算实际写入）", r.Written)
	}
	// 两段模型：latest_value 与 hourly_snapshot 都写入（后者 revision=0），
	// 但 hourly_staging 必须为空（不参与后续结算）。
	if n := e.count(`select count(*) from latest_value`); n != 2 {
		t.Fatalf("窗口外数据应写 latest_value，实际 %d 行", n)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("窗口外数据不应写暂存，实际 %d 行", n)
	}
	if n := e.snapshotCount(); n != 2 {
		t.Fatalf("窗口外数据应直写快照，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from hourly_snapshot where revision <> 0`); n != 0 {
		t.Fatalf("首次直写快照 revision 应为 0，实际 %d 行非 0", n)
	}
	var settled *time.Time
	if err := e.pool.QueryRow(context.Background(),
		`select settled_at from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&settled); err != nil {
		t.Fatalf("读取 settled_at: %v", err)
	}
	if settled == nil {
		t.Fatal("settled_at 应非空")
	}
	if n := e.eventCount(testTerminalID, exception.LateData, hour); n != 2 {
		t.Fatalf("late_data 计数 = %d，期望 2（两个数据项）", n)
	}
	if n := e.eventRows(testTerminalID, exception.LateData, hour); n != 1 {
		t.Fatalf("late_data 行数 = %d，期望 1（按终端/小时汇总）", n)
	}
	if n := e.pub.count(); n != 0 {
		t.Fatalf("窗口外数据不应发布，实际 %d 条", n)
	}
	if _, err := e.settler.Run(context.Background()); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n := e.snapshotCount(); n != 2 {
		t.Fatalf("结算不应改变快照行数，实际 %d 行", n)
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
		it("power", 9.0, "kW", baseTime.Add(50*time.Minute)))), testTerminalID); !r.Accepted {
		t.Fatalf("窗口外数据应被受理（R-035）: %+v", r)
	}
	// R-030：冻结概念作废——更晚的 value_time 会修正快照并 revision+1，且不产生第二行。
	var (
		num      float64
		revision int
	)
	if err := e.pool.QueryRow(context.Background(), `
		select value_num, revision from hourly_snapshot
		where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&num, &revision); err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if num != 99.0 {
		t.Fatalf("更晚的窗口外值应修正快照，实际 %v", num)
	}
	if revision != 1 {
		t.Fatalf("修正后 revision = %d，期望 1", revision)
	}
	if n := e.snapshotCount(); n != 2 {
		t.Fatalf("修正不应新增行，快照行数 = %d，期望 2", n)
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

// R-030：快照版本化 —— 同格只有 value_time 更大才覆盖并 revision+1；相等/更早跳过（幂等）。
func TestSnapshotRevisionUpsert(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	hour := hourOf(baseTime)
	num := func(v float64) *float64 { return &v }
	row := func(minute int, v float64) staging.Row {
		return staging.Row{
			Value:     store.Value{TerminalID: testTerminalID, ItemCode: "temp", ValueNum: num(v), Unit: "C", ValueTime: hour.Add(time.Duration(minute) * time.Minute)},
			HourStart: hour,
		}
	}
	apply := func(rows ...staging.Row) int64 {
		t.Helper()
		var cells []settle.CellRevision
		if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
			var err error
			cells, err = settle.UpsertSnapshot(ctx, tx, rows)
			return err
		}); err != nil {
			t.Fatalf("UpsertSnapshot: %v", err)
		}
		return int64(len(cells))
	}
	type snap struct {
		value    float64
		revision int
		settled  time.Time
	}
	read := func() snap {
		t.Helper()
		var s snap
		if err := e.pool.QueryRow(ctx, `
			select value_num, revision, settled_at from hourly_snapshot
			where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
			testTerminalID, hour).Scan(&s.value, &s.revision, &s.settled); err != nil {
			t.Fatalf("读快照: %v", err)
		}
		return s
	}

	// 1) 首次落库：revision=0。
	if n := apply(row(0, 1.0)); n != 1 {
		t.Fatalf("首次写入影响行数 = %d，期望 1", n)
	}
	first := read()
	if first.value != 1.0 || first.revision != 0 || first.settled.IsZero() {
		t.Fatalf("首次落库 = %+v，期望 value=1 revision=0 settled 非空", first)
	}

	// 2) 更晚的 value_time：覆盖 + revision=1 + settled_at 前进。
	if n := apply(row(5, 2.0)); n != 1 {
		t.Fatalf("更新影响行数 = %d，期望 1", n)
	}
	second := read()
	if second.value != 2.0 || second.revision != 1 {
		t.Fatalf("更新后 = %+v，期望 value=2 revision=1", second)
	}
	if second.settled.Before(first.settled) {
		t.Fatalf("settled_at 应前进或不变: %s → %s", first.settled, second.settled)
	}

	// 3) 更早的 value_time：整行跳过，值/版本/时间都不变。
	if n := apply(row(1, 9.0)); n != 0 {
		t.Fatalf("更早值影响行数 = %d，期望 0", n)
	}
	// 4) 相等 value_time：同样跳过（幂等）。
	if n := apply(row(5, 8.0)); n != 0 {
		t.Fatalf("相等值影响行数 = %d，期望 0", n)
	}
	third := read()
	if third != second {
		t.Fatalf("被跳过的写不应改变快照: %+v → %+v", second, third)
	}
}

// R-030：结算对已存在的快照行走 DO UPDATE，revision 递增（不再静默 DO NOTHING）。
func TestSettleUpdatesExistingSnapshot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	hour := hourOf(baseTime)
	num := func(v float64) *float64 { return &v }

	// 先放一条"旧"快照（模拟此前已结算过）。
	if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		_, err := settle.UpsertSnapshot(ctx, tx, []staging.Row{{
			Value: store.Value{TerminalID: testTerminalID, ItemCode: "temp", ValueNum: num(1.0),
				Unit: "C", ValueTime: hour.Add(10 * time.Minute)},
			HourStart: hour,
		}})
		return err
	}); err != nil {
		t.Fatalf("预置快照: %v", err)
	}

	// 暂存里放更晚的同格数据，走的还是结算路径。
	if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		return staging.Upsert(ctx, tx, []staging.Row{{
			Value: store.Value{TerminalID: testTerminalID, ItemCode: "temp", ValueNum: num(2.0),
				Unit: "C", ValueTime: hour.Add(50 * time.Minute)},
			HourStart: hour,
		}})
	}); err != nil {
		t.Fatalf("预置暂存: %v", err)
	}

	var before int
	if err := e.pool.QueryRow(ctx, `select revision from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&before); err != nil {
		t.Fatalf("读 revision: %v", err)
	}

	e.clock.Set(closeOf(hour).Add(time.Second))
	if _, err := e.settler.Run(ctx); err != nil {
		t.Fatalf("结算失败: %v", err)
	}

	var (
		value    float64
		revision int
	)
	if err := e.pool.QueryRow(ctx, `select value_num, revision from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&value, &revision); err != nil {
		t.Fatalf("读快照: %v", err)
	}
	if value != 2.0 {
		t.Fatalf("结算后快照值 = %v，期望 2.0", value)
	}
	if revision != before+1 {
		t.Fatalf("revision = %d，期望 %d（结算走 DO UPDATE）", revision, before+1)
	}
	if n := e.snapshotCount(); n != 1 {
		t.Fatalf("快照行数 = %d，期望 1（不产生第二行）", n)
	}
}

// R-030：同一批里"一个终端窗口内 + 一个终端窗口外" → 一次事务内分别落 staging / snapshot。
func TestTwoPhaseWriteMixedBatch(t *testing.T) {
	e := newEnv(t)
	now := baseTime.Add(5 * time.Minute)
	e.clock.Set(now)
	inWindowAt := baseTime.Add(3 * time.Minute)
	outWindowAt := baseTime.Add(-3 * time.Hour) // 同一个月的已关闭小时，分区已存在

	res := e.handle(batch(testGatewayID,
		termOf(testTerminalID, it("temp", 21.0, "C", inWindowAt), it("power", 1.5, "kW", inWindowAt)),
		termOf(testTerminalB, it("temp", 22.0, "C", outWindowAt), it("power", 2.5, "kW", outWindowAt)),
	))
	r1 := resultOf(t, res, testTerminalID)
	if !r1.Accepted || r1.Written != 2 || !r1.Published {
		t.Fatalf("窗口内终端应正常入库并发布: %+v", r1)
	}
	r2 := resultOf(t, res, testTerminalB)
	if !r2.Accepted || r2.Reason != "" {
		t.Fatalf("窗口外终端应被受理（R-035）: %+v", r2)
	}
	if r2.Published {
		t.Fatalf("窗口外终端不应发布: %+v", r2)
	}

	if n := e.count(`select count(*) from hourly_staging where terminal_id = $1`, testTerminalID); n != 2 {
		t.Fatalf("窗口内终端暂存行数 = %d，期望 2", n)
	}
	if n := e.count(`select count(*) from hourly_staging where terminal_id = $1`, testTerminalB); n != 0 {
		t.Fatalf("窗口外终端不应进暂存，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from hourly_snapshot where terminal_id = $1`, testTerminalB); n != 2 {
		t.Fatalf("窗口外终端快照行数 = %d，期望 2", n)
	}
	if n := e.count(`select count(*) from hourly_snapshot where terminal_id = $1`, testTerminalID); n != 0 {
		t.Fatalf("窗口内终端不应提前落快照，实际 %d 行", n)
	}
	// 窗口外数据 latest_value 照常写（回拨守卫允许，因为是新格）。
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalB); n != 2 {
		t.Fatalf("窗口外终端 latest 行数 = %d，期望 2", n)
	}
	if n := e.pub.count(); n != 1 {
		t.Fatalf("发布条数 = %d，期望 1（只有窗口内终端）", n)
	}
}

// R-031：late_data 的 detail 带接入来源与 msgType（仅来源标记，不参与路径判定）。
func TestLateDataDetailCarriesSourceAndMsgType(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	at := baseTime.Add(30 * time.Minute)
	e.clock.Set(closeOf(hour).Add(time.Second))

	payload := batchWithMsgType("2", testGatewayID, termOf(testTerminalID,
		it("temp", 30.0, "C", at), it("power", 1.0, "kW", at)))
	res, err := e.proc.HandleSource(context.Background(), "mqtt", testGatewayID, payload)
	if err != nil {
		t.Fatalf("处理失败: %v", err)
	}
	if r := resultOf(t, res, testTerminalID); !r.Accepted || r.Reason != "" {
		t.Fatalf("窗口外数据应被受理（R-035）: %+v", r)
	}
	digest := e.digestOf(testTerminalID, exception.LateData)
	for _, want := range []string{"source=mqtt", "msgType=2(补传)", "item=temp"} {
		if !strings.Contains(digest, want) {
			t.Fatalf("detail 应包含 %q，实际 %q", want, digest)
		}
	}
	// msgType 不参与路径判定：同一份数据换 msgType 仍走同一条路（写快照、不发布）。
	if n := e.snapshotCount(); n != 2 {
		t.Fatalf("快照行数 = %d，期望 2", n)
	}
	if n := e.pub.count(); n != 0 {
		t.Fatalf("不应发布，实际 %d 条", n)
	}
}

// R-034：版本表写入 —— UpsertSnapshot 返回写入后的 revision，InsertVersions 与之对齐。
func TestInsertVersionsWithDB(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	hour := hourOf(baseTime)
	num := func(v float64) *float64 { return &v }
	cell := staging.Row{
		Value: store.Value{
			TerminalID: testTerminalID, ItemCode: "temp", ValueNum: num(1.0), Unit: "C",
			ValueTime: hour.Add(5 * time.Minute),
		},
		HourStart: hour,
	}
	var cells []settle.CellRevision
	if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		var err error
		cells, err = settle.UpsertSnapshot(ctx, tx, []staging.Row{cell})
		return err
	}); err != nil {
		t.Fatalf("写快照: %v", err)
	}
	if len(cells) != 1 || cells[0].Revision != 0 {
		t.Fatalf("首写返回 %+v，期望 1 格 revision=0", cells)
	}
	if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error {
		return settle.InsertVersions(ctx, tx, []settle.VersionRow{{
			TerminalID: cells[0].TerminalID, ItemCode: cells[0].ItemCode, HourStart: cells[0].HourStart,
			Revision: cells[0].Revision, Source: settle.VersionSourceMQTTBackfill,
			ValueTime: cell.ValueTime, Actor: "",
		}})
	}); err != nil {
		t.Fatalf("写版本行: %v", err)
	}
	var (
		revision int
		source   int16
		actor    *string
	)
	if err := e.pool.QueryRow(ctx, `
		select revision, source, actor from snapshot_cell_history
		where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&revision, &source, &actor); err != nil {
		t.Fatalf("读版本行: %v", err)
	}
	if revision != 0 || source != settle.VersionSourceMQTTBackfill || actor != nil {
		t.Fatalf("版本行 revision=%d source=%d actor=%v，期望 0/1/nil", revision, source, actor)
	}
}

// R-034：保留期清理 —— changed_at 早于保留截止月的版本行在 EnsurePartitions 时删除。
func TestDropExpiredRemovesOldVersions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	hour := hourOf(baseTime)
	// 造两条版本行：一条 30 个月前（过期），一条当前。
	insert := func(changedAt time.Time) {
		t.Helper()
		if _, err := e.pool.Exec(ctx, `
			insert into snapshot_cell_history
			    (terminal_id, item_code, hour_start, revision, source, value_time, changed_at)
			values ($1, 'temp', $2, 0, 1, $2, $3)`,
			testTerminalID, hour, changedAt); err != nil {
			t.Fatalf("种版本行: %v", err)
		}
	}
	now := baseTime.Add(24 * time.Hour)
	insert(now.AddDate(0, -30, 0))
	insert(now)

	if err := settle.EnsurePartitions(ctx, e.pool, now); err != nil {
		t.Fatalf("EnsurePartitions: %v", err)
	}
	var n int
	if err := e.pool.QueryRow(ctx, `select count(*) from snapshot_cell_history`).Scan(&n); err != nil {
		t.Fatalf("统计版本行: %v", err)
	}
	if n != 1 {
		t.Fatalf("清理后版本行 = %d，期望 1（只删过期那条）", n)
	}
}

// R-035：补录旧值不报 protocol_error；窗口内回拨仍报。
func TestBackfillOlderValueNoProtocolError(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	hour := hourOf(baseTime)
	at := baseTime.Add(30 * time.Minute)
	send := func(msgType int, items ...item) {
		t.Helper()
		payload := batchWithMsgType(strconv.Itoa(msgType), testGatewayID, termOf(testTerminalID, items...))
		if _, err := e.proc.HandleSource(ctx, "mqtt", testGatewayID, payload); err != nil {
			t.Fatalf("处理失败: %v", err)
		}
	}

	// 窗口外：先补录，再用更早的 value_time 补一次 → 幂等跳过，不写 protocol_error。
	e.clock.Set(closeOf(hour).Add(time.Minute))
	send(2, it("temp", 5.0, "C", at), it("power", 1.0, "kW", at))
	send(2, it("temp", 4.0, "C", at.Add(-5*time.Minute)), it("power", 0.5, "kW", at.Add(-5*time.Minute)))
	if n := e.eventCount(testTerminalID, exception.ProtocolError, hour); n != 0 {
		t.Fatalf("窗口外旧值不应产生 protocol_error，实际 %d", n)
	}

	// 窗口内：先写新值再写旧值 → 回拨仍记 protocol_error。
	e.clock.Set(baseTime.Add(5 * time.Minute))
	send(1, it("temp", 6.0, "C", at.Add(10*time.Minute)), it("power", 2.0, "kW", at.Add(10*time.Minute)))
	send(1, it("temp", 7.0, "C", at), it("power", 3.0, "kW", at))
	if n := e.eventCount(testTerminalID, exception.ProtocolError, hour); n == 0 {
		t.Fatal("窗口内回拨应产生 protocol_error")
	}
}
