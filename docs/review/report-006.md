# report-006 · 测试库隔离（task-006 / R3）

- `task:` task-006（v1，`status: READY`）
- `baseline:` `rv-09`（`284a463`）
- `commits:`
  | 内容 | hash |
  |---|---|
  | 代码：每包独立测试库 + 5 处接入 | `5b413d3` |
  | 本报告 | 见 `git log -- docs/review/report-006.md` |
- `docs:` `docs/test-env.md` §6 已随 Lead 的 `acaaf0a` 入库（本报告的改动清单里不再重复）
- `red-lines:` `go.mod`/`go.sum`/`migrations/`/`nongoal_test.go`/`internal/store` 零改动；
  生产代码零改动（diff 只有 `_test.go`）

## 1. 方案

**独立库名 + DSN 派生**（任务书 §2 方案 1）。运维只维护一条 `IOT_TEST_DSN`，每个需要 DB 的
测试包在包内把库名换成 `<原库名>_<包短名>`，并把建库做成幂等操作。

| 包 | 使用的库 |
| --- | --- |
| `internal/admin` | `iot_admin` |
| `internal/ingest` | `iot_ingest` |

改动落点（全部 `_test.go`）：

| 文件 | 改动 |
| --- | --- |
| `internal/admin/dsnhelper_test.go`（新增） | `deriveDSN` / `ensureDatabase` / `testDSN` + `TestDeriveDSN` |
| `internal/ingest/dsnhelper_test.go`（新增） | 同上（另一份拷贝，后缀 `ingest`） |
| `internal/admin/{query,exceptions,options}_test.go` | 取 DSN 的 3 处改 `testDSN(t)` |
| `internal/ingest/{helper,exception}_test.go` | 取 DSN 的 2 处改 `testDSN(t)` |

没有新增共享包（任务书 §2 要求），也没有改 `internal/store`。两个包各一份拷贝是有意为之：
为测试引入一个新 package 到模块树不值得，20–30 行重复可接受。

## 2. 派生规则

`deriveDSN(base, suffix)`：解析 URL，只替换 path 里的库名，其余（scheme/userinfo/host/port/query）原样。

| 输入 | 后缀 | 输出 |
| --- | --- | --- |
| `postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable` | `admin` | `postgres://postgres:pg@127.0.0.1:55432/iot_admin?sslmode=disable` |
| 同上 | `ingest` | `…:55432/iot_ingest?sslmode=disable` |
| `postgresql://u:p@db:5432/iot` | `admin` | `postgresql://u:p@db:5432/iot_admin` |
| `postgres://u:p@h:5432/iot_test` | `ingest` | `postgres://u:p@h:5432/iot_test_ingest` |

非法输入报错且**不回显 DSN**（避免口令进测试日志）：空后缀、缺库名、缺库名带斜杠、
非 `postgres|postgresql` scheme 四类都有 `TestDeriveDSN` 覆盖（两个包各一份）。

`IOT_TEST_DSN` 未设置时仍 `t.Skip`，行为与改动前一致。

## 3. 建库幂等

`ensureDatabase(dsn)`：

1. 用同一凭据连维护库 `postgres`；
2. `select exists(select 1 from pg_database where datname = $1)`；
3. 已存在 → 返回，什么都不做；
4. 不存在 → `create database <name>`（标识符经 `pgx.Identifier{name}.Sanitize()` 转义）；
   并发建库撞上 `42P04 duplicate_database` 视为已存在（多个进程同时首次跑也不报错）；
5. 之后照常 `store.Connect` + `store.Migrate`，迁移在各自库里跑。

实测（`pg_database` + `information_schema`）：`iot` / `iot_admin` / `iot_ingest` 三个库都存在，
两个派生库各自有 9 张业务表（`data_domain`…`exception_interval` + `schema_migrations`）。
连续 3 次并行跑 + 1 次 `-p 1` 跑，建库路径都是幂等返回，没有 `already exists` 报错。

## 4. 失败模式

| 场景 | 行为 |
| --- | --- |
| 测试用户无 `createdb`/建库权限（SQLSTATE `42501`） | `ensureDatabase` 返回哨兵错误 → 用例 `t.Skipf("跳过需要 PostgreSQL 的用例：测试用户没有建库权限…")`；**不会**退回基础库 |
| 查询 `pg_database` 被拒（同样是权限） | 同上，跳过并说明原因 |
| 连不上维护库 / 其它 SQL 错误 | `t.Fatalf`，不吞、不降级 |
| 并发首次建库 | `42P04` 视为已存在，照常继续 |

`_meta`/`options` 这类不碰库的用例不受影响（`New(nil)`），仍随包常规执行。

## 5. 验收输出

命令统一前置 `GOCACHE=$PWD/.devcache/gocache`，DSN 为
`postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable`。

### 5.1 默认并行，连跑 3 次

```
===== 并行 1 =====
ok  github.com/RenJieWeia/iot-admin/cmd/server          0.008s
ok  github.com/RenJieWeia/iot-admin/internal/admin      0.140s
ok  github.com/RenJieWeia/iot-admin/internal/clean      0.003s
ok  github.com/RenJieWeia/iot-admin/internal/ingest     2.362s
ok  github.com/RenJieWeia/iot-admin/internal/meta       0.004s
ok  github.com/RenJieWeia/iot-admin/internal/staging    0.004s
ok  github.com/RenJieWeia/iot-admin/internal/ui         0.008s
ok  github.com/RenJieWeia/iot-admin/internal/validate   0.005s
===== 并行 2 =====
ok  github.com/RenJieWeia/iot-admin/cmd/server          0.006s
ok  github.com/RenJieWeia/iot-admin/internal/admin      0.139s
ok  github.com/RenJieWeia/iot-admin/internal/clean      0.005s
ok  github.com/RenJieWeia/iot-admin/internal/ingest     2.338s
ok  github.com/RenJieWeia/iot-admin/internal/meta       0.004s
ok  github.com/RenJieWeia/iot-admin/internal/staging    0.004s
ok  github.com/RenJieWeia/iot-admin/internal/ui         0.007s
ok  github.com/RenJieWeia/iot-admin/internal/validate   0.005s
===== 并行 3 =====
ok  github.com/RenJieWeia/iot-admin/cmd/server          0.005s
ok  github.com/RenJieWeia/iot-admin/internal/admin      0.131s
ok  github.com/RenJieWeia/iot-admin/internal/clean      0.003s
ok  github.com/RenJieWeia/iot-admin/internal/ingest     2.347s
ok  github.com/RenJieWeia/iot-admin/internal/meta       0.006s
ok  github.com/RenJieWeia/iot-admin/internal/staging    0.004s
ok  github.com/RenJieWeia/iot-admin/internal/ui         0.006s
ok  github.com/RenJieWeia/iot-admin/internal/validate   0.004s
```

### 5.2 兼容路径 `-p 1`

```
ok  github.com/RenJieWeia/iot-admin/cmd/server          0.005s
ok  github.com/RenJieWeia/iot-admin/internal/admin      0.134s
ok  github.com/RenJieWeia/iot-admin/internal/clean      0.003s
ok  github.com/RenJieWeia/iot-admin/internal/ingest     2.488s
ok  github.com/RenJieWeia/iot-admin/internal/meta       0.003s
ok  github.com/RenJieWeia/iot-admin/internal/staging    0.002s
ok  github.com/RenJieWeia/iot-admin/internal/ui         0.007s
ok  github.com/RenJieWeia/iot-admin/internal/validate   0.003s
```

### 5.3 无 DSN

```
ok  github.com/RenJieWeia/iot-admin/cmd/server          0.005s
ok  github.com/RenJieWeia/iot-admin/internal/admin      0.009s
ok  github.com/RenJieWeia/iot-admin/internal/clean      0.004s
ok  github.com/RenJieWeia/iot-admin/internal/ingest     0.007s
ok  github.com/RenJieWeia/iot-admin/internal/meta       0.003s
ok  github.com/RenJieWeia/iot-admin/internal/staging    0.003s
ok  github.com/RenJieWeia/iot-admin/internal/ui         0.007s
ok  github.com/RenJieWeia/iot-admin/internal/validate   0.004s
```

`go vet ./...` 无输出；`TestDeriveDSN` 在两个包内各跑一次均通过。

## 6. 验收对照

| 验收项（task-006 §6） | 结果 |
| --- | --- |
| 1. 带 DSN 默认并行连跑 3 次全绿 | ✅ §5.1 |
| 2. `-p 1` 全绿（兼容路径） | ✅ §5.2 |
| 3. 无 DSN 全绿（DB 用例 skip） | ✅ §5.3 |
| 4. `deriveDSN` 断言（`iot` → `iot_admin`/`iot_ingest`）+ 非法 DSN 报错 | ✅ `TestDeriveDSN` ×2 |
| 5. diff 只含 `_test.go` 与 `docs/test-env.md` | ✅ `git diff --name-only rv-09..5b413d3` = 7 个 `_test.go`；`docs/test-env.md` 已由 Lead 的 `acaaf0a` 入库 |
| 6. `go.mod`/`go.sum`/`migrations/`/`nongoal_test.go` 零改动 | ✅ 同上 diff 不含这些路径；`internal/store` 亦零改动 |

## 7. 风险与遗留

1. **首次运行需要建库权限**：无权限时 DB 用例跳过（不是失败）。若 CI 用受限账号，需要预先
   建好 `iot_admin`/`iot_ingest` 两个库，否则那批用例会批量 skip 而不报错——建议 CI 显式断言
   "非 skip 用例数"或预建库。
2. **基础库 `iot` 不再被测试写入**：里面可能残留历史测试数据（早前的 `qtest-*`、ingest 夹具），
   现在不会被清理，也不会影响结果；需要的话手工 `truncate`。
3. **新增 DB 测试包需要复制 helper**：没有共享包是任务书 §2 的明确要求，代价是第三个包要再加
   20–30 行拷贝；如果后续包数继续增长，建议下一个任务书评估"测试辅助小包"。
4. **未实测"无建库权限"分支**：本地 PG 的 `postgres` 是超级用户，无法在不改实例权限的前提下
   构造 42501。分支按 SQLSTATE 映射实现并注释，属**未验证假设**；若要覆盖，需要在 CI 里用
   受限角色跑一次。
