package ingest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/staging"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// 用例 11：无偏移 "2026-11-05T10:00:00" → 按 +08:00 解析，归属 10 点。
func TestCollectedAtWithoutOffsetDefaultsToShanghai(t *testing.T) {
	e := newEnv(t)
	want := time.Date(2026, 11, 5, 10, 0, 0, 0, staging.Zone)
	e.clock.Set(want.Add(3 * time.Minute))
	res := e.handle(single(testTerminalID,
		rawIt("temp", 25.0, "C", "2026-11-05T10:00:00"),
		rawIt("power", 1.0, "kW", "2026-11-05T10:00:00")))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("无偏移时间应可解析: %+v", r)
	}
	if vt := e.requireLatestNum(testTerminalID, "temp", 25.0); !vt.Equal(want) {
		t.Fatalf("value_time = %s，期望 %s", vt, want)
	}
	if n := e.count(`select count(*) from hourly_staging where hour_start = $1`, want); n != 2 {
		t.Fatalf("小时归属应为 10:00，实际匹配 %d 行", n)
	}
}

// 用例 12："2026-11-05T02:00:00Z" → 换算后归属 10 点（不是 2 点）。
func TestCollectedAtWithZConvertsToShanghaiHour(t *testing.T) {
	e := newEnv(t)
	want := time.Date(2026, 11, 5, 10, 0, 0, 0, staging.Zone)
	e.clock.Set(want.Add(3 * time.Minute))
	res := e.handle(single(testTerminalID,
		rawIt("temp", 25.0, "C", "2026-11-05T02:00:00Z"),
		rawIt("power", 1.0, "kW", "2026-11-05T02:00:00Z")))
	if r := resultOf(t, res, testTerminalID); !r.Accepted {
		t.Fatalf("带 Z 的时间应可解析: %+v", r)
	}
	if vt := e.requireLatestNum(testTerminalID, "temp", 25.0); !vt.Equal(want) {
		t.Fatalf("value_time = %s，期望 %s", vt, want)
	}
	if n := e.count(`select count(*) from hourly_staging where hour_start = $1`, want); n != 2 {
		t.Fatalf("小时归属应为 10:00，实际匹配 %d 行", n)
	}
}

// 用例 13：空格分隔 "2026-11-05 10:00:00" → 两项都 time_unparseable（项级失败）。
// 全部项失败 → accepted=false、reason 为空；不再整终端 msg_incomplete。
func TestCollectedAtWithSpaceSeparatorFailsItems(t *testing.T) {
	e := newEnv(t)
	res := e.handle(single(testTerminalID,
		rawIt("temp", 25.0, "C", "2026-11-05 10:00:00"),
		rawIt("power", 1.0, "kW", "2026-11-05 10:00:00")))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted {
		t.Fatalf("两项时间都不可解析，不应 accepted: %+v", r)
	}
	if r.Reason != validate.ReasonAllItemsFailed {
		t.Fatalf("项级全失败的 reason 应为 %q，实际 %q", validate.ReasonAllItemsFailed, r.Reason)
	}
	m := resultWire(t, r)
	if got := wireInt(t, m, "failedCount"); got != 2 {
		t.Fatalf("failedCount = %d，期望 2", got)
	}
	items := wireItems(t, m)
	if len(items) != 2 {
		t.Fatalf("items 长度 = %d，期望 2: %v", len(items), items)
	}
	for _, item := range items {
		if item["reason"] != itemReasonTimeUnparseable {
			t.Fatalf("项级原因 = %v，期望 %s", item["reason"], itemReasonTimeUnparseable)
		}
	}
	// 兼容字段只收 value_missing；时间不可解析不进 missingCodes（与改造前不同，契约明示）。
	if len(r.MissingCodes) != 0 {
		t.Fatalf("missingCodes = %v，期望空（只收 value_missing）", r.MissingCodes)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("全失败不应写缓存，实际 %d 行", n)
	}
}

// 用例 14：采集时间超前 6 分钟 → 该终端拒收 protocol_error。
func TestFutureCollectedAtRejected(t *testing.T) {
	e := newEnv(t)
	res := e.handle(single(testTerminalID,
		it("temp", 25.0, "C", e.clock.Now().Add(6*time.Minute)),
		it("power", 1.0, "kW", e.clock.Now().Add(6*time.Minute))))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonProtocolError {
		t.Fatalf("超前 6 分钟应拒收: %+v", r)
	}
	if n := e.eventCount(testTerminalID, exception.ProtocolError, e.clock.Now()); n != 1 {
		t.Fatalf("protocol_error 计数 = %d，期望 1", n)
	}
}

// 用例 15：采集时间倒退 → 不覆盖，记 protocol_error。
func TestCollectedAtRegressionDoesNotOverwrite(t *testing.T) {
	e := newEnv(t)
	newer := baseTime.Add(50 * time.Minute)
	older := baseTime.Add(40 * time.Minute)
	if r := resultOf(t, e.handle(single(testTerminalID,
		it("temp", 40.0, "C", newer), it("power", 3.0, "kW", newer))), testTerminalID); !r.Accepted {
		t.Fatalf("首条应入库: %+v", r)
	}
	res := e.handle(single(testTerminalID,
		it("temp", 10.0, "C", older), it("power", 1.0, "kW", older)))
	r := resultOf(t, res, testTerminalID)
	if r.Written != 0 || r.Published {
		t.Fatalf("倒退数据不应写入/发布: %+v", r)
	}
	if vt := e.requireLatestNum(testTerminalID, "temp", 40.0); !vt.Equal(newer) {
		t.Fatalf("latest_value 被倒退值覆盖: %s", vt)
	}
	if n := e.eventCount(testTerminalID, exception.ProtocolError, hourOf(newer)); n != 2 {
		t.Fatalf("protocol_error 计数 = %d，期望 2（两个数据项各一次）", n)
	}
	if n := e.pub.count(); n != 1 {
		t.Fatalf("倒退消息不应发布，实际 %d 条", n)
	}
}

// 用例 16：同小时三条消息乱序到达 → 缓存与快照均取 collectedAt 最大者。
func TestOutOfOrderMessagesKeepMaxCollectedAt(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	cases := []struct {
		at time.Time
		v  float64
	}{
		{baseTime.Add(30 * time.Minute), 31.5},
		{baseTime.Add(10 * time.Minute), 21.5},
		{baseTime.Add(50 * time.Minute), 41.5},
	}
	for _, c := range cases {
		r := resultOf(t, e.handle(single(testTerminalID,
			it("temp", c.v, "C", c.at), it("power", 1.25, "kW", c.at))), testTerminalID)
		if !r.Accepted {
			t.Fatalf("乱序消息应入库，at=%s 结果=%+v", c.at, r)
		}
	}
	want := baseTime.Add(50 * time.Minute)
	if vt := e.requireLatestNum(testTerminalID, "temp", 41.5); !vt.Equal(want) {
		t.Fatalf("latest_value.collectedAt 最大者 = %s，期望 %s", vt, want)
	}
	last := e.pub.all()
	var p publish.Payload
	if err := json.Unmarshal(last[len(last)-1].payload, &p); err != nil {
		t.Fatalf("实时报文非法: %v", err)
	}
	for _, item := range p.Items {
		if item.Code == "temp" && (item.Value.(float64) != 41.5 || item.CollectedAt != want.Format(time.RFC3339Nano)) {
			t.Fatalf("实时报文 temp = %v@%s，期望 41.5@%s", item.Value, item.CollectedAt, want)
		}
	}
	e.clock.Set(closeOf(hour))
	n, err := e.settler.Run(context.Background())
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("结算行数 = %d，期望 2", n)
	}
	var num float64
	var vt time.Time
	if err := e.pool.QueryRow(context.Background(), `
		select value_num, value_time from hourly_snapshot
		where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&num, &vt); err != nil {
		t.Fatalf("快照缺少 temp: %v", err)
	}
	if num != 41.5 || !vt.Equal(want) {
		t.Fatalf("快照 temp = %v@%s，期望 41.5@%s", num, vt, want)
	}
}

// 用例 17：单条消息内数据项跨小时 → 各自归属不同小时。
func TestItemsInSameMessageBelongToDifferentHours(t *testing.T) {
	e := newEnv(t)
	first := time.Date(2026, 11, 5, 10, 50, 0, 0, staging.Zone)
	second := time.Date(2026, 11, 5, 11, 10, 0, 0, staging.Zone)
	e.clock.Set(second.Add(1 * time.Minute))
	res := e.handle(single(testTerminalID,
		it("temp", 20.0, "C", first),
		it("power", 2.0, "kW", second)))
	r := resultOf(t, res, testTerminalID)
	if !r.Accepted || r.Written != 2 {
		t.Fatalf("跨小时数据项应分别入库: %+v", r)
	}
	hours := e.count(`select count(distinct hour_start) from hourly_staging where terminal_id = $1`, testTerminalID)
	if hours != 2 {
		t.Fatalf("暂存覆盖小时数 = %d，期望 2", hours)
	}
	if n := e.count(`select count(*) from hourly_staging where hour_start = $1 and item_code = 'temp'`, hourOf(first)); n != 1 {
		t.Fatalf("temp 应归属 10 点小时，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from hourly_staging where hour_start = $1 and item_code = 'power'`, hourOf(second)); n != 1 {
		t.Fatalf("power 应归属 11 点小时，实际 %d 行", n)
	}
}
