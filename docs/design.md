# IoT 数据中心平台 · 采集与传输模块（v2 批量契约）

## 一、变更清单

### 目录与职责
| 路径 | 职责 |
| --- | --- |
| `cmd/server/` | 程序入口：迁移 → 预建分区 → 连 BroMQ（含断连/重连写异常区间）→ 订阅接入主题 → HTTP → 结算协程 |
| `internal/ingest/` | HTTP `POST /ingest/v1/messages` 与 MQTT `iot/ingest/+` 双通道 + 唯一业务管道（信封校验 → 逐终端隔离处理） |
| `internal/validate/` | v2 信封解析、逐终端结构与时间校验、物模型/终端加载、声明字段全覆盖检查 |
| `internal/clean/` | 类型转换、四组闭集单位换算、精度、5 类质量规则 |
| `internal/latest/` | `latest_value` 批量 upsert（`value_time` 大者覆盖） |
| `internal/staging/` | 小时归属、关闭时刻、窗口判定、`hourly_staging` 批量 upsert |
| `internal/publish/` | 实时主题唯一出口（QoS 1、retain=false），`publishedAt`/`collectedAt` |
| `internal/settle/` | 窗口配置（单一实现）、每分钟结算、月度分区维护 |
| `internal/exception/` | 两张异常表 + 唯一 error_type 枚举 + 网关维度断连区间 |
| `internal/meta/` | 四类元数据（终端/物模型/质量规则/结算窗口）进程内缓存 + 写时失效 |
| `internal/ui/` | 管理后台 SPA 静态服务：`//go:embed all:dist` + `/ui/*` 的 `index.html` fallback（无模板、无 SSR） |
| `web/` | 前端源码（Vite 6 + TS + React 19 + React Router 7 + TanStack Query/Table + Radix），产物输出到 `internal/ui/dist` |
| `internal/admin/` | 6 个数据源实体 CRUD、统一返回结构、鉴权占位、字段元数据与 QueryState 查询、`_meta`/异常只读/`options` 三个 JSON 端点 |
| `internal/store/` | 连接池、迁移执行、按终端分片写锁 |
| `migrations/` | 0001 数据源 / 0002 运行时 / 0003 异常 / **0004 terminal_unknown** |
| `configs/` | 默认配置样例（无凭据、无业务参数） |
| `docs/` | 本说明 + `capacity-record.md` |

### 接口
- `POST /ingest/v1/messages`（Header `X-Gateway-Id`）：整包级错误 400（`invalid_json` / `terminals_invalid` / `unsupported_version` / `gateway_id_missing` / `body_too_large`），其余一律 200 + 逐终端 `results`
- MQTT 接入 `iot/ingest/{gateway_id}`，QoS 1，与 HTTP 同一 `Processor`
- MQTT 发布 `iot/realtime/{domain_code}/{type_code}/{terminal_id}`，QoS 1，retain=false，按终端一条
- 管理 API `/admin/v1/{data-domains,device-types,thing-model-properties,terminals,quality-rules,settle-configs}`，统一 `{code,message,data}`；列表支持 `q/sort/order/page/pageSize/filter.<col>[.<op>]` 并返回 `meta{total,page,pageSize,sort,order}`（`limit/offset` 兼容）
- 管理后台 UI `/ui/*`：React SPA，命中产物文件直接返回，其余路径回退 `index.html`（深链刷新可用）；`/` 302 → `/ui/`
- 管理后台契约 `GET /admin/v1/_meta`（6 实体 + 2 异常页字段元数据、枚举/布尔字典、分页档位）、`GET /admin/v1/exceptions/{events,intervals}`（只读列表）、`GET /admin/v1/options/{entity}`（关联下拉选项，上限 1000）
- 所有响应带 `Content-Security-Policy: default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'`

## 二、v2 契约要点

1. **一种格式**：`{ver, messageId, gatewayId, terminals:[{terminalId, items:[{code,value,unit,collectedAt}]}]}`，顶层不再有 `terminalId`/`items`。
2. **整包 vs 逐终端**：`ver != 1`、`terminals` 非数组/为空、缺 `gatewayId`、JSON 非法、body > 1MB 都是整包 400；其余问题只拒收该终端，`results` 里带 `reason` 与 `missingCodes`（最多 10 个）。
3. **时间**：`collectedAt` 必填，接受 `+08:00`、`Z`（换算 UTC+8）、省略偏移（按 UTC+8）、毫秒，以及**纯数字时间戳**（R-029）。数字按**范围**判定而不是位数：`1_577_836_800 ≤ v ≤ 4_102_444_800` 视为秒、`1_577_836_800_000 ≤ v ≤ 4_102_444_800_000` 视为毫秒（即 2020-01-01T00:00:00Z ~ 2100-01-01T00:00:00Z 的闭区间）；低于下界报「数值早于可解释范围」，高于上界或落在两区间空档报「数值超出可解释范围」。仍拒收仅日期、空格分隔、空串。epoch 数值按 UTC 解释后统一 `In(staging.Zone)`；解析统一在 `validate.ParseCollectedAt`，`hasOffset` 只用于统计。
4. **单终端约束**：`code` 重复 → `msg_incomplete`；`items` 内 `collectedAt` 跨度 > 1 小时 → `protocol_error`；**物模型声明的全部 `item_code` 必须全覆盖**（R-021：缺任一即 `msg_incomplete` + `missingCodes`；`thing_model_property.required` 列保留但不再参与校验，仅作前端提示）；未声明的 `item_code` 仍忽略并计 `clean_failed`。
5. **网关三方一致**：接入网关（header/topic）== 报文 `gatewayId` == 注册表 `terminal.gateway_id`，任一不一致该终端拒收，异常摘要同时记录接入网关与注册网关。
6. **终端未注册/停用** → `terminal_unknown`（事件型，本轮新增）；`auth_failed` 只留给真正的凭证/认证失败，本期无业务写入路径。
7. **单位闭集**：只有 C↔F、W↔kW、Pa↔kPa、Wh↔kWh 四组，闭集外透传原单位 + `clean_failed`，不拒收。
8. **窗口**：关闭时刻 `H + 1h + window`，闭区间（接收时刻 `>= close` 即窗口外）；判定只走 `staging.InWindow` / `staging.CloseAt` 一处口径，结算扫描镜像条件 `<= now`。
9. **结算**：窗口解析只有一个实现（`settle.WindowConfig`）；快照写入只有一个实现（`settle.UpsertSnapshot`，结算与窗口外分流共用），结算按窗口分组"读待结算暂存 → UPSERT 快照 → 删暂存"。
15. **两段写入模型（R-030）**：`冻结`概念作废，任何时间到达的数据都落库，按归属小时是否已关闭分两段——窗口内写 `latest_value` + `hourly_staging` 等结算；窗口外 `latest_value` 照常写，并直接 UPSERT `hourly_snapshot`（不发布实时主题、仍记 `late_data`，detail 带 `source=mqtt|api` 与 `msgType`）。窗口内路径行为与两段模型之前完全一致。
16. **快照版本化（R-030）**：`hourly_snapshot` 带 `revision int not null default 0` 与 `settled_at timestamptz not null default now()`。同一格只有 `value_time` **更大**时才覆盖，且 `revision = revision + 1`、`settled_at = now()` 在 SQL 内原子完成；相等/更早的写入整行跳过（幂等）。历史改写通过 `GET /admin/v1/snapshot/changes?since=&limit=&cursor=` 查询：返回 `settled_at > since` 的行，按 `(settled_at, terminal_id, item_code, hour_start)` 升序游标分页（`limit` 默认 200、上限 1000），`revision > 0` 即被修正过的格子。
10. **区间异常**：`exception_interval` 的写入机制（`OpenInterval` / `CloseInterval` / 抖动合并 / `upper_inf`）完整保留，但**本期没有任何生产写入路径**——见下条。
11. **本期区间表无生产写入路径**（能力具备、平台侧未实现）：BroMQ 侧已具备网关离线观测能力——`tracking` hook 维护 `mqtt_clients` 连接状态（`/api/mqtt/clients` 与 `/metrics` 可读），且支持 MQTT 遗嘱（LWT，仅当网关自己在 CONNECT 里声明 `WillTopic` 时才可用，正常 DISCONNECT 不触发）。**平台侧写入路径仍未实现**：`gateway_disconnected` 只能由 Broker 侧信号判定，把"平台到 Broker 的客户端连接断开"记为网关断连仍会产生 100% 假阳性 + 100% 假阴性，因此当前不得用平台连接状态冒充。写入路径随网关离线观测任务排期（证据：`docs/review/RESEARCH-002.md` §5/§6）。
12. **平台故障与数据问题分离**：DB 等平台侧故障时逐终端标 `internal_error`，HTTP 整包 503（body 仍带完整 `results`）；数据问题仍是 200 + 逐终端 `results`。网关按状态码决定是否整批重传。写路径失败（`latest.Upsert`/`staging.Upsert` 事务失败）同属平台侧故障：异常仍记 `cache_write_failed` 保可观测，但逐终端返回 `internal_error`、整包 503。
13. **质量违规来源**：`quality_violation` 摘要前缀固定 `source=thing_model`（物模型 min/max 越界）或 `source=rule`（终端质量规则命中），同一项两类都命中计 2 次。
14. **行级质量标记（R-022）**：`latest_value` / `hourly_staging` / `hourly_snapshot` 各带 `quality smallint not null default 0` 与 `quality_reason text`。`quality` 是位掩码、可叠加：`1` 物模型 min/max 越界、`2` 终端质量规则命中、`4` 单位不在换算闭集内；`0` 正常，高位留给后续扩展。`quality_reason` 存可读摘要（如 `source=thing_model item=power 低于下限`，多来源用 `; ` 连接）。**原值照常入库**（`value_num`/`value_text`/`unit` 不因质量异常而改变），`exception_count.quality_violation` 的计数口径不变；质量标记跟随"胜出"的行（`value_time` 更大者覆盖，含标记本身），结算时从 staging 原样带进 snapshot。
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

## 三之三、元数据进程内缓存（rv-04）

| 项 | 口径 |
| --- | --- |
| 落点 | `internal/meta.Store` 包装 4 个 loader（`validate.Registry` ×2、`clean.LoadRules`、`settle.LoadWindowConfig`），loader 本身不动 |
| 命中 | 同一 key 连续读取不再访问 PG：首条消息 4 次元数据查询，之后 0 次 |
| 失效 | 管理 API `create/update/delete` 成功后全量 `Flush()`；`list/get` 不失效 |
| 负结果 | `ErrTerminalUnknown`、停用终端、任何 DB 错误一律不写缓存，下次继续穿透 |
| TTL | `30 * time.Second` 常量兜底，防绕过管理 API 直接改库（U-001 假设成立时最多脏 30s） |
| 并发 | 不做 singleflight，并发 miss 允许各自多查一次；`-race` 干净 |
| 前提 | 与 `ShardedLock` 同口径的单实例部署；多实例一致性不在本期范围（风险 6） |
| 不缓存 | 写入路径（`latest_value` / `hourly_staging` / `exception_*` / `hourly_snapshot`） |

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
8. **元数据缓存的一致性代价**：四类元数据已进程内缓存（TTL 30s + 管理 API 写时失效，见 §三之三）。代价有三：单实例前提，多实例各自持有副本；绕过管理 API 直接改库最长脏 30s；缓存命中期间 DB 不可用不再整包 503，故障推迟到写路径（`cache_write_failed` + 200）。
9. **本期不产出区间型异常**：`gateway_disconnected` / `item_silent` / `auth_failed` 三类只有枚举与写入机制，没有任何生产写入路径。网关离线**在 Broker 侧可观测**：BroMQ 具备 `mqtt_clients` 客户端跟踪与 MQTT 遗嘱能力（`RESEARCH-002` §5/§6），缺的是平台侧写入路径，随网关离线观测任务排期。
10. **快照 UPSERT 带来的 UPDATE 膨胀（R-030）**：被修正的格子会产生新版本行版本（revision++），依赖 autovacuum 回收；历史回填集中写入时会放大 WAL 与表膨胀，保留期仍靠按月 DROP 分区兜底。
11. **窗口外写入依赖分区已存在**：补传跨度超出已建月度分区时写入失败（按设计不自动建分区，由 `EnsurePartitions` 负责），表现为整包 503 而非静默丢数据。
12. **503 会触发整批重传**：平台故障恢复后网关会重发整批，可能重复发布实时消息与重复计数（已明确允许，不做幂等）。读元数据失败与写路径失败（`cache_write_failed`）都归入这一条：重传由 `value_time` 相等不覆盖兜底，重复发布属已接受行为。

## 六、未完成项与建议

1. ~~元数据（终端/物模型/规则/窗口配置）进程内缓存 + 管理 API 写时失效~~ ✅ 已完成（rv-04，见 §三之三）。
2. 上传超时异常需要"期望上报周期"元数据与扫描任务。
3. 异常表保留期/分区化；`exception_count` 归档。
4. 管理 API 权限、审计与操作日志（本期占位）。
5. 大小时结算改 COPY/分片提交。
6. 端到端容量：补 `fsync=on` 真实磁盘 + BroMQ 发布链路的联合压测。

## 七、管理后台 SPA（rv-07）

| 项 | 口径 |
| --- | --- |
| 交付形态 | React SPA：Vite 6 构建产物（`internal/ui/dist`）入仓 + `go:embed`，生产构建不依赖 node；单二进制、无 CDN（R-015/R-016） |
| 技术栈 | React 19 + TypeScript + React Router 7（声明式路由，basename `/ui`）+ TanStack Query v5 + TanStack Table v8 + Radix Dialog + CSS 变量令牌；不引 UI 组件库/Tailwind/状态库/i18n/图表库 |
| 目录 | `web/src/{api,components,pages,lib,styles,test}`：`api/` 五个端点封装（AbortSignal + 统一错误）、`components/` 外壳/表格/筛选/分页/抽屉/四态、`pages/` 实体列表与异常只读、`lib/` QueryState↔URL、字段派生、格式化 |
| 页面范围 | 6 个数据源实体（列表 + 侧滑抽屉增删改）+ 异常事件/异常区间只读页（R-009）；运行态看板不做 |
| 查询 | 关键词/筛选/排序/分页全部服务端执行，复用 `admin.QueryState`；`q/filters/sort/order/page/pageSize` 全量进 URL，刷新/后退/抽屉关闭后筛选、排序、分页与滚动位置还原（`sessionStorage` 按地址记忆） |
| 字段元数据 | 唯一事实源是 `internal/admin/fields.go`，经 `GET /admin/v1/_meta` 下发：列、中文标签、枚举/布尔文案与色调、操作符中文、`widthStrategy`、`ref` 关联目标、默认排序、分页档位；页面不硬编码列清单或业务枚举 |
| 筛选区 | 常驻筛选 ≤5，其余进「更多筛选」；已生效条件始终以标签外显，可单删/全清；关键词 300ms 防抖，回车立即生效 |
| 排序/分页 | 排序白名单 + 服务端默认 `defaultSort` + 唯一 tie-breaker；每页档位由 `pagination.options` 下发（默认 10、上限 50）；条件或 pageSize 变化回第一页；超范围返回空页并提示修正 |
| 四态 | 加载（骨架行与真实行高一致，无脉冲动画）/ 无数据（说明 + 新建入口）/ 无匹配（说明 + 清除筛选）/ 失败（原因 + 重试），文案互不相同 |
| 抽屉 | Radix Dialog：标题、底部固定「取消/保存」、Esc 关闭、关闭后焦点回到触发按钮（按 `data-action`/`data-row-id` 重新取节点，列表重渲染换掉旧节点也不丢）；删除走二次确认，确认按钮含对象名 |
| 令牌 | 颜色/间距/字号/圆角/行高集中在 `web/src/styles/tokens.css` 的 `:root`；`stylelint` 断言 `:root` 之外无颜色字面量、不用 `!important`，Vitest 断言间距 4px 网格 |
| 安全 | 前端无内联脚本/内联样式，后端统一下发 CSP `default-src 'self'`；React 默认转义渲染服务端字符串 |
| 非目标 | 批量选择/导出/列偏好持久化/保存视图/SSE/图表/i18n/深色主题/登录页/移动端专项（R-013、R-018） |
