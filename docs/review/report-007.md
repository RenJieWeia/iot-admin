# report-007 · 批 2：毫秒时间戳 + 全字段必填 + 行级质量标记

- `task:` task-007 v2（`status: READY`；v2 只改了 R-029 的区间口径）
- `baseline:` `rv-11`（`241bef8`）
- `commits:`
  | 步 | hash | 内容 |
  |---|---|---|
  | 1 / R-029 | `8d5f438` | `collectedAt` 接受秒/毫秒时间戳 + design §二.3 |
  | 2 / R-021 | `f9b3109` | 物模型声明字段全必填 + 夹具改造 + design §二.4 |
  | 3 / R-022 | `02ac0d2` | `quality` / `quality_reason` 三表加列 + 写入链路 + design §二.14 |
  | — | 本报告 | `docs/review/report-007.md` |
- `红线:` `git diff --name-only rv-11..HEAD` 不含 `web/**`、`internal/ui/**`、`cmd/server/**`、`internal/admin/**`、`nongoal_test.go`、`go.mod`、`go.sum`

## 1. 三块变更

### 1.1 R-029 · `collectedAt` 数值时间戳（`8d5f438`）

`validate.ParseCollectedAt` 新增纯数字分支，按**范围**判定（v2 裁决：两端闭区间）：

| 取值 | 判定 |
| --- | --- |
| `1_577_836_800 ≤ v ≤ 4_102_444_800` | 秒（2020-01-01T00:00:00Z ~ 2100-01-01T00:00:00Z） |
| `1_577_836_800_000 ≤ v ≤ 4_102_444_800_000` | 毫秒 |
| `v < 下界` | 拒收，`collectedAt 数值早于可解释范围` |
| `v > 毫秒上界` 或落在两区间空档 | 拒收，`collectedAt 数值超出可解释范围` |
| 非纯数字 | 维持原「不可解析」分支 |

- 常数 `EpochSecondsMin/Max`、`EpochMillisMin/Max` 定义在 `validate` 包并注依据，用例不散写字面量。
- epoch 视为 UTC，解析后 `.In(staging.Zone)`；字符串格式（`+08:00` / `Z` / 省略偏移 / 毫秒）行为不变。
- `isDigits` 改为纯字符判定；纯数字但超 `int64` 归「超出可解释范围」。

### 1.2 R-021 · 声明字段全必填（`f9b3109`）

- `validate.MissingRequired` 遍历全部 `props`，不再读 `p.Required`（签名不变）。
- `thing_model_property.required` 列保留但**不参与校验**；`Property.Required` 字段保留并注明「仅元数据/前端提示」。
- `_meta.fields[].required`（写库约束层）与 `TestRequiredMatchesDDL` **未动**（任务书非目标）。
- 未声明的 `item_code` 仍忽略并计 `clean_failed`，不参与 missing 判定。

### 1.3 R-022 · 行级质量标记（`02ac0d2`）

| 层 | 改动 |
| --- | --- |
| `migrations/0005_quality_flag.sql`（新增） | 三表 `add column if not exists quality smallint not null default 0` + `quality_reason text`，只加列不建表 |
| `store.Value` | 加 `Quality int16` / `QualityReason string`；位常量 `QualityOK=0`、`QualityOutOfRange=1`、`QualityRuleHit=2`、`QualityUnitUnknown=4` |
| `latest.Upsert` | 每行参数 6 → 8，`on conflict` 同步覆盖两列（标记跟随 `value_time` 胜出的行） |
| `staging.Upsert` | 每行 7 → 9，同上 |
| `settle.insertClosedSQL` | 列清单加 `quality, quality_reason`，从 staging 原样带进 snapshot |
| `ingest/pipeline.go` | 物模型 Violation → `1`、规则命中 → `2`、`out.UnitUnknown` → `4`，按 item 聚合位并拼 `source=… item=… …` 摘要（多来源 `; ` 连接）随 `store.Value` 写入；`exception_count.quality_violation` 计数口径不变 |

## 2. 夹具改造清单（只改数据构造，断言语义未动）

| 文件 / 用例 | 改动 | 断言 |
| --- | --- | --- |
| `helper_test.go` `seed()` | 默认物模型声明从 5 项收窄为 `temp`/`power` | 无断言 |
| `helper_test.go` 新增 `addStdProps()` / `allItems(at)` / `withItems(base,…)` | 需要多点位的用例自行补声明与构造完整报文 | 无断言 |
| `cache_test.go` `TestAdminPropertyUpdateTakesEffectImmediately` | 原「PUT 改 `required=true` 后拒收」→「POST 新增 `hum` 声明后拒收」 | 断言语义不变（管理写路径立即生效 + `missingCodes=[hum]`） |
| `cache_test.go` `TestTTLFallsBackForDirectDBEdit` | 原「直改 `required=true`」→「直接 insert `hum` 声明」 | 断言语义不变（TTL 内仍用旧定义、过期后按新定义拒收） |
| `cache_test.go` | 新增 `adminPost` 辅助、删除随之失效的 `propertyID` 辅助 | 无断言 |
| `batch_test.go` 新增 `TestAllDeclaredPropsRequired` | 4 项缺 hum → 拒收；5 项+1 未声明项 → 接收并计 `clean_failed`；`required=false` 仍必填 | 新用例，覆盖 task-007 §5 第 2 条 |
| `message_test.go` `TestMissingRequired` | 语义从「只报 required 缺失」改为「报全部声明缺失」 | 任务书 §2.2 明确要求 |
| 其余 `batch/cache/quality/settle/time/mqtt/exception/interval` 用例 | **未改**（默认声明收窄后，`temp+power` 报文即完整） | 全部原样通过 |

`admin_test.go`、`capacity_test.go`、`nongoal_test.go`、`dsnhelper_test.go`、`mqtt_test.go` 的 `TestCheckGranted` 均未触碰。

## 3. 验收输出

命令统一前置 `GOCACHE=$PWD/.devcache/gocache`，DSN 为 `postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable`。

```
$ go build ./... && go vet ./...
（无输出）

$ go test ./... -count=1                       # 无 DSN
ok  cmd/server 0.006s | internal/admin 0.009s | internal/clean 0.004s | internal/ingest 0.008s
ok  internal/meta 0.004s | internal/staging 0.003s | internal/ui 0.006s | internal/validate 0.004s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1
ok  cmd/server 0.005s | internal/admin 0.173s | internal/clean 0.003s | internal/ingest 2.470s
ok  internal/meta 0.003s | internal/staging 0.003s | internal/ui 0.005s | internal/validate 0.003s

$ go test ./internal/validate -run TestParseCollectedAt -v -count=1
--- PASS: TestParseCollectedAtAccepts（10 例）/ TestParseCollectedAtRejects（8 例）

$ go test -p 1 ./internal/ingest -run 'TestAllDeclaredPropsRequired|TestQualityFlags' -v -count=1
--- PASS: TestAllDeclaredPropsRequired (0.05s)
--- PASS: TestQualityFlags (0.06s)

$ grep -n "不接受 epoch 数字" internal/validate/message.go docs/design.md
（无命中）
```

验收对照（task-007 §5）：

| 验收项 | 结果 |
| --- | --- |
| `ParseCollectedAt("1700000000")`/`("1700000000123")` 正确，`("1")`/`("9e12")`/`("4102444801")` 拒收 | ✅ accepts 含两者与毫秒上下界；rejects 断言 `早于`/`超出`/`不可解析` |
| 缺 `hum` 的 4 项报文 → `msg_incomplete` + `missingCodes=["hum"]`；5 项 + 1 未声明项 → 接收且 `clean_failed` 计 1 | ✅ `TestAllDeclaredPropsRequired` |
| `quality` 五种取值各 1 例（0/1/2/3/4） | ✅ `TestQualityFlags` |
| 结算后 `hourly_snapshot.quality` 与 staging 一致（非 0 样本 ≥1） | ✅ `TestQualityFlags` §5 逐行比对 |
| `grep "不接受 epoch 数字"` 无命中 | ✅ |
| 无 DSN 全绿 / 有 DSN `-p 1` 全绿 | ✅ 上文输出 |
| diff 不含 `web/**`、`internal/ui/**`、`cmd/server/**`、`internal/admin/**`、`nongoal_test.go` | ✅ |

## 4. 风险处置（对应 task-007 §7）

1. **R-021 上线即全拒**：本次只改了校验与夹具，**未**做上线灰度/真实报文回放——需运维按「先一台、观察 `msg_incomplete`」执行；代码侧没有兼容开关（任务书未要求）。
2. **拒收面扩大 → `exception_count` 飙量**：`msg_incomplete` 计数口径未动，采样仍不在本批；上线后需盯曲线。
3. **`quality` 位语义后续扩展**：已用 `1 << n` 分配并保留高位，`quality_reason` 兜底可读性；新增来源只需加常量与一处 `markQuality`。
4. **加列锁表**：`0005` 用 `add column if not exists … default 0`（PG16 元数据操作）；`hourly_snapshot` 是分区父表，按分区递归执行，实测在测试库上毫秒级完成（分区数量少）。

## 5. 遗留

- `quality` / `quality_reason` 目前只有写入侧与 `select *` 可见，**读路径未暴露**（任务书非目标：不做筛选/UI 列）。
- R-021 的「网关点位未勾满」运维动作不在代码范围，报告已列为风险 1。
