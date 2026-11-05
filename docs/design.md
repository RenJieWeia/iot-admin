# IoT 数据中心平台 · 采集与传输模块（首版）设计说明

## 一、变更清单

### 目录与职责
| 路径 | 职责 |
| --- | --- |
| `cmd/server/` | 程序入口：连 PG → 执行迁移 → 预建分区 → 连 BroMQ → 订阅接入主题 → 起 HTTP（接入 + 管理）→ 起结算扫描协程 |
| `internal/ingest/` | HTTP `POST /ingest/v1/messages`、MQTT `iot/ingest/+` 两路接入 + 唯一业务管道（校验→清洗→缓存/暂存→发布） |
| `internal/validate/` | 报文结构校验、物模型/终端加载、required 齐备校验、超前时判断 |
| `internal/clean/` | 类型转换、单位换算（量纲表）、精度取整、5 类质量规则 |
| `internal/latest/` | `latest_value` 批量 upsert（`value_time` 大者覆盖） |
| `internal/staging/` | 小时归属计算、窗口关闭时刻、`hourly_staging` 批量 upsert |
| `internal/publish/` | 实时主题唯一出口（QoS 1、retain=false），主题与报文组装 |
| `internal/settle/` | 每分钟结算扫描、快照幂等落库、月度分区预建与过期 DROP |
| `internal/exception/` | 两张异常表写入 + **唯一 error_type 枚举定义** + 样本摘要 |
| `internal/admin/` | 6 个数据源实体的 CRUD、统一返回结构、鉴权中间件占位 |
| `internal/store/` | 连接池、迁移执行、按终端分片的写锁、共享行结构 |
| `migrations/` | `0001_datasource` / `0002_runtime` / `0003_exception`（含 `btree_gist`） |
| `configs/server.env.example` | 默认配置样例（只有连接信息，无凭据、无业务参数） |
| `docs/` | 本设计说明 + `capacity-record.md` 容量实测 |

### 表（12 张，全部在 migrations 中）
数据源：`data_domain`、`device_type`、`thing_model_property`、`terminal`、`quality_rule`、`settle_config`
运行时：`latest_value`、`hourly_staging`、`hourly_snapshot`（`PARTITION BY RANGE (hour_start)`，按月分区，主键 `(terminal_id, item_code, hour_start)`，无自增 id）
异常：`exception_interval`（状态型，`EXCLUDE USING gist`）、`exception_count`（事件型，PK `(terminal_id, hour_start, error_type)`）
迁移记录：`schema_migrations`

### 接口
- `POST /ingest/v1/messages`，Header `X-Gateway-Id`；返回 `{code, message, data}`，`data` 为 `{accepted, reason, written, published}`
- MQTT 接入：`iot/ingest/{gateway_id}`，QoS 1
- MQTT 发布：`iot/realtime/{domain_code}/{type_code}/{terminal_id}`，QoS 1，retain=false
- 管理 API（`/admin/v1/`，默认只监听 `127.0.0.1`，`WithAuth(AllowAll)` 占位）：
  `data-domains`、`device-types`、`thing-model-properties`、`terminals`、`quality-rules`、`settle-configs`
  各支持 `GET 列表 / POST 新建 / GET 单个 / PUT 局部更新 / DELETE`

## 二、关键语义（评审重点）

1. **小时与窗口**：`hour_start = truncate(item.time, hour)`（Asia/Shanghai，固定 +08:00，不依赖 tzdata）。
   关闭时刻 `close(H) = H + 1h + window_seconds`。**闭区间语义：平台接收时刻 `>= close(H)` 即窗口外（迟到）；`< close(H)` 在窗口内。** 结算扫描用同一比较的镜像：`H + 1h + window <= now` 即可结算。因此"恰好等于关闭时刻"这一瞬间既判迟到、又可结算，判定唯一且可测（用例 4）。
2. **同小时覆盖**：`hourly_staging` 与 `latest_value` 都以 `value_time` 比较，SQL 层 `ON CONFLICT ... WHERE excluded.value_time > 既有值`，到达顺序不影响结果（用例 2/9）。
3. **并发**：单实例，但 MQTT 回调与 HTTP handler 并发；写入按 `terminal_id` 哈希分片加锁（`store.ShardedLock`，64 分片），同一终端严格串行，不同终端并行。
4. **结算幂等不建状态表**：`INSERT ... ON CONFLICT DO NOTHING` + 删除已结算小时的暂存，重复扫描不产生第二行；任一步失败记一条 `snapshot_failed` 并下轮重试。
5. **冻结**：快照写入后不再 UPDATE；迟到数据不修正历史。
6. **异常两表分工**：状态型（`gateway_disconnected`/`auth_failed`/`item_silent`）进 `exception_interval`，事件型（9 种）进 `exception_count`；枚举集中定义在 `internal/exception/errors.go`，SQL 侧只有 check 约束镜像。
7. **抖动合并**：开启新区间时，若上一条已结束区间距本次开启 < 5 分钟则并入上一条（`upper` 改回 `+inf`，`raw_count + 1`）。新区间此刻时长为 0，恒 < 5 分钟，故只需判间隔（用例 13：3 次 2 分钟断续 → 1 条 / `raw_count=3`）。
8. **未结束区间** = `upper_inf(period)`；本期不设自动关闭任务（用例 12）。`CloseInterval` 已提供，本期没有恢复信号来源，供契约测试与后续接入使用。

## 三、实现决策与规格偏差（需要评审确认）

| # | 决策 | 原因 |
| --- | --- | --- |
| 1 | 窗口从**小时结束**起算（`H + 1h + window`），不是 `H + window` | 流程第 5 步明确"小时结束 + 结算窗口未过 → 视为窗口内"；若按 `H + window` 且 window=15 min，会在小时内就冻结快照，丢整点后 45 分钟数据且不可修正 |
| 2 | `exception_interval` 的 EXCLUDE 用 `coalesce(item_code,'')` 参与比较 | PG 中 NULL 永不相等，直接用 `item_code` 会让 `item_silent`（item_code 为 NULL）绕过约束出现双开，与用例 11 冲突 |
| 3 | 物模型 `min_value/max_value` 越界 → `quality_violation`，不拒收 | 拒收清单只有"转换失败/NaN/Infinity"；边界越界属于质量问题 |
| 4 | 物模型未声明的 item code → 忽略 + `clean_failed` 计数（不拒收） | 无法无模型推断类型；静默丢弃不可接受，故留计数痕迹 |
| 5 | HTTP 网关标识头与报文 `gatewayId` 不一致 → `protocol_error` 拒收 | 两路同源要求网关标识可信；不希望静默采用其中一个 |
| 6 | 迟到按**数据项**剔除，全部项都迟到才整体判 `late_data` | 一条消息内各 `item.time` 可以跨小时；逐项剔除不会因为个别项迟到丢弃同批新鲜数据 |
| 7 | 时间倒退：不写 latest/staging、记 `protocol_error`，消息本身仍 `accepted`（`written=0`） | 与"不覆盖 + 记异常"一致，不是格式错误 |
| 8 | `precision` 规则参数为 `{"scale": N}`；物模型 `precision` 只用于入库取整 | 两者语义不同：前者是质量判定，后者是存储精度 |
| 9 | 元数据每条消息 4 次查询，未做进程内缓存 | 首版优先正确性；容量实测已给出余量，缓存列为后续优化（代码内有 `shortcut:` 注释标注） |
| 10 | 实时报文 `ts` = 平台发布时刻 | 与 `items[].time`（采集时刻）区分 |

## 四、测试

15 个用例 + 2 个附加用例（管理 API 冒烟、两路一致性）全部实现于 `internal/ingest/*_test.go`，真实 PostgreSQL + 真实 BroMQ：

| 用例 | 测试函数 |
| --- | --- |
| 1 required 缺失拒收 | `TestRequiredPropertyMissingRejected` |
| 2 乱序取最大 value_time | `TestOutOfOrderMessagesKeepMaxValueTime` |
| 3 迟到只记 late_data | `TestLateDataOutsideWindow` |
| 4 窗口边界（闭区间） | `TestWindowBoundaryIsClosedAtCloseTime` |
| 5 无数据小时无快照行 | `TestHourWithoutDataProducesNoSnapshotRow` |
| 6 重复结算不产生第二行 | `TestRepeatedSettleProducesNoSecondRow` |
| 7 质量规则命中照常入库 | `TestQualityRuleViolationStillPersisted` |
| 8 超前 6 分钟拒收 | `TestFutureTimestampRejected` |
| 9 时间倒退不覆盖 | `TestTimeRegressionDoesNotOverwrite` |
| 10 跨月暂存 + 分区切换 | `TestMonthlyPartitionsAndMonthBoundaryStaging` |
| 11 并发开区间被 EXCLUDE 拦截 | `TestConcurrentIntervalOpenStaysSingle` |
| 12 永不恢复 upper_inf = true | `TestOpenIntervalStaysOpenWithoutBackgroundJob` |
| 13 抖动合并 raw_count=3 | `TestIntervalJitterMerge` |
| 14 3000 次失败聚合成 1 行 | `TestEventAggregationSingleRow` |
| 15 非目标项不存在 | `TestNoOutOfScopeArtifacts` |
| 附加 | `TestAdminAPICRUD`、`TestHTTPAndMQTTPathsProduceIdenticalResults`、`TestCapacityBaseline10kMessages`（`IOT_CAPACITY=1`） |

运行方式（本机已起本地 PG 16.4 与 BroMQ）：

```bash
go test ./...                                                            # 仅 PG 依赖用例（跳过 MQTT/容量）
IOT_TEST_DSN=... MQTT_BROKER_URL=tcp://127.0.0.1:1883 go test ./... -v   # 含两路一致性
IOT_TEST_DSN=... IOT_CAPACITY=1 go test ./internal/ingest -run TestCapacity -v
```

## 五、风险

1. **保留期容量**：24 个月 × 2 万台 × 15 项 ≈ 52 亿行 / 756~766 GB（实测 145.8 B/行，含索引）。本机 445 GB 可用磁盘装不下，生产需 ≥ 1.5 TB（含 WAL、膨胀、备份余量），并且必须依赖按月 DROP 才能控制总量；若终端/数据项规模上浮，先扩存储再扩容终端。
2. **参数仍是假设**：单终端 15 个数据项、60 秒上报、2 万台上限为待确认假设；数据项数直接线性放大行数与体积，上报频率放大写入 QPS。当前实现把这两者写死在测试语料里，业务侧没有参数化（规格禁止新增配置项），需要以"物模型属性数 × 上报频率"作为容量评审输入。
3. **管理 API 无鉴权**：`WithAuth(AllowAll)` 直接放行，默认只监听 `127.0.0.1`，仅内网可达；任何能访问该端口的人都能改数据源与质量规则。本期决定延后，接入前必须补权限与审计。
4. **无幂等去重（按规格）**：重复消息会重复发布实时主题、重复累加异常计数；Broker 或网关重投时订阅方需自行容忍重复。
5. **单实例**：写锁是进程内的，多实例水平扩展会破坏 `value_time` 串行化语义，需换成 PG 行锁或单写队列。
6. **结算扫描与写入争用**：结算事务会锁已关闭小时的暂存行；当前规模无实测冲突，但高峰小时（30 万行）结算约 6 s 量级，若与下一个小时写入重叠需观察锁等待。
7. **异常表无清理策略**（规格未要求）：`exception_count` 长期累积，需后续按保留期归档。

## 六、未完成项与建议

1. 元数据（终端/物模型/质量规则/结算窗口）加进程内缓存 + 管理 API 写时失效，预计写入吞吐再提升 2~3 倍。
2. 快照批量写入改为 `COPY`/分片提交，降低大小时结算的事务时长。
3. `item_silent` 的静默检测需要"期望上报周期"元数据与扫描任务（本期明确不设自动关闭任务）。
4. 异常表保留期与归档策略、`exception_count` 分区化。
5. 管理 API 的权限、审计与操作日志（本期占位）。
6. 容量实测补真实磁盘（`fsync=on`）与 BroMQ 发布链路一起压测的端到端数字。
