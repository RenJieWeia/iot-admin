# IoT 数据中心平台 · 采集与传输模块（v2 批量契约）

## 一、变更清单

### 目录与职责
| 路径 | 职责 |
| --- | --- |
| `cmd/server/` | 程序入口：迁移 → 预建分区 → 连 BroMQ（含断连/重连写异常区间）→ 订阅接入主题 → HTTP → 结算协程 |
| `internal/ingest/` | HTTP `POST /ingest/v1/messages` 与 MQTT `iot/ingest/+` 双通道 + 唯一业务管道（信封校验 → 逐终端隔离处理） |
| `internal/validate/` | v2 信封解析、逐终端结构与时间校验、物模型/终端加载、required 覆盖检查 |
| `internal/clean/` | 类型转换、四组闭集单位换算、精度、5 类质量规则 |
| `internal/latest/` | `latest_value` 批量 upsert（`value_time` 大者覆盖） |
| `internal/staging/` | 小时归属、关闭时刻、窗口判定、`hourly_staging` 批量 upsert |
| `internal/publish/` | 实时主题唯一出口（QoS 1、retain=false），`publishedAt`/`collectedAt` |
| `internal/settle/` | 窗口配置（单一实现）、每分钟结算、月度分区维护 |
| `internal/exception/` | 两张异常表 + 唯一 error_type 枚举 + 网关维度断连区间 |
| `internal/admin/` | 6 个数据源实体 CRUD、统一返回结构、鉴权占位 |
| `internal/store/` | 连接池、迁移执行、按终端分片写锁 |
| `migrations/` | 0001 数据源 / 0002 运行时 / 0003 异常 / **0004 terminal_unknown** |
| `configs/` | 默认配置样例（无凭据、无业务参数） |
| `docs/` | 本说明 + `capacity-record.md` |

### 接口
- `POST /ingest/v1/messages`（Header `X-Gateway-Id`）：整包级错误 400（`invalid_json` / `terminals_invalid` / `unsupported_version` / `gateway_id_missing` / `body_too_large`），其余一律 200 + 逐终端 `results`
- MQTT 接入 `iot/ingest/{gateway_id}`，QoS 1，与 HTTP 同一 `Processor`
- MQTT 发布 `iot/realtime/{domain_code}/{type_code}/{terminal_id}`，QoS 1，retain=false，按终端一条
- 管理 API `/admin/v1/{data-domains,device-types,thing-model-properties,terminals,quality-rules,settle-configs}`，统一 `{code,message,data}`

## 二、v2 契约要点

1. **一种格式**：`{ver, messageId, gatewayId, terminals:[{terminalId, items:[{code,value,unit,collectedAt}]}]}`，顶层不再有 `terminalId`/`items`。
2. **整包 vs 逐终端**：`ver != 1`、`terminals` 非数组/为空、缺 `gatewayId`、JSON 非法、body > 1MB 都是整包 400；其余问题只拒收该终端，`results` 里带 `reason` 与 `missingCodes`（最多 10 个）。
3. **时间**：`collectedAt` 必填，接受 `+08:00`、`Z`（换算 UTC+8）、省略偏移（按 UTC+8）、毫秒；拒收 epoch 数字、仅日期、空格分隔、空串。解析统一在 `validate.ParseCollectedAt`，`hasOffset` 只用于统计。
4. **单终端约束**：`code` 重复 → `msg_incomplete`；`items` 内 `collectedAt` 跨度 > 1 小时 → `protocol_error`；required 未全覆盖 → `msg_incomplete` + `missingCodes`。
5. **网关三方一致**：接入网关（header/topic）== 报文 `gatewayId` == 注册表 `terminal.gateway_id`，任一不一致该终端拒收，异常摘要同时记录接入网关与注册网关。
6. **终端未注册/停用** → `terminal_unknown`（事件型，本轮新增）；`auth_failed` 只留给真正的凭证/认证失败，本期无业务写入路径。
7. **单位闭集**：只有 C↔F、W↔kW、Pa↔kPa、Wh↔kWh 四组，闭集外透传原单位 + `clean_failed`，不拒收。
8. **窗口**：关闭时刻 `H + 1h + window`，闭区间（接收时刻 `>= close` 即迟到）；结算扫描镜像条件 `<= now`。
9. **结算**：窗口解析只有一个实现（`settle.WindowConfig`），SQL 侧不再内联 CTE；按窗口分组批量 INSERT ... ON CONFLICT DO NOTHING + DELETE 暂存。
10. **区间异常**：`exception_interval` 的写入机制（`OpenInterval` / `CloseInterval` / 抖动合并 / `upper_inf`）完整保留，但**本期没有任何生产写入路径**——见下条。
11. **本期区间表无生产写入路径**：网关断连需要 Broker 侧信号（BroMQ 的 client tracking 或 MQTT 遗嘱消息）才能感知；平台只是另一个 MQTT 客户端，观测不到"某个网关掉线"，把"平台到 Broker 的客户端连接断开"记为网关断连会产生 100% 假阳性 + 100% 假阴性。该能力随 Broker 集成排期，当前不得用平台连接状态冒充。
12. **平台故障与数据问题分离**：DB 等平台侧故障时逐终端标 `internal_error`，HTTP 整包 503（body 仍带完整 `results`）；数据问题仍是 200 + 逐终端 `results`。网关按状态码决定是否整批重传。
13. **质量违规来源**：`quality_violation` 摘要前缀固定 `source=thing_model`（物模型 min/max 越界）或 `source=rule`（终端质量规则命中），同一项两类都命中计 2 次。
14. **单位换算短路**：`from == target` 时直接透传、不计数——即使该单位不在换算闭集内。否则电表场景里 A / V / kvar 等只要物模型声明与上报一致就会每分钟刷出大量假 `clean_failed`。

## 三、本轮必修项落地（对应任务书第二章）

| 必修 | 落点 | 做法 |
| --- | --- | --- |
| 1 单终端契约 | `validate/message.go`、`ingest/pipeline.go` | 改为 `Envelope` + `Terminals[]`，全链路只认批量格式 |
| 2 时间字段与时区 | `validate/message.go:ParseCollectedAt`、`publish/publish.go` | `collectedAt`/`publishedAt`；省略偏移按 +08:00；统一转 Asia/Shanghai |
| 3 静默去重 | `validate/message.go` | 删除 dedupe，重复 code → 该终端 `msg_incomplete` |
| 4 单终端跨度 | `validate/message.go:ParseTerminal` | 跨度 > 1h → `protocol_error` |
| 5 网关未与注册表校验 | `ingest/pipeline.go` | 三方一致校验 + 摘要记录两个网关 |
| 6 HTTP 静默截断 | `ingest/http.go` | `http.MaxBytesReader`，超限 400 `body_too_large` |
| 7 非目标检查依赖 DB | `nongoal_test.go` + 三包纯逻辑单测 | 非目标用例零依赖（`admin.New(nil)`）；validate/clean/staging 补纯逻辑单测 |
| 8 未注册用 auth_failed | `exception/errors.go`、`migrations/0004`、`pipeline.go` | 新增 `terminal_unknown`，业务校验不再写 `auth_failed` |
| 9 单位表超出闭集 | `clean/units.go` | 收敛为四组 8 个符号 |
| 10 断连无写入路径 | `exception/gateway.go`、`cmd/server/main.go` | 断连开区间、重连关闭，按网关维度 |

## 三之二、rv-03 审核意见处置（T1-T7）

| 项 | 结论 | 落点 |
| --- | --- | --- |
| T1 摘除断连写入路径 | `gateway.go` 整体删除，`main.go` 两个回调只打日志；区间表、`OpenInterval`/`CloseInterval`、抖动合并保留 | `internal/exception`、`cmd/server` |
| T2 平台故障与数据问题区分 | 逐终端 `internal_error` + HTTP 503（body 带 results），其余 200 | `internal/ingest/http.go`、`pipeline.go` |
| T3 删除死代码 | 删除 `RawItem.HasOffset` 与 `ParseCollectedAt` 第二返回值 | `internal/validate/message.go` |
| T4 Deviation 更正 | 代码 `from == target` 短路放行是对的，文档写错；决策表新增"同单位闭集外直接透传不计数" | 本文第二十章 14 条 |
| T5 网关一致性提信封层 | `ingress != env.GatewayID` 在 `Handle` 判定一次，逐终端产出同 reason | `internal/ingest/pipeline.go` |
| T6 质量违规来源 | 摘要加 `source=thing_model|rule` | `internal/ingest/pipeline.go` |
| T7 错误体命名 | `bodyTooLargeError` → `envelopeErrorBody` | `internal/ingest/http.go` |

## 四、测试（31 项 + 纯逻辑单测）

| 需求用例 | 测试 |
| --- | --- |
| 1 批量 3 终端隔离 | `TestBatchIsolatesFailingTerminal` |
| 2 value 为 null | `TestNullValueRejectsTerminal` |
| 3 数字字符串 | `TestNumericStringValueConverted` |
| 4 code 重复 | `TestDuplicateCodeRejectsTerminal` |
| 5 未知 code | `TestUnknownCodeIgnoredAndCounted` |
| 6 跨度 70 分钟 | `TestSpanOverOneHourRejectsTerminal` |
| 7 网关与注册表不符 | `TestGatewayMismatchWithRegistry` |
| 8 未注册终端 | `TestUnknownTerminalRecordsTerminalUnknown` |
| 9 ver=2 | `TestUnsupportedVersionRejectedWholeBatch` |
| 10 body > 1MB | `TestBodyTooLargeRejected`（+ `TestTerminalsNotArrayRejected`） |
| 11 省略偏移 | `TestCollectedAtWithoutOffsetDefaultsToShanghai` |
| 12 Z 偏移换算 | `TestCollectedAtWithZConvertsToShanghaiHour` |
| 13 空格分隔 | `TestCollectedAtWithSpaceSeparatorRejected` |
| 14 超前 6 分钟 | `TestFutureCollectedAtRejected` |
| 15 时间倒退 | `TestCollectedAtRegressionDoesNotOverwrite` |
| 16 乱序取最大 | `TestOutOfOrderMessagesKeepMaxCollectedAt` |
| 17 单条消息跨小时 | `TestItemsInSameMessageBelongToDifferentHours` |
| 18 窗口边界 | `TestWindowBoundaryIsClosedAtCloseTime` |
| 19 迟到 | `TestLateDataOutsideWindow` |
| 20 无数据小时 | `TestHourWithoutDataProducesNoSnapshotRow` |
| 21 重复结算 | `TestRepeatedSettleProducesNoSecondRow` |
| 22 分区切换/月初边界 | `TestMonthlyPartitionsAndMonthBoundaryRows` |
| 23 并发开区间 | `TestConcurrentIntervalOpenStaysSingle` |
| 24 永不恢复 | `TestOpenIntervalStaysOpenWithoutBackgroundJob` |
| 25 抖动合并 | `TestIntervalJitterMerge` |
| 26 3000 次聚合 | `TestEventAggregationSingleRow` |
| 27 断连/重连/多网关 | `TestGatewayDisconnectIntervalLifecycle` |
| 28 质量规则 | `TestQualityRuleViolationStillPersisted` |
| 29 单位闭集 | `TestUnitConversionClosedSet` |
| 30 两路一致 | `TestHTTPAndMQTTPathsProduceIdenticalResults` |
| 31 非目标（零依赖） | `TestNoOutOfScopeArtifacts` |
| 纯逻辑 | `validate/message_test.go`、`clean/clean_test.go`、`staging/staging_test.go`（无 IOT_TEST_DSN 也执行） |
| rv-03 新增 | `TestPlatformFailureReturns503`（平台故障 503）、`TestNoIntervalAnomaliesInNormalFlow`（正常流程区间表零行）；删除 `TestGatewayDisconnectIntervalLifecycle` |

运行方式：

```bash
go test ./...                                                            # 纯逻辑用例真实执行，DB/Broker 用例 skip
IOT_TEST_DSN=... MQTT_BROKER_URL=tcp://127.0.0.1:1883 go test ./... -v   # 全量
IOT_TEST_DSN=... IOT_CAPACITY=1 go test ./internal/ingest -run TestCapacity -v
```

## 五、风险

1. **保留期容量**：24 个月 ≈ 52 亿行 / 753 GB（145 B/行含索引），需 ≥1.5 TB 可用空间（含 WAL、膨胀、备份）；必须依赖按月 DROP 控制总量。
2. **整条拒收**：任一数据项转换失败即整条终端拒收，单探头故障会导致该终端该小时全部数据项缺失（无 carry-forward，无补录）。
3. **省略偏移**：网关误按 UTC 打时间戳又不带偏移，会被静默解释为 +08:00 并落到错误小时，只能靠上游时钟治理。
4. **管理 API 无鉴权**：`AllowAll` 直接放行、仅 `127.0.0.1` 监听，属已知并接受的遗留风险。
5. **上传超时异常未实现**：`upload_timeout` 只有枚举没有写入路径，需随元数据治理（期望上报周期）一起排期。
6. **单实例**：写锁是进程内的，多实例会破坏 `value_time` 串行化。
7. **异常表无清理策略**：`exception_count` 长期累积，需按保留期归档。
8. **元数据每批 4 次查询**：未加进程内缓存，元数据读成为吞吐上限时再优化（代码内有 `shortcut:` 注释）。
9. **本期不产出区间型异常**：`gateway_disconnected` / `item_silent` / `auth_failed` 三类只有枚举与写入机制，没有任何生产写入路径；网关掉线对平台不可观测，需要 Broker 侧集成。
10. **503 会触发整批重传**：平台故障恢复后网关会重发整批，可能重复发布实时消息与重复计数（已明确允许，不做幂等）。

## 六、未完成项与建议

1. 元数据（终端/物模型/规则/窗口配置）进程内缓存 + 管理 API 写时失效。
2. 上传超时异常需要"期望上报周期"元数据与扫描任务。
3. 异常表保留期/分区化；`exception_count` 归档。
4. 管理 API 权限、审计与操作日志（本期占位）。
5. 大小时结算改 COPY/分片提交。
6. 端到端容量：补 `fsync=on` 真实磁盘 + BroMQ 发布链路的联合压测。
