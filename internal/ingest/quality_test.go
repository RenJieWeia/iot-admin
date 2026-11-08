package ingest_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
)

// 用例 28：质量规则命中 → 照常入库，最新值存真实值，quality_violation +1。
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
}

// 用例 29：单位只取服务端物模型——网关侧 unit 不换算、不落库、不判质量。
func TestGatewayUnitIgnored(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)

	// 物模型声明 temp=C、power=kW；上游发 F / W 也按原值入库，单位仍取物模型。
	res := e.handle(single(testTerminalID,
		it("temp", 50.0, "F", at),
		it("power", 1500.0, "W", at)))
	if r := resultOf(t, res, testTerminalID); !r.Accepted || r.Written != 2 {
		t.Fatalf("应入库且不换算: %+v", r)
	}
	e.requireLatestNum(testTerminalID, "temp", 50.0)
	e.requireLatestNum(testTerminalID, "power", 1500.0)
	var unit string
	if err := e.pool.QueryRow(context.Background(), `
		select coalesce(unit,'') from latest_value where terminal_id = $1 and item_code = 'temp'`,
		testTerminalID).Scan(&unit); err != nil {
		t.Fatalf("读取单位: %v", err)
	}
	if unit != "C" {
		t.Fatalf("单位应取物模型 C，实际 %q", unit)
	}
	if n := e.eventCount(testTerminalID, exception.CleanFailed, hourOf(at)); n != 0 {
		t.Fatalf("网关单位不应触发 clean_failed，实际 %d", n)
	}

	// 闭集外单位（hp）同样忽略：单位仍是 kW，数值原样保留。
	e.clock.Add(1 * time.Minute)
	res = e.handle(single(testTerminalID,
		it("temp", 20.0, "C", at.Add(time.Minute)),
		it("power", 3.0, "hp", at.Add(time.Minute))))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("不应拒收: %+v", r)
	}
	num, _, unit, _, ok := e.latestOf(testTerminalID, "power")
	if !ok || num == nil || *num != 3.0 {
		t.Fatalf("数值应原样保留 3.0，实际 %v", num)
	}
	if unit != "kW" {
		t.Fatalf("单位应取物模型 kW，实际 %q", unit)
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

	// 4) 仅物模型越界（T2 无规则）→ 1；网关单位 hp 被忽略 → power 无质量位。
	if r := resultOf(t, e.handle(single(testTerminalB,
		it("temp", 150.0, "C", at.Add(2*time.Minute)),
		it("power", 3.0, "hp", at.Add(2*time.Minute)))), testTerminalB); !r.Accepted {
		t.Fatalf("T2 报文应被接收: %+v", r)
	}
	if q, reason := qualityOf(testTerminalB, "temp", "latest_value"); q != 1 ||
		!strings.Contains(reason, "source=thing_model") {
		t.Fatalf("仅越界 quality=%d reason=%q，期望 1 + source=thing_model", q, reason)
	}
	if q, reason := qualityOf(testTerminalB, "power", "latest_value"); q != 0 || reason != "" {
		t.Fatalf("网关单位应被忽略，power quality=%d reason=%q，期望 0/空", q, reason)
	}
}

// 最新值守卫：同 value_time 重发属幂等跳过（不写、不发布）；更早的 value_time 不覆盖已有值，
// 并按「collectedAt 回拨」记 protocol_error。快照/结算已下线，这里只锁最新值语义。
func TestLatestValueGuardAndIdempotentResend(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)

	first := resultOf(t, e.handle(single(testTerminalID, it("temp", 20.0, "C", at))), testTerminalID)
	if !first.Accepted || first.Written != 1 || !first.Published {
		t.Fatalf("首写应落库并发布: %+v", first)
	}

	// 同 value_time 重发：守卫跳过 → 不写、不发布（幂等重发不重复发布）。
	same := resultOf(t, e.handle(single(testTerminalID, it("temp", 20.0, "C", at))), testTerminalID)
	if same.Written != 0 || same.Published {
		t.Fatalf("幂等重发不应重复写入/发布: written=%d published=%v", same.Written, same.Published)
	}
	e.requireLatestNum(testTerminalID, "temp", 20.0)

	// 更早的 value_time：不覆盖，同样记 protocol_error。
	e.clock.Add(time.Minute)
	older := resultOf(t, e.handle(single(testTerminalID, it("temp", 99.0, "C", at.Add(-5*time.Minute)))), testTerminalID)
	if older.Written != 0 {
		t.Fatalf("回拨项不应写入，实际 written=%d", older.Written)
	}
	e.requireLatestNum(testTerminalID, "temp", 20.0)
	// 守卫跳过的两种情形（同值幂等重发、更早的回拨）都会记一条 protocol_error，
	// 与改造前「窗口内未写入即记」的计数口径一致：这里共 2 条。
	if n := e.eventCount(testTerminalID, exception.ProtocolError, hourOf(at)); n != 2 {
		t.Fatalf("守卫跳过的项应各记 1 条 protocol_error，实际 %d", n)
	}
}
