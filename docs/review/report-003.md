# report-003 · 写路径平台故障改走 503（R-014 / Q-003 落地）

- `task:` task-003（`status: READY`）
- `baseline:` rv-05（d5fd48b）
- `tag:` **rv-06**
- `commit:` `b82a788`（单提交，按 §6）
- `diff 范围:` `git diff --name-only rv-05..b82a788` = `internal/ingest/pipeline.go`、`internal/ingest/exception_test.go`、`docs/design.md`（与 §4 三文件一致）

## 1. 变更说明

| 文件 | 改动 |
| --- | --- |
| `internal/ingest/pipeline.go:215-224` | `write()` 失败分支：保留 `p.Exc.Event(cache_write_failed, …)`，返回值 `Reason` 由 `string(exception.CacheWriteFailed)` 改为 `ReasonInternal`（`Accepted` 仍为 `false`）。`http.go` 既有判定（results 中出现 `internal_error` → 整包 503）无需改动。 |
| `internal/ingest/exception_test.go` | 新增 `TestWriteFailureReturns503`：双终端批次、先跑一次把元数据读进缓存，再把 `proc.DB` 换成新连接池并关闭，只让写路径失败（异常记录器仍用原池）→ 断言 HTTP 503、`message=internal_error`、两条 `results` 均 `internal_error` 且 `accepted=false`、`exception_count` 中 `cache_write_failed` ≥ 1。 |
| `docs/design.md` | §二.12 补"写路径失败同属平台侧故障：异常仍记 `cache_write_failed`，逐终端 `internal_error` → 整包 503"；§五.10 补"读元数据失败与写路径失败都归入整批重传，重传安全性由 `value_time` 相等不覆盖兜底"。 |

## 2. 测试结果

**无 DSN**

```
$ GOCACHE=/tmp/gocache go build ./... && go vet ./...     # 均无输出
$ go test ./... -count=1
ok cmd/server  ok internal/admin  ok internal/clean  ok internal/ingest
ok internal/meta  ok internal/staging  ok internal/ui  ok internal/validate
```

**有 DSN**（`postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable`）

```
$ go test ./internal/ingest -count=1 -run 'TestWriteFailureReturns503|TestPlatformFailureReturns503' -v
--- PASS: TestPlatformFailureReturns503 (0.05s)
--- PASS: TestWriteFailureReturns503 (0.06s)
$ go test ./... -count=1                                  # 全绿（9 个包）
```

**用例敏感性（变异验证）**：把 reason 改回 `cache_write_failed` 后重跑，`TestWriteFailureReturns503` 立即失败并报"写路径故障应返回 503，实际 200 ok"，说明该用例确实锁住了这次改动。

**验收 §7 逐条**

| 验收项 | 结果 | 证据 |
| --- | --- | --- |
| 有 DSN：写失败后 `Results` 每项 `internal_error` + `Accepted=false` | ✅ | `TestWriteFailureReturns503` 遍历两条 results 断言 |
| 有 DSN：`HTTPHandler` 返回 503 且 body 带完整 `results` | ✅ | 同用例断言 `data.results` 长度 2 |
| 有 DSN：`exception_count` 中 `cache_write_failed` ≥ 1 | ✅ | 同用例按小时聚合计数断言 |
| 无 DSN：`go test ./...` 全绿、`go vet ./...` 无输出 | ✅ | §2 第一段 |
| `git diff` 只触及 §4 三个文件 | ✅ | 见头部 diff 范围 |

## 3. 风险

1. **重传放大**：写失败改为 503 后，网关会整批重传；已成功终端会重复写（`value_time` 相等不覆盖，安全）并重复发布实时消息（design §五.10 已接受，不做幂等）。本次未新增去重逻辑（§3 非目标）。
2. **异常可观测性依赖异常表可用**：异常表本身不可用时只记日志、仍返回 503，`cache_write_failed` 会丢——这是既有行为，本次未改（§5 硬约束只要求"不因返回 503 而丢"，已满足）。
3. **测试库并发**：`internal/ingest` 的 DSN 用例不支持同库并发（两个进程同时跑 `./...` 会互踩 fixture）。本次验证时确实撞到一次（并行跑出 `TestBatchIsolatesFailingTerminal` 假失败，单独跑全绿）。该问题与本次改动无关，`be-api` 已在 `Q-005` 提出，建议另立任务书做测试库隔离。

## 4. 与其他任务书的边界

- 本提交不含任何 React/UI 改动；`task-004` 的 UI 工作由另一位执行者推进。
- 按 R-020，`task-003` 先落并打 `rv-06`；但**后端契约提交（`8208525`/`d117983`/`03ca27e`，属 task-004 步 1）先行落在了同一条线上**。因此 `rv-06` 的实际范围是「task-004 步 1 + task-003」，审核请按提交分组：`8208525`/`d117983`/`03ca27e` = task-004 步 1（后端契约），`b82a788` = task-003（本报告）。未改写历史（禁止 rebase）。
- `Q-004`（`internal/ui` 里 `entityLabels`/`exceptionEntities`/`exceptionRoutes` 与 admin 侧重复）留给前端步 6 收口，改用 `admin.EntityLabel` / `admin.ExceptionPages` / `admin.ExceptionBySlot|ByEntity`。
- `Q-005`（测试库并发隔离）未处理，等排期。
