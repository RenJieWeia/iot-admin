package ingest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/publish"
)

// 用例 1：required 属性缺失 -> 拒收，latest_value 与 staging 均无变化。
func TestRequiredPropertyMissingRejected(t *testing.T) {
	e := newEnv(t)
	res := e.handle(build("", "", item{Code: "temp", Value: 25.0, Unit: "C", Time: baseTime}))
	if res.Accepted {
		t.Fatalf("缺 required 属性不应入库: %+v", res)
	}
	if res.Reason != string(exception.MsgIncomplete) {
		t.Fatalf("拒收原因 = %q，期望 %q", res.Reason, exception.MsgIncomplete)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("latest_value 行数 = %d，期望 0", n)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("hourly_staging 行数 = %d，期望 0", n)
	}
	if n := e.eventCount(exception.MsgIncomplete, baseTime); n != 1 {
		t.Fatalf("msg_incomplete 计数 = %d，期望 1", n)
	}
}

// 用例 2：同小时三条消息乱序到达 -> 缓存与快照取 value_time 最大者，且发布内容与落库一致。
func TestOutOfOrderMessagesKeepMaxValueTime(t *testing.T) {
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
		res := e.handle(build("", "",
			item{Code: "temp", Value: c.v, Unit: "C", Time: c.at},
			item{Code: "power", Value: 1.25, Unit: "kW", Time: c.at}))
		if !res.Accepted {
			t.Fatalf("乱序消息应入库，time=%s 结果=%+v", c.at, res)
		}
	}
	wantTime := baseTime.Add(50 * time.Minute)
	if got := e.requireLatestNum("temp", 41.5); !got.Equal(wantTime) {
		t.Fatalf("latest_value.temp 的 value_time = %s，期望 %s", got, wantTime)
	}

	// 发布内容 = 最后一次写入的数据
	last := e.pub.last(t)
	var p publish.Payload
	if err := json.Unmarshal(last.payload, &p); err != nil {
		t.Fatalf("实时报文非法: %v", err)
	}
	if last.topic != "iot/realtime/energy/meter/T1" {
		t.Fatalf("实时主题 = %q", last.topic)
	}
	for _, it := range p.Items {
		if it.Code == "temp" {
			if it.Value.(float64) != 41.5 {
				t.Fatalf("实时报文 temp = %v，期望 41.5", it.Value)
			}
			if it.Time != wantTime.Format(time.RFC3339Nano) {
				t.Fatalf("实时报文 temp time = %s", it.Time)
			}
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
		where terminal_id = $1 and item_code = 'temp' and hour_start = $2`, testTerminalID, hour).Scan(&num, &vt); err != nil {
		t.Fatalf("快照缺少 temp: %v", err)
	}
	if num != 41.5 || !vt.Equal(wantTime) {
		t.Fatalf("快照 temp = %v@%s，期望 41.5@%s", num, vt, wantTime)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("结算后暂存应为空，实际 %d 行", n)
	}
}

// 用例 3：迟到数据落窗口外 -> 记 late_data，不写缓存，快照不含该值。
func TestLateDataOutsideWindow(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	e.clock.Set(closeOf(hour).Add(time.Second))
	res := e.handle(build("", "",
		item{Code: "temp", Value: 30.0, Unit: "C", Time: baseTime.Add(30 * time.Minute)},
		item{Code: "power", Value: 1.0, Unit: "kW", Time: baseTime.Add(30 * time.Minute)}))
	if res.Accepted || res.Reason != string(exception.LateData) {
		t.Fatalf("窗口外消息应记 late_data，实际 %+v", res)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("迟到数据不应写缓存，latest_value 行数 = %d", n)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("迟到数据不应写暂存，staging 行数 = %d", n)
	}
	if n := e.eventCount(exception.LateData, hour); n != 2 {
		t.Fatalf("late_data 计数 = %d，期望 2（两个数据项）", n)
	}
	if n := e.eventRows(exception.LateData, hour); n != 1 {
		t.Fatalf("late_data 行数 = %d，期望 1（按终端/小时汇总）", n)
	}
	if n := e.pub.count(); n != 0 {
		t.Fatalf("迟到数据不应发布实时主题，实际发布 %d 条", n)
	}
	if _, err := e.settler.Run(context.Background()); err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n := e.snapshotCount(); n != 0 {
		t.Fatalf("快照不应包含迟到值，实际 %d 行", n)
	}
}

// 用例 4：窗口边界 —— 恰好等于关闭时刻判为迟到；关闭前 1 秒仍在窗口内；关闭时刻可结算。
func TestWindowBoundaryIsClosedAtCloseTime(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	closeAt := closeOf(hour)

	e.clock.Set(closeAt)
	res := e.handle(build("", "",
		item{Code: "temp", Value: 30.0, Unit: "C", Time: baseTime.Add(30 * time.Minute)},
		item{Code: "power", Value: 1.0, Unit: "kW", Time: baseTime.Add(30 * time.Minute)}))
	if res.Accepted || res.Reason != string(exception.LateData) {
		t.Fatalf("恰好关闭时刻应判迟到（闭区间），实际 %+v", res)
	}
	if n := e.stagingCount(); n != 0 {
		t.Fatalf("关闭时刻的消息不应入暂存，实际 %d 行", n)
	}

	e.clock.Set(closeAt.Add(-time.Second))
	res = e.handle(build("", "",
		item{Code: "temp", Value: 30.0, Unit: "C", Time: baseTime.Add(30 * time.Minute)},
		item{Code: "power", Value: 1.0, Unit: "kW", Time: baseTime.Add(30 * time.Minute)}))
	if !res.Accepted {
		t.Fatalf("关闭前 1 秒应仍在窗口内，实际 %+v", res)
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

// 用例 7：质量规则命中 -> 照常入库，快照存真实值，异常计数 +1。
func TestQualityRuleViolationStillPersisted(t *testing.T) {
	e := newEnv(t)
	e.addRule("temp", "range", `{"min": 0, "max": 100}`)
	hour := hourOf(baseTime)
	res := e.handle(build("", "",
		item{Code: "temp", Value: 150.0, Unit: "C", Time: baseTime.Add(5 * time.Minute)},
		item{Code: "power", Value: 2.0, Unit: "kW", Time: baseTime.Add(5 * time.Minute)}))
	if !res.Accepted {
		t.Fatalf("质量规则违规不得拒收，实际 %+v", res)
	}
	if got := e.requireLatestNum("temp", 150.0); got.IsZero() {
		t.Fatal("latest_value 应写入真实值")
	}
	if n := e.eventCount(exception.QualityViolation, hour); n != 1 {
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
	if n := e.eventRows(exception.QualityViolation, hour); n != 1 {
		t.Fatalf("quality_violation 行数 = %d，期望 1（按批次汇总）", n)
	}
}

// 用例 8：采集时间超前 6 分钟 -> 拒收 protocol_error；超前 4 分钟 -> 正常入库。
func TestFutureTimestampRejected(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(baseTime) // 以 10:00 为平台时刻，才能构造"超前 6 分钟"
	res := e.handle(build("", "", item{Code: "temp", Value: 20.0, Unit: "C", Time: baseTime.Add(6 * time.Minute)}))
	if res.Accepted || res.Reason != string(exception.ProtocolError) {
		t.Fatalf("超前 6 分钟应拒收 protocol_error，实际 %+v", res)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("超前消息不应入库，latest_value = %d 行", n)
	}
	res = e.handle(build("", "",
		item{Code: "temp", Value: 20.0, Unit: "C", Time: baseTime.Add(4 * time.Minute)},
		item{Code: "power", Value: 1.0, Unit: "kW", Time: baseTime.Add(4 * time.Minute)}))
	if !res.Accepted {
		t.Fatalf("超前 4 分钟应接受，实际 %+v", res)
	}
	if n := e.eventCount(exception.ProtocolError, baseTime); n != 1 {
		t.Fatalf("protocol_error 计数 = %d，期望 1", n)
	}
}

// 用例 9：采集时间倒退 -> 不覆盖，记 protocol_error。
func TestTimeRegressionDoesNotOverwrite(t *testing.T) {
	e := newEnv(t)
	hour := hourOf(baseTime)
	newer := baseTime.Add(50 * time.Minute)
	older := baseTime.Add(40 * time.Minute)
	if res := e.handle(build("", "",
		item{Code: "temp", Value: 40.0, Unit: "C", Time: newer},
		item{Code: "power", Value: 3.0, Unit: "kW", Time: newer})); !res.Accepted {
		t.Fatalf("首条消息应入库: %+v", res)
	}
	res := e.handle(build("", "",
		item{Code: "temp", Value: 10.0, Unit: "C", Time: older},
		item{Code: "power", Value: 1.0, Unit: "kW", Time: older}))
	if res.Written != 0 {
		t.Fatalf("时间倒退不应写入，实际 %+v", res)
	}
	if got := e.requireLatestNum("temp", 40.0); !got.Equal(newer) {
		t.Fatalf("latest_value 被倒退值覆盖: %s", got)
	}
	var vt time.Time
	if err := e.pool.QueryRow(context.Background(), `
		select value_time from hourly_staging where terminal_id = $1 and item_code = 'temp' and hour_start = $2`,
		testTerminalID, hour).Scan(&vt); err != nil {
		t.Fatalf("暂存缺少 temp: %v", err)
	}
	if !vt.Equal(newer) {
		t.Fatalf("暂存被倒退值覆盖: %s", vt)
	}
	if n := e.eventCount(exception.ProtocolError, hour); n != 2 {
		t.Fatalf("protocol_error 计数 = %d，期望 2（两个数据项各一次）", n)
	}
	if n := e.pub.count(); n != 1 {
		t.Fatalf("倒退消息不应发布，实际发布 %d 条", n)
	}
}
