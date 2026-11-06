# report-013 · 补录留痕（版本记录表）+ 对外口径修正

- `task:` task-013（`docs/review/task-013.md`）
- `baseline:` `rv-16`（`f22be73`）
- `commits:`
  | 步 | hash | 内容 |
  |---|---|---|
  | 1 / R-034 | `d3d61e9` | 迁移 `0007_snapshot_cell_history.sql` + `UpsertSnapshot returning revision` + `InsertVersions` + 保留期清理 |
  | 2 / R-034·R-031 | `dc47438` | `pipeline` 接版本表（仅补录、同一事务）+ 用例 |
  | 3 / R-035 | `abbf904` | 口径修正（`Written`/`Accepted`/`regressed`）+ 用例更新 |
  | — | 本报告 | `docs/review/report-013.md` |
- `red-lines:` `nongoal_test.go` **只有白名单 +1 行**（`snapshot_cell_history`），禁词循环与其它断言一字未动；
  `admin.Entities` 仍 6；`go.mod`/`go.sum` 未动；`hourly_snapshot` 未加 `source` 列

## 0. 表名改名（与任务书的偏差，Lead 已裁决）

任务书 §2.A 写的是 `snapshot_version`，但 `internal/ingest/nongoal_test.go` 的禁词表含 `version`
（本意挡 `snapshot-versions` 一类非目标表），只加白名单仍会 `fatal`。Lead 裁决选 B：**表名改为
`snapshot_cell_history`**，不动禁词循环。DDL 结构、列定义、索引语义与书一致：

| 项 | 书 | 实现 |
| --- | --- | --- |
| 迁移文件 | `0007_snapshot_version.sql` | `0007_snapshot_cell_history.sql` |
| 表 | `snapshot_version` | `snapshot_cell_history` |
| 索引 | `..._cell_idx` / `..._changed_idx` | `snapshot_cell_history_cell_idx` / `snapshot_cell_history_changed_idx` |

`grep -rn "snapshot_version" internal/ migrations/` = **0**。`nongoal_test.go` 仍只有白名单 +1 行。

## 1. 变更说明

### 1.1 步 1 · 版本表与写入接口（`d3d61e9`）

- `migrations/0007_snapshot_cell_history.sql`：表 `snapshot_cell_history`
  (`id, terminal_id, item_code, hour_start, revision, source, value_time, changed_at, actor`)
  + `cell_idx (terminal_id,item_code,hour_start, revision desc)` + `changed_idx (changed_at)`。
- `settle.UpsertSnapshot` 由 `Exec` 改 `Query … returning terminal_id, item_code, hour_start, revision`，
  返回 `[]CellRevision`（**实际写入**的格子与写入后的版本号）；`value_time` 守卫与
  `revision = revision + 1` 的原子递增语义不变，被守卫跳过的行不出现在返回值里。
  `Settler.Run` 的 `snapshot_writes` 计数改为 `len(cells)`（`rows` 仍是结算的暂存行数）。
- `settle.InsertVersions(ctx, tx, []VersionRow)`：批量写版本行；`source` 闭集
  `VersionSourceMQTTBackfill=1` / `VersionSourceManualAPI=2`；`actor` 空串落 `NULL`。
- `settle.partition.dropExpired`：DROP 过期分区前先
  `delete from snapshot_cell_history where changed_at < cutoff`（与快照同寿命）。
- 夹具：`newEnv` 的 truncate 列表补 `snapshot_cell_history`（测试隔离，非断言改动）。

### 1.2 步 2 · pipeline 接版本表（`dc47438`）

- 新增 `writeVersionMeta{source, actor}` 与 `versionSourceFor(msgType)`：**只有 `msgType=2`** 映射到
  `source=1`，其余（缺省 / 1 / 未知）为 0 → 不留痕。手工 API 的 `actor` 已留参数位（本期无调用方）。
- `write()` 用 `UpsertSnapshot` 返回的 `CellRevision` 组装 `VersionRow`（`revision` 取写入后的值、
  `value_time` 取该格本次采集时间），**在同一事务**里 `InsertVersions`；快照失败回滚，版本行不残留。

### 1.3 步 3 · 口径修正（`abbf904`）

| 项 | 旧 | 新（R-035） |
| --- | --- | --- |
| `results[].written` | `latest.Upsert` 的返回行数 | **实际写入任一表的行数**（窗口内 staging + 窗口外 snapshot） |
| 窗口外写入成功 | `accepted=false` + `reason=late_data` | `accepted=true` + `reason=""`（迟到只由 `late_data` 事件承载） |
| 幂等重发（守卫跳过） | 窗口外曾被判迟到 | 同样 `accepted=true`（`written=0`） |
| `regressed`（回拨 `protocol_error`） | 两个窗口都判 | **只对窗口内项生效** |
| 写失败 | `internal_error` + 503 | 不变（`accepted=true` 不会掩盖） |

### 1.4 文档

`docs/design.md`：§二 新增 **17 补录留痕（R-034）**、**18 结果口径（R-035）**；§五 新增
**13 版本表容量**、**14 `results[].written` 数值变大的影响**；顺带修正 §二 里重复的编号
（`单位换算短路` 14 → 19）。

> ⚠️ **协作事故（需 Lead 知悉，不改写历史）**：这批 `docs/design.md` 的改动在工作区
> 未提交时，被 `fe-web` 的 `199dd76`（F5 Makefile）一并提交了（其 `Scope:` 也写了
> `docs/design.md`）。**内容在 HEAD 上是完整正确的**，只是归属落在那个提交里；我的
> `abbf904` 因此不含 design.md。建议在复核记录里注明，不做 rebase/amend。

## 2. 测试结果

### 2.1 干净 worktree（`git worktree add .devcache/wt-t13 abbf904`）

```
$ git rev-parse --short HEAD
abbf904
$ git status --short
(空=干净树)
$ grep -rn "snapshot_version" internal/ migrations/ | wc -l
0

$ go build ./... && go vet ./...
(无输出)

$ go test ./... -count=1   # 无 DSN
ok  cmd/server 0.016s | internal/admin 0.020s | internal/clean 0.009s | internal/ingest 0.010s
ok  internal/meta 0.005s | internal/staging 0.006s | internal/ui 0.010s | internal/validate 0.006s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1
ok  cmd/server 0.005s | internal/admin 0.264s | internal/clean 0.003s | internal/ingest 7.333s
ok  internal/meta 0.004s | internal/staging 0.004s | internal/ui 0.006s | internal/validate 0.003s
```

### 2.2 定向用例（有 DSN）

| 用例 | 覆盖的验收项（task-013 §7） | 结果 |
| --- | --- | --- |
| `TestInsertVersionsWithDB` | 首次写快照 `revision=0` → `InsertVersions` → 版本行 `revision/source/actor` 对齐 | PASS |
| `TestDropExpiredRemovesOldVersions` | 30 个月前的版本行在 `EnsurePartitions` 时删除，当前行保留 | PASS |
| `TestBackfillWritesCellHistory` | 实时写入 → 版本表 0 行；补录首写 → 版本行 2、`revision=0`；更晚补录 → `revision=1`、版本行 4、最新行 `source=1`；更早补录 → 幂等跳过、版本行仍 4 | PASS |
| `TestBackfillOlderValueNoProtocolError` | 窗口外补录旧值不产生 `protocol_error`；窗口内回拨仍产生 | PASS |
| `TestLateDataOutsideWindow`（改断言） | 窗口外 → `accepted=true`、`reason=""`、`written=2`、staging 0、快照 2 行、无发布、`late_data` 仍记 | PASS |
| `TestWindowBoundaryIsClosedAtCloseTime`（改断言） | 关闭时刻 → `accepted=true`（窗口外直写快照），仍不进 staging | PASS |
| `TestRepeatedSettleProducesNoSecondRow` / `TestTwoPhaseWriteMixedBatch` / `TestLateDataDetailCarriesSourceAndMsgType`（改断言） | 窗口外终端的 `accepted` 期望改为 true，其余断言不变 | PASS |
| `TestWriteFailureReturns503`（未改） | 写失败仍 `internal_error` + 503，不被 `accepted=true` 掩盖 | PASS |
| `TestNoOutOfScopeArtifacts` | 白名单 +1 行后通过；禁词循环未动 | PASS |

## 3. 口径变化说明（对外行为）

1. **`results[].written` 数值变大**：窗口外补录过去是 `0`，现在是实际写入行数（每项一行）。
   网关/对账侧若按旧数值做阈值或断言，需要同步调整。
2. **窗口外不再回 `late_data` reason**：`accepted=true`、`reason=""`。迟到信息只存在于
   `exception_count.late_data` 事件（detail 带 `source=`/`msgType=`）。若下游原本靠
   `accepted=false` 识别"迟到"，需要改为看事件表。
3. **幂等重发**（`value_time` 相等被守卫跳过）现在也是 `accepted=true`、`written=0`，不再有 reason 提示。
4. **真实失败不变**：写路径失败仍逐终端 `internal_error` + 整包 503。

## 4. 风险

1. **版本表容量**：只在补录写入时增长，与补录频率成正比；集中回填会快速累积，靠保留期
   （`changed_at` 早于快照截止月即删）兜底。
2. **写放大**：补录报文单次写两张表（快照 + 版本）。批量回填时 WAL 与锁持有时间相应增加。
3. **表名与任务书不一致**：后续任务书/研报引用时需用 `snapshot_cell_history`（见 §0）。
4. **`written` 语义变化**：见 §3.1，属对外可见变化。
5. **`revision` 与版本行的一致性**：版本行取 `returning` 的值，天然与快照对齐；但**历史数据**
   （0007 之前写入的格子）没有版本行，版本历史只能从本批之后追。

## 5. 未闭环

- 无新增 `Q-NNN`；版本历史查询端点按书为非目标（表与索引已就绪，后续按需加）。
