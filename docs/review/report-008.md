# report-008 · 批 3：两段写入模型 + 快照版本化 + 变更查询

- `task:` task-008（`status: READY`）
- `baseline:` `rv-12`（`d909596`）
- `commits:`
  | 步 | hash | 内容 |
  |---|---|---|
  | 1 / R-030 | `0aa098e` | 迁移 0006 + `settle.UpsertSnapshot` 唯一实现 + 结算改 UPSERT |
  | 2 / R-030·R-031 | `acb8bf1` | `pipeline` 窗口外分流 + `msgType` 来源标记 + 用例 |
  | 3 / R-032 | `605a240` | `GET /admin/v1/snapshot/changes` + 用例 + design 口径 |
  | — | 本报告 | `docs/review/report-008.md` |
- `范围:` 我的三个提交只碰 `migrations/`、`internal/{settle,ingest,validate,admin}`、`docs/design.md`；
  `rv-12..HEAD` 区间里的 `web/**` 与 `internal/ui/dist/**` 来自 `fe-web` 的 `c79e5bb`（最新值只读页），
  与本任务无关。逐提交 `git show --name-only` 已核，互不混入。

## 1. 变更说明

### 1.1 步 1 · 快照版本化与唯一 UPSERT（`0aa098e`）

| 项 | 内容 |
| --- | --- |
| 迁移 | `migrations/0006_snapshot_revision.sql`：`hourly_snapshot` 加 `revision int not null default 0`、`settled_at timestamptz not null default now()`，建 `(settled_at, terminal_id, item_code, hour_start)` 索引 |
| 实现 | `settle.UpsertSnapshot(ctx, tx, rows []staging.Row) (int64, error)` —— 快照写入的**唯一**实现 |
| 语义 | `on conflict … do update set …, revision = hourly_snapshot.revision + 1, settled_at = now() where excluded.value_time > hourly_snapshot.value_time`（`revision` 在 SQL 内原子递增，相等/更早整行跳过） |
| 结算 | 结算改为「读待结算暂存行 → `UpsertSnapshot` → 删暂存」，同一事务；不再有第二份插入 SQL |
| 注释 | `migrations/0002_runtime.sql` 的「冻结语义 = 写入后不再 UPDATE」改为「可被修正（R-030）」 |

`Settler.Run` 的返回值语义调整为「本轮结算的暂存行数」（旧实现为插入行数）。这样"关闭时刻结算 2 行"
在快照已存在时仍成立；`Loop` 的日志随之（无调用方依赖精确语义）。

### 1.2 步 2 · 窗口外分流（`acb8bf1`）

| 判定 | 路径 |
| --- | --- |
| 归属小时未关闭（`staging.InWindow`） | `latest_value` + `hourly_staging`，等结算（行为与改动前一致） |
| 归属小时已关闭 | `latest_value` 照常写 + 直接 `settle.UpsertSnapshot`（`revision++`）；不发布实时，仍记 `late_data` |

- 判定仍只走 `staging.InWindow` / `CloseAt` 一处口径；同一批混存时在**一次事务**里分别落 staging 与 snapshot。
- 实时发布只覆盖窗口内写成功的项（新增 `inWindowOnly` 过滤），窗口外不产生 `iot/realtime/*`。
- 对外结果口径不变：整条消息全部落在窗口外时仍返回 `accepted=false` + `reason=late_data`
  （变化在于数据不再被丢弃——已写 `latest_value` 与 `hourly_snapshot`）。
- 新增可选 `msgType`（R-031）：`validate.Envelope.MsgType`（0 未声明 / 1 实时 / 2 补传 / 其它原样透传），
  **只进异常 detail 与日志，不参与任何路径判定**。
- 接入来源标记：新增 `Processor.HandleSource(ctx, source, …)`，HTTP 的 `Handle` 等价于 `source=api`，
  MQTT 回调改调 `HandleSource(ctx, "mqtt", …)`。`late_data` 的 detail 形如
  `source=mqtt msgType=2(补传) item=temp collectedAt=…`。

### 1.3 步 3 · 变更查询（`605a240`）

`GET /admin/v1/snapshot/changes?since=<RFC3339>&limit=&cursor=`

- `since` 必填且必须是 RFC3339（含偏移）；非法 → 400 `invalid_query`。
- `limit` 默认 200、上限 1000（超出截断，非报错）；非整数 → 400。
- `cursor` 不透明：`base64url(RFC3339Nano settled_at ␟ terminal ␟ item ␟ RFC3339Nano hour)`，只能原样回传；
  被篡改 → 400。
- SQL 用行值比较 `(settled_at, terminal_id, item_code, hour_start) > (…)` 做键集分页，多取一行判 `hasMore`。
- 响应：`{"code":0,"message":"ok","data":{"rows":[…],"limit":N,"hasMore":bool,"cursor":"…"}}`；
  `rows` 元素含 `terminalId/itemCode/hourStart/valueNum/valueText/unit/valueTime/revision/settledAt/quality/qualityReason`。
- 不进 `admin.Entities`（实体仍 6），不接 UI。

## 2. 测试结果

命令统一前置 `GOCACHE=$PWD/.devcache/gocache`，DSN 为 `postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable`。

```
$ go build ./... && go vet ./...
（无输出）

$ go test ./... -count=1                        # 无 DSN
ok  cmd/server 0.010s | internal/admin 0.012s | internal/clean 0.006s | internal/ingest 0.009s
ok  internal/meta 0.005s | internal/staging 0.004s | internal/ui 0.007s | internal/validate 0.005s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1
ok  cmd/server 0.005s | internal/admin 0.176s | internal/clean 0.003s | internal/ingest 3.188s
ok  internal/meta 0.004s | internal/staging 0.003s | internal/ui 0.005s | internal/validate 0.004s
```

定向用例（均有 DSN）：

| 用例 | 覆盖的验收项 | 结果 |
| --- | --- | --- |
| `TestSnapshotRevisionUpsert` | 首写 `revision=0`；更晚 `value_time` → 值覆盖 + `revision=1` + `settled_at` 前进；更早/相等 → 0 行且快照三字段不变 | PASS |
| `TestSettleUpdatesExistingSnapshot` | 结算对已存在行走 `DO UPDATE`，`revision=before+1`，仍 1 行 | PASS |
| `TestTwoPhaseWriteMixedBatch` | 同批「窗口内 + 窗口外」：前者进 staging 并发布、后者直写快照不发布、latest 都写、一次事务 | PASS |
| `TestLateDataOutsideWindow`（重写） | 窗口外 → latest 2 行 + snapshot 2 行（`revision=0`、`settled_at` 非空）+ staging 0 + 无发布 + `late_data` 记 2 次/1 行 | PASS |
| `TestRepeatedSettleProducesNoSecondRow`（重写） | 重复结算 0 行；更晚的窗口外值**修正**快照（`revision=1`、行数不变）——冻结语义作废 | PASS |
| `TestWindowBoundaryIsClosedAtCloseTime` | 窗口内行为零变化（关闭时刻判迟到、关闭前 1 秒接收、结算 2 行） | PASS（未改） |
| `TestLateDataDetailCarriesSourceAndMsgType` | detail 含 `source=mqtt` / `msgType=2(补传)` / `item=…`，且 msgType 不改变路径 | PASS |
| `TestParseEnvelopeMsgType` | 缺省 0 / 1 / 2 / 未知值透传，不影响终端解析 | PASS |
| `TestParseChangesQuery` | `since` 缺失/非法、`limit` 非整数、`cursor` 非法 → 400；limit 截断到 1000；HTTP 层同样 400（nil pool） | PASS |
| `TestChangesCursorRoundTrip` | 游标往返一致（纳秒），篡改串报错 | PASS |
| `TestChangesWithDB` | `since` 过滤、`limit=2` 逐页取完 5 行不重不漏、`revision/quality` 与库一致、未来 `since` 空页 | PASS |

真机 curl（临时实例，`127.0.0.1:18102`，用测试库）：

```
$ curl "/admin/v1/snapshot/changes?since=2026-11-05T23:53:38Z&limit=2"
{"code":0,"message":"ok","data":{"rows":[
  {"terminalId":"T1","itemCode":"power","hourStart":"2026-11-05T11:00:00+08:00","valueNum":2,
   "valueText":null,"unit":"kW","valueTime":"2026-11-05T11:10:00+08:00","revision":0,
   "settledAt":"2026-11-06T09:52:37.514893+08:00","quality":0,"qualityReason":""},
  {"terminalId":"T1","itemCode":"temp","hourStart":"2026-11-05T10:00:00+08:00","valueNum":20,
   "valueText":null,"unit":"C","valueTime":"2026-11-05T10:50:00+08:00","revision":0,
   "settledAt":"2026-11-06T09:52:37.514893+08:00","quality":0,"qualityReason":""}],
  "limit":2,"hasMore":false}}

$ curl -o /dev/null -w '%{http_code}' "/admin/v1/snapshot/changes?since=yesterday"
400
```

## 3. 覆盖的决策

| 决策 | 落点 |
| --- | --- |
| R-030 两段写入模型 | 步 1 + 步 2：窗口内/窗口外分流，窗口外直写快照，冻结作废 |
| R-031 `msgType` 仅来源标记 | 步 2：`Envelope.MsgType` + `late_data` detail，不参与路径判定 |
| R-032 `snapshot/changes` | 步 3：游标分页端点 |
| R-026 `late_data` 记账 | 步 2：窗口外仍记 `late_data`（detail 增 source/msgType） |
| R-027 只走 API | 未新增任何表/白名单；端点仍 `AllowAll` + 回环 |

## 4. 风险

1. **`revision` 引发的 UPDATE 膨胀**：快照从只插改成 UPSERT 后，被修正的格子会产生新行版本，依赖 autovacuum；
   集中回填时 WAL 与表膨胀会放大，保留期仍靠按月 DROP 分区兜底。
2. **窗口外写入依赖分区已存在**：补传跨度超出已建月度分区时写入失败（按书要求不自动建分区），
   表现为整包 503 而不是静默丢数据；`EnsurePartitions` 只覆盖当月/次月，长跨度回填前需要先扩分区。
3. **`accepted=false` + 已落库的组合**：对外结果口径没变（全窗口外仍 `late_data`），但数据实际已写入，
   联调时需要向网关说明"迟到 ≠ 丢弃"。
4. **`Settler.Run` 返回值语义变化**：由"插入行数"改为"结算行数"，若有外部告警按该数值做阈值需同步。
5. **`changes` 首页扫描**：`since` 很早时首页仍要扫 `(settled_at, …)` 索引（无分区裁剪），大表下需观察。
6. **`cursor` 未签名**：客户端篡改只会得到非法位置（400），不构成越权（鉴权仍 `AllowAll` + 回环）。

## 5. 未闭环

- 无新增 `Q-NNN`。
- `changes` 端点尚未接 UI（任务书非目标：UI 不展示 `revision`）。
