package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// ---------- 契约字面量 ----------

// 与 ingest.MaxItemResults / validate.ItemValueType / ItemValueMissing / ItemTimeUnparseable 对齐。
// 这里集中用字面量而不是引用实现常量，是为了让**同一份用例**也能在改造前的实现上编译，
// 从而拿到「旧实现红 / 新实现绿」的对照证据；改契约时这四处必须同步。
const (
	itemReasonValueType       = "value_type"
	itemReasonValueMissing    = "value_missing"
	itemReasonTimeUnparseable = "time_unparseable"
	maxItemResults            = 20
)

// ---------- 应答契约的断言辅助 ----------

// resultWire 把一条逐终端结果转成对外 JSON 形状。
// 用 JSON 而不是直接读字段，是为了让同一份用例在改造前后都能编译：
// 红/绿只来自行为差异，而不是「字段不存在」的新旧类型差异。
func resultWire(t *testing.T, r ingest.Result) map[string]any {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("序列化逐终端结果: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("反序列化逐终端结果: %v", err)
	}
	return m
}

// wireInt 读必填的数值字段；缺字段直接失败（改造前没有 failedCount）。
func wireInt(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("应答缺字段 %q（契约要求）：%v", key, m)
	}
	f, ok := raw.(float64)
	if !ok {
		t.Fatalf("字段 %q 不是数字：%v", key, raw)
	}
	return int(f)
}

// wireIntOrZero 读可缺省的数值字段：契约里 failedCount/items 都带 omitempty，
// 零值不会出现在 JSON 上，此时按 0 处理。
func wireIntOrZero(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0
	}
	f, ok := raw.(float64)
	if !ok {
		t.Fatalf("字段 %q 不是数字：%v", key, raw)
	}
	return int(f)
}

// wireItems 读 items 明细；字段缺失按「空」处理（结构性拒收时 omitempty 不出现）。
func wireItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["items"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("items 不是数组：%v", raw)
	}
	out := make([]map[string]any, len(list))
	for i, v := range list {
		item, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("items[%d] 不是对象：%v", i, v)
		}
		out[i] = item
	}
	return out
}

// itemReason 取某个 code 的项级失败原因；不存在返回空串。
func itemReason(items []map[string]any, code string) string {
	for _, item := range items {
		if item["code"] == code {
			reason, _ := item["reason"].(string)
			return reason
		}
	}
	return ""
}

// addProp 给该终端类型追加一个物模型点位（声明即必填，用前先想清楚报文要发齐）。
func (e *testEnv) addProp(code, valueType string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `
		insert into thing_model_property (type_id, item_code, name, value_type)
		values ($1, $2, $2, $3)`, e.typeID, code, valueType); err != nil {
		e.t.Fatalf("追加物模型属性 %s: %v", code, err)
	}
}

// addProps 追加 n 个 number 类型点位（p00、p01…），用于上限类用例。
func (e *testEnv) addProps(n int) []string {
	e.t.Helper()
	codes := make([]string, n)
	for i := 0; i < n; i++ {
		codes[i] = fmt.Sprintf("p%02d", i)
		e.addProp(codes[i], "number")
	}
	return codes
}

// ---------- 批量与项级失败 ----------

// 用例 1：批量 3 终端；T4 缺 power → 只该项失败（value_missing），temp 照常入库；
// 其余终端不受影响。项级失败不再让整终端作废。
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
	if !bad.Accepted {
		t.Fatalf("缺一项不应整终端拒收，实际 %+v", bad)
	}
	if bad.Written != 1 {
		t.Fatalf("其余项应入库，written = %d，期望 1: %+v", bad.Written, bad)
	}
	badWire := resultWire(t, bad)
	if got := wireInt(t, badWire, "failedCount"); got != 1 {
		t.Fatalf("failedCount = %d，期望 1: %v", got, badWire)
	}
	items := wireItems(t, badWire)
	if len(items) != 1 || items[0]["code"] != "power" || items[0]["reason"] != itemReasonValueMissing {
		t.Fatalf("items = %v，期望 [{power value_missing}]", items)
	}
	// 兼容保留：missingCodes 仍列出 value_missing 的 code。
	if len(bad.MissingCodes) != 1 || bad.MissingCodes[0] != "power" {
		t.Fatalf("missingCodes = %v，期望 [power]", bad.MissingCodes)
	}

	ok2 := resultOf(t, res, testTerminalB)
	if !ok2.Accepted || ok2.Written != 2 || !ok2.Published {
		t.Fatalf("健康终端 T2 结果异常: %+v", ok2)
	}
	// 三个终端都写入成功过，各发布一条。
	if n := e.pub.count(); n != 3 {
		t.Fatalf("发布条数 = %d，期望 3（按终端一条）", n)
	}
	// 直查库：失败项没进 latest_value，其余项进去了（2+1+2=5）。
	if n := e.count(`select count(*) from latest_value`); n != 5 {
		t.Fatalf("latest_value 行数 = %d，期望 5", n)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalC); n != 1 {
		t.Fatalf("T4 应只写入 1 项，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1 and item_code = 'power'`,
		testTerminalC); n != 0 {
		t.Fatalf("T4 的失败项 power 不应入库，实际 %d 行", n)
	}
}

// 用例 2：value 为 null → 只该项 value_missing，其余项入库，其他终端不受影响。
func TestNullValueFailsOnlyThatItem(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(batch(testGatewayID,
		termOf(testTerminalID, it("temp", nil, "C", at), it("power", 1.0, "kW", at)),
		termOf(testTerminalB, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at)),
	))
	bad := resultOf(t, res, testTerminalID)
	if !bad.Accepted || bad.Written != 1 {
		t.Fatalf("null 值只应让该项失败: %+v", bad)
	}
	m := resultWire(t, bad)
	if got := wireInt(t, m, "failedCount"); got != 1 {
		t.Fatalf("failedCount = %d，期望 1", got)
	}
	if reason := itemReason(wireItems(t, m), "temp"); reason != itemReasonValueMissing {
		t.Fatalf("temp 的项级原因 = %q，期望 value_missing: %v", reason, m["items"])
	}
	if len(bad.MissingCodes) != 1 || bad.MissingCodes[0] != "temp" {
		t.Fatalf("missingCodes = %v，期望 [temp]", bad.MissingCodes)
	}
	if ok := resultOf(t, res, testTerminalB); !ok.Accepted {
		t.Fatalf("同批其他终端应正常入库: %+v", ok)
	}
	// 直查库：null 的那项没进去，power 进去了。
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalID); n != 1 {
		t.Fatalf("应写入 1 项，实际 %d 行", n)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1 and item_code = 'temp'`,
		testTerminalID); n != 0 {
		t.Fatalf("value 为 null 的项不应入库，实际 %d 行", n)
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

// 用例 4：同终端内 code 重复 → 结构性拒收（回归护栏，行为不变，items 必须为空）。
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
	if items := wireItems(t, resultWire(t, r)); len(items) != 0 {
		t.Fatalf("结构性拒收不应带项级明细，实际 %v", items)
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
	// 物模型未声明的 code 继续忽略并计 clean_failed，不算项级失败。
	if got := wireIntOrZero(t, resultWire(t, r), "failedCount"); got != 0 {
		t.Fatalf("未声明 code 不计项级失败，failedCount = %d，期望 0", got)
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

// R-021 验收：物模型**声明的全部** item_code 必填；缺失降级为项级 value_missing。
func TestAllDeclaredPropsRequired(t *testing.T) {
	e := newEnv(t)
	e.addStdProps() // temp/power/hum/status/online 全部声明
	at := baseTime.Add(5 * time.Minute)
	full := allItems(at)

	// 1) 缺 hum 的 4 项报文 → 其余 4 项入库，hum 记项级 value_missing。
	withoutHum := make([]item, 0, len(full))
	for _, it := range full {
		if it.Code != "hum" {
			withoutHum = append(withoutHum, it)
		}
	}
	r := resultOf(t, e.handle(single(testTerminalID, withoutHum...)), testTerminalID)
	if !r.Accepted || r.Written != len(withoutHum) {
		t.Fatalf("缺一项应只记项级失败: %+v", r)
	}
	m := resultWire(t, r)
	if got := wireInt(t, m, "failedCount"); got != 1 {
		t.Fatalf("failedCount = %d，期望 1", got)
	}
	if reason := itemReason(wireItems(t, m), "hum"); reason != itemReasonValueMissing {
		t.Fatalf("hum 的项级原因 = %q，期望 value_missing", reason)
	}
	if len(r.MissingCodes) != 1 || r.MissingCodes[0] != "hum" {
		t.Fatalf("缺失项 = %v，期望 [hum]", r.MissingCodes)
	}

	// 2) 5 项齐全 + 1 个未声明项 → 接受、写 5 行、clean_failed 计 1。
	res := e.handle(single(testTerminalID, withItems(full, it("bogus", 9.9, "", at))...))
	r = resultOf(t, res, testTerminalID)
	if !r.Accepted {
		t.Fatalf("全量报文应被接收: %+v", r)
	}
	// 第 1 步已把其余 4 项写入，本步只有新增的 hum 真正落库（相同 value_time 幂等跳过）。
	if r.Written != 1 {
		t.Fatalf("written = %d，期望 1（仅新增项 hum）: %+v", r.Written, r)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalID); n != len(full) {
		t.Fatalf("latest_value 行数 = %d，期望 %d", n, len(full))
	}
	if n := e.eventCount(testTerminalID, exception.CleanFailed, e.clock.Now()); n != 1 {
		t.Fatalf("未声明项应计 1 次 clean_failed，实际 %d", n)
	}

	// 3) required 列显式置 false 也照样必填（声明即必填）：仍是项级 value_missing。
	if _, err := e.pool.Exec(context.Background(),
		`update thing_model_property set required = false where type_id = $1`, e.typeID); err != nil {
		t.Fatalf("改 required 列: %v", err)
	}
	e.proc.Meta.Flush()
	r = resultOf(t, e.handle(single(testTerminalID, withoutHum...)), testTerminalID)
	if !r.Accepted {
		t.Fatalf("required=false 不应豁免必填、也不应整终端拒收: %+v", r)
	}
	if reason := itemReason(wireItems(t, resultWire(t, r)), "hum"); reason != itemReasonValueMissing {
		t.Fatalf("required=false 时 hum 仍应记 value_missing，实际 %q", reason)
	}
}

// ---------- 结构性拒回（回归护栏：行为与今日一致，items 必须为空） ----------

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
	if items := wireItems(t, resultWire(t, r)); len(items) != 0 {
		t.Fatalf("结构性拒收不应带项级明细，实际 %v", items)
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
	if items := wireItems(t, resultWire(t, r)); len(items) != 0 {
		t.Fatalf("结构性拒收不应带项级明细，实际 %v", items)
	}
	digest := e.digestOf(testTerminalID, exception.ProtocolError)
	if !bytes.Contains([]byte(digest), []byte(testGatewayID)) || !bytes.Contains([]byte(digest), []byte(testGatewayAlt)) {
		t.Fatalf("异常摘要应同时保留接入网关与注册网关，实际 %q", digest)
	}
}

// 用例 8：terminalId 未注册 → 拒收，记 terminal_unknown。
func TestUnknownTerminalRecordsTerminalUnknown(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single("T-NOPE",
		it("temp", 25.0, "C", at), it("power", 1.0, "kW", at)))
	r := resultOf(t, res, "T-NOPE")
	if r.Accepted || r.Reason != validate.ReasonTerminalUnknown {
		t.Fatalf("未注册终端应记 terminal_unknown: %+v", r)
	}
	if items := wireItems(t, resultWire(t, r)); len(items) != 0 {
		t.Fatalf("结构性拒收不应带项级明细，实际 %v", items)
	}
	if n := e.eventCount("T-NOPE", exception.TerminalUnknown, e.clock.Now()); n != 1 {
		t.Fatalf("terminal_unknown 计数 = %d，期望 1", n)
	}
	// 状态型异常整套能力已随「异常区间下线」批次移除，枚举里只剩事件型，
	// 因此不再断言任何状态型计数为 0。
}

// code 为空是结构性错误（该项连标识都没有）：仍整终端拒收，不降级为项级失败。
func TestEmptyCodeRejectsTerminal(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("", 1.0, "C", at),
		it("power", 1.0, "kW", at),
	))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("code 为空应结构性命中 msg_incomplete: %+v", r)
	}
	if items := wireItems(t, resultWire(t, r)); len(items) != 0 {
		t.Fatalf("结构性拒收不应带项级明细，实际 %v", items)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("被拒终端不应写缓存，实际 %d 行", n)
	}
}

// ---------- 整包级错误（不受本次改造影响） ----------

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

// ---------- 验收标准 1–7 的专用用例 ----------

// 验收 1：一项类型不符、其余入库（并直查 latest_value 证明失败项没进去）。
func TestItemValueTypeFailureKeepsOtherItems(t *testing.T) {
	e := newEnv(t)
	e.addProp("hum", "number") // 声明为 number，报文给字符串 → value_type
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", 25.0, "C", at),
		it("power", 1.0, "kW", at),
		it("hum", "很湿", "%", at),
	))
	r := resultOf(t, res, testTerminalID)
	if !r.Accepted {
		t.Fatalf("只有一项类型不符，不应整终端拒收: %+v", r)
	}
	if r.Written != 2 {
		t.Fatalf("written = %d，期望 2（其余两项入库）: %+v", r.Written, r)
	}
	m := resultWire(t, r)
	if got := wireInt(t, m, "failedCount"); got != 1 {
		t.Fatalf("failedCount = %d，期望 1", got)
	}
	items := wireItems(t, m)
	if len(items) != 1 || items[0]["code"] != "hum" || items[0]["reason"] != itemReasonValueType {
		t.Fatalf("items = %v，期望 [{hum value_type}]", items)
	}
	// 直查库：成功的两项在、失败项不在。
	if n := e.count(`select count(*) from latest_value where terminal_id = $1`, testTerminalID); n != 2 {
		t.Fatalf("latest_value 行数 = %d，期望 2", n)
	}
	if n := e.count(`select count(*) from latest_value where terminal_id = $1 and item_code = 'hum'`,
		testTerminalID); n != 0 {
		t.Fatalf("类型不符的 hum 不应入库，实际 %d 行", n)
	}
}

// 验收 2：全部项都失败 → accepted=false、reason 为空、items 列全部失败项、written=0。
func TestAllItemsFailedKeepsAcceptedFalse(t *testing.T) {
	e := newEnv(t)
	e.addProp("hum", "number")
	at := baseTime.Add(5 * time.Minute)
	res := e.handle(single(testTerminalID,
		it("temp", "不是数字", "C", at), // value_type
		it("power", nil, "kW", at),  // value_missing
		it("hum", "也不是数字", "%", at), // value_type
	))
	r := resultOf(t, res, testTerminalID)
	if r.Accepted {
		t.Fatalf("全部项失败不应标记 accepted: %+v", r)
	}
	if r.Reason != validate.ReasonAllItemsFailed {
		t.Fatalf("项级全失败的 reason 应为 %q（不是 clean_failed/msg_incomplete），实际 %q",
			validate.ReasonAllItemsFailed, r.Reason)
	}
	if r.Written != 0 {
		t.Fatalf("written = %d，期望 0", r.Written)
	}
	m := resultWire(t, r)
	if got := wireInt(t, m, "failedCount"); got != 3 {
		t.Fatalf("failedCount = %d，期望 3", got)
	}
	items := wireItems(t, m)
	if len(items) != 3 {
		t.Fatalf("items 长度 = %d，期望 3: %v", len(items), items)
	}
	if reason := itemReason(items, "power"); reason != itemReasonValueMissing {
		t.Fatalf("power 的项级原因 = %q，期望 value_missing", reason)
	}
	if n := e.count(`select count(*) from latest_value`); n != 0 {
		t.Fatalf("全失败不应写缓存，实际 %d 行", n)
	}
}

// 验收 4：同一份报文，HTTP 与 MQTT 两个入口的逐终端结果一致。
func TestMQTTAndHTTPAgreeOnItemResults(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	body := single(testTerminalID,
		it("temp", "坏值", "C", at), // value_type
		it("power", 1.0, "kW", at),
	)

	// MQTT 入口：mqtt.go 走的就是 HandleSource("mqtt", …)。
	mqttRes, err := e.proc.HandleSource(context.Background(), "mqtt", testGatewayID, body)
	if err != nil {
		t.Fatalf("MQTT 入口处理失败: %v", err)
	}
	mqttWire := resultWire(t, resultOf(t, mqttRes, testTerminalID))

	status, resp := e.post(testGatewayID, body)
	if status != 200 {
		t.Fatalf("HTTP 入口应 200，实际 %d %v", status, resp["message"])
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("HTTP 响应缺少 data: %v", resp)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("HTTP data.results = %v，期望 1 条", data["results"])
	}
	httpWire, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("HTTP results[0] 不是对象: %v", results[0])
	}

	for _, key := range []string{"accepted", "reason", "failedCount", "items"} {
		if fmt.Sprint(mqttWire[key]) != fmt.Sprint(httpWire[key]) {
			t.Fatalf("字段 %q 两入口不一致：mqtt=%v http=%v", key, mqttWire[key], httpWire[key])
		}
	}
}

// 验收 6（上限）：30 项全失败 → failedCount 报真实总数 30，items 截断到 20。
func TestFailedItemsCappedAtTwenty(t *testing.T) {
	e := newEnv(t)
	// 声明 temp/power + 28 个新点位 = 30 个声明点位，报文把它们全部发坏。
	codes := e.addProps(28)
	at := baseTime.Add(5 * time.Minute)
	items := []item{
		it("temp", "坏", "C", at),
		it("power", "坏", "kW", at),
	}
	for _, code := range codes {
		items = append(items, it(code, "坏", "", at))
	}
	r := resultOf(t, e.handle(single(testTerminalID, items...)), testTerminalID)

	if r.Accepted || r.Written != 0 {
		t.Fatalf("全失败不应 accepted/written: %+v", r)
	}
	m := resultWire(t, r)
	if got := wireInt(t, m, "failedCount"); got != 30 {
		t.Fatalf("failedCount = %d，期望 30（真实总数不被截断）", got)
	}
	if items := wireItems(t, m); len(items) != maxItemResults {
		t.Fatalf("items 长度 = %d，期望上限 %d", len(items), maxItemResults)
	}
}

// 验收 7（兼容）：missingCodes 仍为 value_missing 的 code。
func TestMissingCodesStillReportValueMissing(t *testing.T) {
	e := newEnv(t)
	at := baseTime.Add(5 * time.Minute)
	r := resultOf(t, e.handle(single(testTerminalID, it("temp", 25.0, "C", at))), testTerminalID)

	if !r.Accepted || r.Written != 1 {
		t.Fatalf("缺 power 只应记项级失败: %+v", r)
	}
	if len(r.MissingCodes) != 1 || r.MissingCodes[0] != "power" {
		t.Fatalf("missingCodes = %v，期望 [power]", r.MissingCodes)
	}
	if reason := itemReason(wireItems(t, resultWire(t, r)), "power"); reason != itemReasonValueMissing {
		t.Fatalf("power 的项级原因 = %q，期望 value_missing", reason)
	}
}
