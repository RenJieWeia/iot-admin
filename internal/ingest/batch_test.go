package ingest_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// 用例 1：批量 3 终端，1 个 required 缺失 → 健康 2 个入库并各发布 1 条，
// 失败 1 个出现在 results 且带 missingCodes。
func TestBatchIsolatesFailingTerminal(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(batch(testGatewayID,
		termOf(testTerminalID, it("temp", 25.4, "C", at), it("power", 1.2, "kW", at)),
		termOf(testTerminalC, it("temp", 26.0, "C", at)),
		termOf(testTerminalB, it("temp", 22.0, "C", at), it("power", 2.0, "kW", at)),
	))
	if len(res.Results) != 3 {
		t.Fatalf("results 数量 = %d，期望 3", len(res.Results))
	}
	ok1 := resultOf(t, res, testTerminalID)
	if !ok1.Accepted || ok1.Written != 2 || !ok1.Published {
		t.Fatalf("健康终端 T1 结果异常: %+v", ok1)
	}
	bad := resultOf(t, res, testTerminalC)
	if bad.Accepted || bad.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("缺 required 的终端应拒收 msg_incomplete: %+v", bad)
	}
	if len(bad.MissingCodes) != 1 || bad.MissingCodes[0] != "power" {
		t.Fatalf("missingCodes = %v，期望 [power]", bad.MissingCodes)
	}
	ok2 := resultOf(t, res, testTerminalB)
	if !ok2.Accepted || ok2.Written != 2 || !ok2.Published {
		t.Fatalf("健康终端 T2 结果异常: %+v", ok2)
	}
	if n := e.pub.count(); n != 2 {
		t.Fatalf("发布条数 = %d，期望 2（按终端一条）", n)
	}
	if n := e.count(`select count(*) from latest_value`); n != 4 {
		t.Fatalf("latest_value 行数 = %d，期望 4", n)
	}
}

// 用例 2：value 为 null → 该终端拒收 msg_incomplete，其余终端不受影响。
func TestNullValueRejectsTerminal(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(batch(testGatewayID,
		termOf(testTerminalID, it("temp", nil, "C", at), it("power", 1.0, "kW", at)),
		termOf(testTerminalB, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at)),
	))
	bad := resultOf(t, res, testTerminalID)
	if bad.Accepted || bad.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("null value 应拒收: %+v", bad)
	}
	if len(bad.MissingCodes) != 1 || bad.MissingCodes[0] != "temp" {
		t.Fatalf("missingCodes = %v，期望 [temp]", bad.MissingCodes)
	}
	if ok := resultOf(t, res, testTerminalB); !ok.Accepted {
		t.Fatalf("同批其他终端应正常入库: %+v", ok)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalID); n != 0 {
		t.Fatalf("被拒终端不应写缓存，实际 %d 行", n)
	}
}

// 用例 3：value 为 "25.4" 字符串 → 正常转换入库。
func TestNumericStringValueConverted(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", "25.4", "C", at),
		it("power", "1.75", "kW", at),
	))
	r := resultOf(t, res, testTerminalID)
	if !r.Accepted || r.Written != 2 {
		t.Fatalf("数字字符串应正常入库: %+v", r)
	}
	e.requireLatestNum(testTerminalID, "temp", 25.4)
	e.requireLatestNum(testTerminalID, "power", 1.75)
}

// 用例 4：同终端内 code 重复 → 该终端拒收 msg_incomplete。
func TestDuplicateCodeRejectsTerminal(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", 25.0, "C", at),
		it("temp", 26.0, "C", at),
		it("power", 1.0, "kW", at),
	))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("code 重复应拒收: %+v", r)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("被拒终端不应写缓存，实际 %d 行", n)
	}
}

// 用例 5：未知 code → 忽略该项，其余正常入库，clean_failed 计数 +1（摘要含 code 名）。
func TestUnknownCodeIgnoredAndCounted(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", 25.0, "C", at),
		it("power", 1.0, "kW", at),
		it("bogus", 9.9, "", at),
	))
	r := resultOf(t, res, testTerminalID)
	if !r.Accepted || r.Written != 2 {
		t.Fatalf("未知 code 不应导致拒收: %+v", r)
	}
	if n := e.count(`select count(*) from latest_value where item_code = 'bogus'`); n != 0 {
		t.Fatalf("未知 code 不应入库，实际 %d 行", n)
	}
	if n := e.eventCount(testTerminalID, exception.CleanFailed, e.clock.Now()); n != 1 {
		t.Fatalf("clean_failed 计数 = %d，期望 1", n)
	}
	if digest := e.digestOf(testTerminalID, exception.CleanFailed); !bytes.Contains([]byte(digest), []byte("bogus")) {
		t.Fatalf("摘要应包含 code 名，实际 %q", digest)
	}
}

// R-021 验收：物模型**声明的全部** item_code 必填；未声明项仍忽略并计 clean_failed。
func TestAllDeclaredPropsRequired(t *testing.T) {
	e := newEnv(t)
	e.addStdProps() // temp/power/hum/status/online 全部声明
	at := baseTime.Add(5 * time.Minute)
	full := allItems(at)

	// 1) 缺 hum 的 4 项报文 → msg_incomplete，missingCodes=[hum]。
	withoutHum := make([]item, 0, len(full))
	for _, it := range full {
		if it.Code != "hum" {
			withoutHum = append(withoutHum, it)
		}
	}
	r := resultOf(t, e.handle(single(testTerminalID, withoutHum...)), testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("缺 hum 应判 msg_incomplete: %+v", r)
	}
	if len(r.MissingCodes) != 1 || r.MissingCodes[0] != "hum" {
		t.Fatalf("缺失项 = %v，期望 [hum]", r.MissingCodes)
	}

	// 2) 5 项齐全 + 1 个未声明项 → 接受、写 5 行、clean_failed 计 1。
	res := e.handle(single(testTerminalID, withItems(full, it("bogus", 9.9, "", at))...))
	r = resultOf(t, res, testTerminalID)
	if !r.Accepted || r.Written != len(full) {
		t.Fatalf("全量报文应被接收且写入 %d 项: %+v", len(full), r)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalID); n != len(full) {
		t.Fatalf("latest_value 行数 = %d，期望 %d", n, len(full))
	}
	if n := e.eventCount(testTerminalID, exception.CleanFailed, e.clock.Now()); n != 1 {
		t.Fatalf("未声明项应计 1 次 clean_failed，实际 %d", n)
	}

	// 3) required 列显式置 false 也照样必填（声明即必填）。
	if _, err := e.pool.Exec(context.Background(),
		`update thing_model_property set required = false where type_id = $1`, e.typeID); err != nil {
		t.Fatalf("改 required 列: %v", err)
	}
	e.proc.Meta.Flush()
	r = resultOf(t, e.handle(single(testTerminalID, withoutHum...)), testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("required=false 不应豁免必填: %+v", r)
	}
}

// 用例 6：单终端内 collectedAt 跨度 70 分钟 → 该终端拒收 protocol_error。
func TestSpanOverOneHourRejectsTerminal(t *testing.T) {
	e := newEnv(t)
	start := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", 25.0, "C", start),
		it("power", 1.0, "kW", start.Add(70*time.Minute)),
	))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonProtocolError {
		t.Fatalf("跨度 70 分钟应拒收 protocol_error: %+v", r)
	}
	if n := e.eventCount(testTerminalID, exception.ProtocolError, e.clock.Now()); n != 1 {
		t.Fatalf("protocol_error 计数 = %d，期望 1", n)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("被拒终端不应写缓存，实际 %d 行", n)
	}
}

// 用例 7：接入网关与注册表不符 → 该终端拒收，异常同时保留接入网关与注册网关。
func TestGatewayMismatchWithRegistry(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	payload := batch(testGatewayAlt, termOf(testTerminalID,
		it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)))
	res := e.handleGateway(testGatewayAlt, payload)
	r := resultOf(t, res, testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonProtocolError {
		t.Fatalf("接入网关与注册网关不符应拒收: %+v", r)
	}
	digest := e.digestOf(testTerminalID, exception.ProtocolError)
	if !bytes.Contains([]byte(digest), []byte(testGatewayID)) || !bytes.Contains([]byte(digest), []byte(testGatewayAlt)) {
		t.Fatalf("异常摘要应同时保留接入网关与注册网关，实际 %q", digest)
	}
}

// 用例 8：terminalId 未注册 → 拒收，记 terminal_unknown（不是 auth_failed）。
func TestUnknownTerminalRecordsTerminalUnknown(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single("T-NOPE",
		it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)))
	r := resultOf(t, res, "T-NOPE")
	if r.Accepted || r.Reason != validate.ReasonTerminalUnknown {
		t.Fatalf("未注册终端应记 terminal_unknown: %+v", r)
	}
	if n := e.eventCount("T-NOPE", exception.TerminalUnknown, e.clock.Now()); n != 1 {
		t.Fatalf("terminal_unknown 计数 = %d，期望 1", n)
	}
	if n := e.count(`select count(*) from exception_count where error_type = 'auth_failed'`); n != 0 {
		t.Fatalf("auth_failed 不应由业务校验触发，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from exception_count where error_type = $1`,
		string(exception.AuthFailed)); n != 0 {
		t.Fatal("auth_failed 计数应为 0")
	}
}

// 用例 9：ver = 2 → 整包 400。
func TestUnsupportedVersionRejectedWholeBatch(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	status, resp := e.post(testGatewayID, batchVer(2, testGatewayID,
		termOf(testTerminalID, it("temp", 25.0, "C", at), it("power", 1.0, "kW", at))))
	if status != 400 {
		t.Fatalf("ver=2 应返回 400，实际 %d", status)
	}
	if resp["message"] != validate.ReasonUnsupportedVersion {
		t.Fatalf("reason = %v，期望 %s", resp["message"], validate.ReasonUnsupportedVersion)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("整包被拒不应落库，实际 %d 行", n)
	}
}

// 用例 10：请求体超过 1MB → 400 且 reason = body_too_large。
func TestBodyTooLargeRejected(t *testing.T) {
	e := newEnv(t)
	body := bytes.Repeat([]byte("#"), ingest.MaxBodyBytes+1024)
	status, resp := e.post(testGatewayID, body)
	if status != 400 {
		t.Fatalf("超限请求体应返回 400，实际 %d", status)
	}
	if resp["message"] != validate.ReasonBodyTooLarge {
		t.Fatalf("reason = %v，期望 %s", resp["message"], validate.ReasonBodyTooLarge)
	}
}

// 追加：整包结构错误（terminals 非数组）→ 400 terminals_invalid。
func TestTerminalsNotArrayRejected(t *testing.T) {
	e := newEnv(t)
	status, resp := e.post(testGatewayID, []byte(`{"ver":1,"gatewayId":"GW1","terminals":{"terminalId":"T1"}}`))
	if status != 400 || resp["message"] != validate.ReasonTerminalsInvalid {
		t.Fatalf("terminals 非数组应 400 terminals_invalid，实际 %d %v", status, resp["message"])
	}
}
