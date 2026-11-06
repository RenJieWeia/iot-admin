# REVIEW-008 · task-007 复核（R-029 / R-021 / R-022）

- `basis:` `rv-11..972b69e`（`8d5f438` / `f9b3109` / `02ac0d2` + 报告）
- `verdict:` **放行**，同意打 `rv-12`
- `method:` 命令复跑 + 定向用例 + 迁移列核对 + 文档表述核对

## 1. 范围

三个提交文件互不重叠（`design.md` 按 §二.3 / §二.4 / §二.14 分小节）：

| 提交 | 决策 | 文件 |
| --- | --- | --- |
| `8d5f438` | R-029 | `validate/{message.go,message_test.go}`、`design.md §二.3` |
| `f9b3109` | R-021 | `validate/{model.go,message_test.go}`、`ingest/{helper,batch,cache}_test.go`、`design.md §二.4` |
| `02ac0d2` | R-022 | `migrations/0005_quality_flag.sql`（新）、`store/value.go`、`latest`、`staging`、`settle`、`ingest/{pipeline.go,quality_test.go}`、`design.md §二.14` |

红线：`web/**`、`internal/ui/**`、`cmd/server/**`、`internal/admin/**`、`nongoal_test.go`、`go.mod`、`go.sum` **零改动**；迁移只新增 `0005`。

## 2. 断言是否被削弱（重点核查）

`git diff` 里确实有断言行被删，逐条核过，**均属场景替换、断言语义未降**：

- `cache_test.go` 两个用例：原「把 `hum.required` 改 true」→ 现「新增 `hum` 声明」。R-021 后 `required` 列不参与校验，原场景已无法触发同一行为；新场景仍验证「管理写路径立即生效」与「直改库最长脏 TTL」，且新增了 `adminPost` helper。**接受**。
- `message_test.go`：`TestMissingRequired` 按 R-021 语义改为「报全部声明缺失」，`TestParseCollectedAt*` 按 v2 裁决调整边界样本。**接受**。
- 其余 ingest 用例断言未动（`Written`/行数/异常计数均保持）。

## 3. 命令复核

```
$ go build ./... && go vet ./...                                  → 干净
$ go test ./internal/validate -run 'TestParseCollectedAt|TestMissingRequired' -v → PASS
$ go test ./internal/ingest -run 'TestAllDeclaredPropsRequired|TestQualityFlags|TestAdminPropertyUpdateTakesEffectImmediately|TestTTLFallsBackForDirectDBEdit' -v（有 DSN）→ 4/4 PASS
$ go test ./...（无 DSN）                                          → 8 包全绿
$ IOT_TEST_DSN=… go test -p 1 ./...                               → 8 包全绿
```

## 4. 数据与文档核对

- `information_schema.columns`：`latest_value` 有 `quality`、`quality_reason`（0005 已应用于 `iot_admin`/`iot_ingest`）。
- `grep -c "不接受 epoch 数字\|只校验 required" docs/design.md` = **0**。
- v2 裁决落实：`ParseCollectedAt` accepts 含 `1700000000`、`1700000000123`、`1577836800`；rejects 覆盖 `1`/`1577836799`/`4102444801`/`9e12`（错误串分「早于/超出/不可解析」三类，用例中已断言）。

## 5. 遗留 / 风险

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | R-021 无兼容开关 | 代码侧不支持「只对某台设备放开」；灰度靠运维（先接一台、真实报文回放）。报告 §4.1 已列 |
| 2 | `quality` 读路径不可见 | `/admin/v1` 与 UI 尚未暴露两列；本轮非目标，下一批开 UI/筛选时再动 `FieldMeta` |
| 3 | 0005 不可逆 | 两个测试库已应用；回滚需手工 `drop column`，属迁移常态 |
| 4 | 拒收面扩大 | R-021 后未勾满点位的设备每条报文全拒，`exception_count` 的 `msg_incomplete` 会涨；采样不在本批 |
