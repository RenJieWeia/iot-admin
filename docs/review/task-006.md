# task-006 · 测试库隔离：让带 DSN 的 `go test ./...` 默认并行也能绿

- `supersedes:` 无
- `date:` 2026-11-06
- `decisions:` R3（来自 REVIEW-002/005/006）
- `depends-on:` 无（`rv-09` 已收口）
- `baseline:` `rv-09`（`284a463`）
- `status:` READY —— Lead 代拟，reviewer 可随时改版（`version+1` 只发差异）
- `handoff:` 单提交或两提交（辅助函数 + 各包接入），`Base: rv-09`

## 1. 背景与目标

带 `IOT_TEST_DSN` 时 `go test ./...` 默认按包并行，`internal/ingest` 的 `truncate`（`helper_test.go:112-117`，11 张业务表 `restart identity cascade`）会清掉 `internal/admin` DB 用例正在使用的行；反向亦然（admin 的 `cleanup()` 删 `qtest-*` 对 ingest 断言是噪声）。现象：`TestBatchIsolatesFailingTerminal` / `TestAdminAPICRUD` 随机红；`-p 1` 才稳定。

目标：不改任何既有断言、不改 `internal/store` 生产代码、不加依赖的前提下，让**默认并行**的 `go test ./...` 稳定全绿。

## 2. 方案（已定，按此实现）

**独立库名（每包一个库），DSN 派生。**

- 基础 DSN 仍由 `IOT_TEST_DSN` 提供（运维只维护一条）。
- 每个需要 DB 的测试包，在包内 test helper 里把 DSN 的库名替换为 `<原库名>_<包名>`（如 `iot_test_admin` / `iot_test_ingest`），并保证库存在（幂等）。
- 建库方式：先用同一 hosts/凭据连维护库（`postgres`），查 `pg_database` 不存在则 `create database`；然后照常 `store.Connect` + `store.Migrate`。**不得**改 `internal/store` 的生产代码。
- helper 允许在两个包里各写一份（约 20–30 行），**不新增共享包**（避免为测试引入新 package 到模块树）。

## 3. 非目标

- 不改任何既有断言、不改 `nongoal_test.go`。
- 不改 `internal/store`、`migrations/`、`go.mod`/`go.sum`。
- 不改 `internal/admin`、`internal/ingest` 的生产代码（只动 `_test.go`）。
- 不引 testcontainers / docker 依赖；不引入新 Go 依赖。
- 不改 CI 之外的口头约定：`-p 1` 仍可用，但不再是唯一保证。

## 4. 涉及文件

| 层 | 路径 | 说明 |
|---|---|---|
| 改 | `internal/ingest/helper_test.go` | `newEnv` 使用派生 DSN（库名 `<base>_ingest`），保持清理逻辑不变 |
| 改 | `internal/admin/query_test.go` / `internal/admin/exceptions_test.go` | 改用派生 DSN（库名 `<base>_admin`），`cleanup()` 逻辑不变 |
| 新增 | 各包内 `dsn_test_helper.go`（或在上述文件内的私有函数） | `deriveDSN(base, suffix)` + `ensureDatabase(dsn)` |
| 改 | `docs/test-env.md` | 记录“每包一个测试库”、建库幂等行为、两种验收命令 |

## 5. 硬约束

- `IOT_TEST_DSN` 未设置时，所有 DB 用例仍 `t.Skip`（行为不变）。
- 库名派生必须幂等：重复运行、并发运行都不报错。
- 不要求用户改 `IOT_TEST_DSN`，也不新增环境变量。
- 若测试库用户无建库权限：跳过建库并**明确失败信息**（`t.Skipf` 带原因）而不是静默用错库。
- 不改测试语义：断言、夹具、清理范围与现在一致。

## 6. 验收（可执行）

1. `IOT_TEST_DSN=… go test ./... -count=1` **连续 3 次**全绿（默认并行，不再依赖 `-p 1`）。
2. `IOT_TEST_DSN=… go test -p 1 ./... -count=1` 全绿（兼容路径仍在）。
3. 无 `IOT_TEST_DSN`：`go test ./...` 全绿（DB 用例 skip）。
4. 用例断言派生结果：`deriveDSN` 对 `postgres://…/iot` 产出 `…/iot_admin`、`…/iot_ingest`；非法 DSN 报错清晰。
5. `git diff --name-only rv-09..HEAD` 只含 `_test.go` 与 `docs/test-env.md`。
6. `go.mod`/`go.sum`/`migrations/`/`nongoal_test.go` 零改动。

## 7. 交付格式

提交 trailer 带 `Decision: R3`、`Test:`（含上面 3 次并行跑的输出）、`Review-Ready: yes`；`docs/review/report-006.md` 写：方案、派生规则、建库幂等性、失败模式（无权限时）、验收输出。
