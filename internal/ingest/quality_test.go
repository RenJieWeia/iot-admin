package ingest_test

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
)

// 用例 28：质量规则命中 → 照常入库，快照存真实值，quality_violation +1。
func TestQualityRuleViolationStillPersisted(t *testing.T) {
	e := newEnv(t)
	e.addRule(testTerminalID, "temp", "range", `{"min": 0, "max": 100}`)
	hour := hourOf(baseTime)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", 150.0, "C", at), it("power", 2.0, "kW", at)))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("质量规则违规不得拒收: %+v", r)
	}
	e.requireLatestNum(testTerminalID, "temp", 150.0)
	if n := e.eventCount(testTerminalID, exception.QualityViolation, hour); n != 1 {
		t.Fatalf("quality_violation 计数 = %d，期望 1", n)
	}
	// T6：来源标记必须写进摘要
	if digest := e.digestOf(testTerminalID, exception.QualityViolation); !bytes.Contains([]byte(digest), []byte("source=rule")) {
		t.Fatalf("质量规则命中的摘要应带 source=rule，实际 %q", digest)
	}
	e.clock.Set(closeOf(hour))
	if _, err := e.settler.Run(context.Background()); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	var num float64
	if err := e.pool.QueryRow(context.Background(), `
		select value_num from hourly_snapshot where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&num); err != nil {
		t.Fatalf("快照缺少 temp: %v", err)
	}
	if num != 150.0 {
		t.Fatalf("快照应存真实值 150，实际 %v", num)
	}
}

// 用例 29：单位闭集内换算（kW→W、F→C）结果正确；闭集外单位透传 + clean_failed。
func TestUnitConversionClosedSet(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)

	// 物模型声明 temp=C、power=kW；上游分别发 F 与 W
	res := e.handle(single(testTerminalID,
		it("temp", 50.0, "F", at),
		it("power", 1500.0, "W", at)))
	if r := resultOf(t, res, testTerminalID); !r.Accepted || r.Written != 2 {
		t.Fatalf("闭集内单位换算应入库: %+v", r)
	}
	e.requireLatestNum(testTerminalID, "temp", 10.0)
	e.requireLatestNum(testTerminalID, "power", 1.5)
	var unit string
	if err := e.pool.QueryRow(context.Background(), `
		select coalesce(unit,'') from latest_value where terminal_id = $1 and item_code = 'temp'`,
		testTerminalID).Scan(&unit); err != nil {
		t.Fatalf("读取单位: %v", err)
	}
	if unit != "C" {
		t.Fatalf("换算后单位应为物模型声明单位 C，实际 %q", unit)
	}

	// 闭集外单位（hp）：透传原单位 + clean_failed 计数，不拒收
	e.clock.Add(1 * time.Minute)
	res = e.handle(single(testTerminalID,
		it("temp", 20.0, "C", at.Add(time.Minute)),
		it("power", 3.0, "hp", at.Add(time.Minute))))
	r := resultOf(t, res, testTerminalID)
	if !r.Accepted {
		t.Fatalf("闭集外单位不应拒收: %+v", r)
	}
	num, _, unit, _, ok := e.latestOf(testTerminalID, "power")
	if !ok || num == nil || *num != 3.0 {
		t.Fatalf("闭集外单位应透传原值 3.0，实际 %v", num)
	}
	if unit != "hp" {
		t.Fatalf("闭集外单位应透传原单位 hp，实际 %q", unit)
	}
	if n := e.eventCount(testTerminalID, exception.CleanFailed, hourOf(at.Add(time.Minute))); n != 1 {
		t.Fatalf("clean_failed 计数 = %d，期望 1", n)
	}
}

// T6：物模型 min/max 越界的摘要带 source=thing_model，与规则命中区分。
func TestQualityViolationMarksThingModelSource(t *testing.T) {
	e := newEnv(t)
	if _, err := e.pool.Exec(context.Background(), `
		update thing_model_property set min_value = 0, max_value = 100
		where type_id = $1 and item_code = 'temp'`, e.typeID); err != nil {
		t.Fatalf("设置物模型边界: %v", err)
	}
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", 150.0, "C", at), it("power", 1.0, "kW", at)))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("物模型越界不得拒收: %+v", r)
	}
	digest := e.digestOf(testTerminalID, exception.QualityViolation)
	if !bytes.Contains([]byte(digest), []byte("source=thing_model")) {
		t.Fatalf("物模型越界的摘要应带 source=thing_model，实际 %q", digest)
	}
	if bytes.Contains([]byte(digest), []byte("source=rule")) {
		t.Fatalf("不应出现规则来源标记，实际 %q", digest)
	}
}

// R-022：行级质量标记位（0/1/2/3/4）与结算传播。
func TestQualityFlags(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	hour := hourOf(at)

	qualityOf := func(terminal, code, table string) (int16, string) {
		t.Helper()
		var q int16
		var reason *string
		q2 := `select quality, quality_reason from ` + table +
			` where terminal_id = $1 and item_code = $2`
		if err := e.pool.QueryRow(ctx, q2, terminal, code).Scan(&q, &reason); err != nil {
			t.Fatalf("读取 %s(%s/%s): %v", table, terminal, code, err)
		}
		if reason == nil {
			return q, ""
		}
		return q, *reason
	}

	// 1) 正常：quality=0 且无摘要。
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 25.0, "C", at), it("power", 1.0, "kW", at))), testTerminalID); !r.Accepted {
		t.Fatalf("正常报文应被接收: %+v", r)
	}
	if q, reason := qualityOf(testTerminalID, "temp", "latest_value"); q != 0 || reason != "" {
		t.Fatalf("正常值 quality=%d reason=%q，期望 0/空", q, reason)
	}

	// 2) 仅规则命中 → 2。
	e.addRule(testTerminalID, "temp", "range", `{"min": 0, "max": 100}`)
	e.clock.Add(time.Minute)
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 150.0, "C", at.Add(time.Minute)),
		it("power", 1.0, "kW", at.Add(time.Minute)))), testTerminalID); !r.Accepted {
		t.Fatalf("规则命中不得拒收: %+v", r)
	}
	if q, reason := qualityOf(testTerminalID, "temp", "latest_value"); q != 2 ||
		!strings.Contains(reason, "source=rule") {
		t.Fatalf("规则命中 quality=%d reason=%q，期望 2 + source=rule", q, reason)
	}

	// 3) 物模型越界 + 规则命中 → 3（位可叠加）。
	if _, err := e.pool.Exec(ctx,
		`update thing_model_property set min_value = 0, max_value = 100 where type_id = $1 and item_code = 'temp'`,
		e.typeID); err != nil {
		t.Fatalf("设置物模型边界: %v", err)
	}
	e.proc.Meta.Flush()
	e.clock.Add(time.Minute)
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 150.0, "C", at.Add(2*time.Minute)),
		it("power", 1.0, "kW", at.Add(2*time.Minute)))), testTerminalID); !r.Accepted {
		t.Fatalf("越界不得拒收: %+v", r)
	}
	if q, reason := qualityOf(testTerminalID, "temp", "latest_value"); q != 3 ||
		!strings.Contains(reason, "source=thing_model") || !strings.Contains(reason, "source=rule") {
		t.Fatalf("叠加 quality=%d reason=%q，期望 3 + 两个来源", q, reason)
	}

	// 4) 仅物模型越界（T2 无规则）→ 1；单位闭集外 → 4。
	if r := resultOf(t, e.handle(single(testTerminalB,
		it("temp", 150.0, "C", at.Add(2*time.Minute)),
		it("power", 3.0, "hp", at.Add(2*time.Minute)))), testTerminalB); !r.Accepted {
		t.Fatalf("T2 报文应被接收: %+v", r)
	}
	if q, reason := qualityOf(testTerminalB, "temp", "latest_value"); q != 1 ||
		!strings.Contains(reason, "source=thing_model") {
		t.Fatalf("仅越界 quality=%d reason=%q，期望 1 + source=thing_model", q, reason)
	}
	if q, reason := qualityOf(testTerminalB, "power", "latest_value"); q != 4 ||
		!strings.Contains(reason, "单位不在换算闭集内") {
		t.Fatalf("单位闭集外 quality=%d reason=%q，期望 4", q, reason)
	}

	// 5) 结算：snapshot 的质量标记与 staging 一致（非 0 样本至少 1 例）。
	type row struct {
		q      int16
		reason string
	}
	staging := map[string]row{}
	rows, err := e.pool.Query(ctx, `
		select terminal_id || '/' || item_code, quality, coalesce(quality_reason, '')
		from hourly_staging where hour_start = $1`, hour)
	if err != nil {
		t.Fatalf("读取 staging: %v", err)
	}
	for rows.Next() {
		var k string
		var r row
		if err := rows.Scan(&k, &r.q, &r.reason); err != nil {
			t.Fatalf("扫描 staging: %v", err)
		}
		staging[k] = r
	}
	rows.Close()
	if len(staging) == 0 {
		t.Fatal("staging 为空，无法比较结算传播")
	}

	e.clock.Set(closeOf(hour))
	if _, err := e.settler.Run(ctx); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	nonZero := 0
	rows, err = e.pool.Query(ctx, `
		select terminal_id || '/' || item_code, quality, coalesce(quality_reason, '')
		from hourly_snapshot where hour_start = $1`, hour)
	if err != nil {
		t.Fatalf("读取 snapshot: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var k string
		var r row
		if err := rows.Scan(&k, &r.q, &r.reason); err != nil {
			t.Fatalf("扫描 snapshot: %v", err)
		}
		want, ok := staging[k]
		if !ok {
			t.Fatalf("snapshot 出现 staging 没有的键 %q", k)
		}
		if r.q != want.q || r.reason != want.reason {
			t.Fatalf("%s 结算后 quality=%d/%q，staging 为 %d/%q", k, r.q, r.reason, want.q, want.reason)
		}
		if r.q != 0 {
			nonZero++
		}
		seen++
	}
	if seen != len(staging) {
		t.Fatalf("snapshot 行数 = %d，staging 行数 = %d", seen, len(staging))
	}
	if nonZero == 0 {
		t.Fatal("结算样本里没有非 0 quality，无法验证传播")
	}
}

// 补录回填到已结算小时：仍进 hourly_snapshot、仍走 UPSERT 且 revision 按 value_time 自增。
// 留痕表已下线（R-034 撤销），本用例只锁业务行为：进快照、revision 自增、旧值幂等跳过。
func TestBackfillWritesSnapshotAndRevision(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	// 两个都已关闭的小时：H1 用于实时对照，H2 用于补录序列（互不影响）。
	h1 := hourOf(baseTime)
	h2 := h1.Add(-3 * time.Hour)
	e.clock.Set(h1.Add(2 * time.Hour)) // 两个小时都在窗口外
	at1 := h1.Add(30 * time.Minute)
	at2 := h2.Add(30 * time.Minute)

	// 行不存在时 QueryRow 会报 no rows，因此这条断言同时证明「已进快照」。
	revisionOf := func(code string, hour time.Time) int {
		var rev int
		if err := e.pool.QueryRow(ctx, `
			select revision from hourly_snapshot where terminal_id = $1 and item_code = $2 and hour_start = $3`,
			testTerminalID, code, hour).Scan(&rev); err != nil {
			t.Fatalf("读 revision(%s): %v", code, err)
		}
		return rev
	}
	send := func(msgType int, items ...item) {
		t.Helper()
		payload := batchWithMsgType(strconv.Itoa(msgType), testGatewayID, termOf(testTerminalID, items...))
		if _, err := e.proc.HandleSource(ctx, "mqtt", testGatewayID, payload); err != nil {
			t.Fatalf("处理失败: %v", err)
		}
	}

	// 1) 实时（msgType=1）→ 窗口外直写快照，首写 revision=0。
	send(1, it("temp", 1.0, "C", at1), it("power", 1.0, "kW", at1))
	if rev := revisionOf("temp", h1); rev != 0 {
		t.Fatalf("实时首写 revision = %d，期望 0", rev)
	} else {
		t.Logf("步骤1 实时首写：hourly_snapshot 行存在，revision=%d", rev)
	}

	// 2) 补录（msgType=2）另一个已关闭小时 → 同样进快照，首写 revision=0。
	send(2, it("temp", 2.0, "C", at2), it("power", 2.0, "kW", at2))
	if rev := revisionOf("temp", h2); rev != 0 {
		t.Fatalf("补录首写 revision = %d，期望 0", rev)
	} else {
		t.Logf("步骤2 补录首写：进了已结算小时的快照，revision=%d", rev)
	}

	// 3) 更晚的 value_time → UPSERT 覆盖，revision++。
	send(2, it("temp", 3.0, "C", at2.Add(time.Minute)), it("power", 3.0, "kW", at2.Add(time.Minute)))
	if rev := revisionOf("temp", h2); rev != 1 {
		t.Fatalf("修正后 revision = %d，期望 1", rev)
	} else {
		t.Logf("步骤3 更晚 value_time：UPSERT 覆盖，revision 自增到 %d", rev)
	}

	// 4) 更早的 value_time → 幂等跳过：revision 不变。
	send(2, it("temp", 0.5, "C", at2), it("power", 0.5, "kW", at2))
	if rev := revisionOf("temp", h2); rev != 1 {
		t.Fatalf("幂等跳过后 revision = %d，期望仍为 1", rev)
	} else {
		t.Logf("步骤4 更早 value_time：幂等跳过，revision 保持 %d", rev)
	}
}
