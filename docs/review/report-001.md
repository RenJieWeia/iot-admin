# report-001 · 元数据进程内缓存与写时失效

- `task:` task-001（v1，用户 2026-11-05 确认 DRAFT → 生效）
- `baseline:` rv-03（0270ec3）
- `tag:` **rv-04**
- `commits:` `3a1d54e` `95982d4` `323c06a` `9dd2092` `e812d5e`（+ 本 report 的 docs 提交）
- `testing:` 无 DSN 全绿；`IOT_TEST_DSN` 全绿（实测库：postgres:16-alpine，容器 `iot-pg`）

## 1. 变更说明

| 单元 | 文件 | 职责 |
| --- | --- | --- |
| 缓存主体（新增包） | `internal/meta/store.go` | `Loader` 接口 + `Store`（四类载荷各一份 cache，`RWMutex` 保护）+ `NewStore` + `Flush`；`PGLoader` 是默认实现（终端/物模型走 `validate.Registry`，规则走 `clean.LoadRules`，窗口走 `settle.LoadWindowConfig`） |
| 缓存单测 | `internal/meta/store_test.go` | 假 loader + 假时钟，7 个用例覆盖命中/Flush/错误不缓存/未知终端/TTL/多载荷/并发 |
| 管道改读缓存 | `internal/ingest/pipeline.go` | `Processor.Reg *validate.Registry` → `Meta *meta.Store`；4 处元数据读取（`pipeline.go:81/106/130/142` 改前）统一走 Store |
| 装配 | `cmd/server/main.go` | `meta.NewStore(meta.NewPGLoader(pool), meta.DefaultTTL, now)`；`adminAPI.Invalidate = proc.Meta.Flush` |
| 失效钩子 | `internal/admin/admin.go` | `API.Invalidate func()` + `invalidate()`（nil 安全）；`create/update/delete` 成功后调用，`list/get` 不调用 |
| DB 用例（新增） | `internal/ingest/cache_test.go` | 4 个需 DB 的用例 + `countingLoader` 计数替身 |
| 既有用例适配 | `internal/ingest/exception_test.go` | 503 用例在关池后补一次 `Flush`（仅 setup，断言未动，见 Q-003） |
| 文档 | `docs/design.md` | 目录表增 `internal/meta/`；新增 §三之三 缓存口径表；§五.8 风险改写为"一致性代价"；§六.1 打勾 |

接口与任务书 §4 的闭集一致，未新增任何方法、配置项、环境变量、表或迁移。

## 2. 测试结果

### 2.1 无 DSN（CI 默认口径）

```
$ go build ./... && go vet ./...          # 均无输出
$ go test ./... -count=1
ok   internal/clean    0.004s
ok   internal/ingest   0.009s      # DB/Broker 用例 skip
ok   internal/meta     0.005s
ok   internal/staging  0.005s
ok   internal/validate 0.005s
```

`go test ./internal/meta/... -race -count=1` 7 个用例全 PASS（含 16 goroutine 并发读）。

### 2.2 有 DSN

```
$ IOT_TEST_DSN='postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable' go test ./... -count=1
ok   internal/clean 0.006s   ok internal/ingest 2.777s   ok internal/meta 0.003s
ok   internal/staging 0.003s ok internal/validate 0.007s
```

`internal/ingest` 测试函数 **36 → 40**（+4 个缓存用例），`-v` 下 PASS 38、SKIP 2（`TestCapacityBaseline10kMessages` 需 `IOT_CAPACITY=1`、`TestHTTPAndMQTTPathsProduceIdenticalResults` 需 `MQTT_BROKER_URL`，与 rv-03 相同）。

### 2.3 验收标准逐条对照（task-001 §8）

| # | 验收项 | 结果 | 证据 |
| --- | --- | --- | --- |
| 1 | 同 key 连读 3 次，loader 只 1 次 | ✅ | `TestLoadTerminalCachesSuccess` |
| 2 | `Flush()` 后重新穿透 | ✅ | `TestFlushForcesReload`（1 → 2 次） |
| 3 | 错误不缓存，连续 2 次错误 = 2 次调用 | ✅ | `TestLoaderErrorNotCached` |
| 4 | `ErrTerminalUnknown` 不缓存 | ✅ | `TestTerminalUnknownNotCached` |
| 5 | 假时钟前进 > TTL 自动穿透一次 | ✅ | `TestTTLExpires`（29s 命中 / 31s 穿透；`ttl=0` 走 DefaultTTL） |
| 6 | `-race` 16 goroutine 同 key，调用次数 ≤ 并发数 | ✅ | `TestConcurrentReadsSameKey`（`-race` 干净） |
| 7 | 同终端两条消息，第二条元数据查询 0 次 | ✅ | `TestCacheSkipsMetadataQueriesOnSecondMessage`：首条 4 次（终端/物模型/规则/窗口各 1），第二条 **0 次** |
| 8 | PUT 物模型改 `required` 后立即判 `msg_incomplete` | ✅ | `TestAdminPropertyUpdateTakesEffectImmediately`（`missingCodes=[hum]`） |
| 9 | 停用终端 TTL 内仍判 `terminal_unknown`，启用后 `Flush` 立即生效 | ✅ | `TestAdminTerminalToggleTakesEffectImmediately`（连续 2 次拒绝 → 启用后接收） |
| 10 | 现有用例在 DSN 下全绿（行为零变化） | ⚠️ 全绿，但 503 用例的故障注入需补 `Flush` | 见 §2.2 与 Q-003 |
| 11 | 无 DSN：`go test ./...` 全绿、`go vet ./...` 无输出 | ✅ | §2.1 |

补充用例（R-004 兜底）：`TestTTLFallsBackForDirectDBEdit` —— 直接 `update thing_model_property` 不走管理 API 时，TTL 内沿用旧定义、超过 TTL 后按新定义判 `msg_incomplete`。

**用例敏感性（变异验证）**：把 `handler.Invalidate` 置 `nil` 后重跑，`TestAdminPropertyUpdateTakesEffectImmediately` 与 `TestAdminTerminalToggleTakesEffectImmediately` 双双 FAIL（旧定义/旧 enabled 生效），证明用例能捕捉"失效钩子未接"。

## 3. 性能

任务书未要求容量实测（容量项属 R-007 DEFERRED），仅给出可断言的计数：单条消息元数据查询 **4 → 0**（首条仍 4 次）。按 `capacity-record.md:79` 的口径（每条消息 6 次 PG round trip 中 4 次是元数据），稳态下每消息 round trip 由 6 降到 2。真实吞吐增益未测。

## 4. 风险

1. **数据丢失**：无。缓存只读元数据，不参与写入路径；写路径（`latest_value`/`hourly_staging`/`exception_*`）明确未缓存。
2. **一致性**：缓存命中期间 DB 不可用时不再整包 503，故障推迟到写路径（`cache_write_failed` + 200），见 Q-003；绕过管理 API 直接改库最长脏 30s（R-004 的既定代价）。
3. **性能退化**：写成功后全量清空，冷缓存的首批消息恢复 4 次元数据查询；管理 API 是低频人工操作，影响面小。
4. **部署依赖**：与 `ShardedLock` 同口径的单实例前提（R-005）。多实例各自持有副本，跨实例失效不生效——本期范围外。
5. **共享可变数据**：`LoadProperties` 返回的切片与 `LoadRules` 返回的 map 在多个 goroutine 间共享同一份，调用方只读的约定目前由管道代码满足（`propByCode` 是逐次新建的 map）。
6. **未验证假设**：U-001 仍为 `ASSUMED`（是否存在绕过管理 API 直接改库的写入路径）；测试覆盖了 TTL 兜底路径，但真实运维脚本是否存在未核实。
7. **文档不一致（未改，超 §4 清单）**：`internal/validate/model.go:39` 仍留着 `shortcut: 每条消息两次元数据查询，未加进程内缓存` 的注释，现已过期；`docs/capacity-record.md:79` 仍写"本期未做"。两处都在 task-001 §4 文件清单之外，未擅自修改。

## 5. Deviation 汇总

| # | 偏离 | 原因 | 建议 |
| --- | --- | --- | --- |
| D1 | 改装配波及 `capacity_test.go:49`、`mqtt_test.go:51`（§4 未列名） | `Processor.Reg` 换类型后不改编译失败；仅构造，断言未动 | **已闭环**：Q-002 由 reviewer 答复「放行：只允许改构造行，§4 属漏列，不构成偏离」 |
| D2 | 改 `exception_test.go` 的 503 用例 setup（关池后加 `Flush`） | 首条消息预热缓存后，"关池即触发元数据读失败"的前提被缓存打破；不改 setup 则断言 503 无法成立 | 见 Q-003，需评审确认"缓存命中期间 DB 故障仍须 503"是否成立 |
| D3 | `Processor` 字段名 `Reg` → `Meta`（任务书只说换类型） | 避免"注册表"与"缓存"同名混淆 | 无异议则可保留 |
| D4 | 新增 `TestTTLFallsBackForDirectDBEdit`（§8 未列） | 直接证明 R-004 的 TTL 兜底在真实管道上成立，而不只是在 `meta` 包内 | 可保留或移入下一期 |

## 6. 未闭环问题

- `Q-002`（ANSWERED → 本次 report 提交以 `Answer:` 闭环）：放行，仅改构造行。
- `Q-003`（OPEN）：缓存命中期间 DB 不可用的 503 口径，以及 503 用例 setup 适配是否接受。
- `Q-001`：已在首个提交 `3a1d54e` 的 `Answer:` 闭环。
