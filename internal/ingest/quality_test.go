package ingest_test

import (
	"context"
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
