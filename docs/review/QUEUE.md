# 未决问题队列

`Q-001` 起严格递增，永不复用。双向：执行者提问，审核者回答。

| ID | 提问方 | 问题 | blocks | 状态 | 回答 | 闭环于 |
|---|---|---|---|---|---|---|
| Q-001 | implementer | `docs/review/task-001.md` 不存在，无执行计划可产出 | 全部步骤 | ANSWERED | 由执行者按 `docs/design.md` §6.1 + §五.8 起草草案，用户确认后转 SPEC | 2026-11-05 |
| Q-002 | implementer | task-001 §4 只列 `internal/ingest/helper_test.go` 改装配，但 `capacity_test.go:49`、`mqtt_test.go:51` 也自建 `ingest.Processor{Reg: validate.NewRegistry(pool)}`；`Reg` 换类型后这三处必须同步改才能编译。是否放行这两个未列名测试文件（仅构造，不动断言）？ | 步骤 2 | ANSWERED | 放行：`Processor` 字段换类型属于编译期硬依赖，两个文件只允许改构造行。事实核对：`95982d4` 已按此落地（各 +1 行 import、改 1 行 `Meta:` 装配），无断言改动；task-001 §4 的文件清单属漏列，下一版任务书补入，不构成偏离 | 2026-11-05 |
| Q-003 | implementer | 缓存命中期间 DB 不可用不再整包 503：`TestPlatformFailureReturns503` 先用一条正常消息预热缓存，关池后元数据不再访问库，故障推迟到写路径返回 `cache_write_failed` + 200。已在关池后补一次 `Meta.Flush()` 保留 503 断言（仅 setup，断言未动）。①「元数据命中期间 DB 故障」是否仍需整包 503？②若要，只能在 `http.go`/`pipeline.go` 增加「写失败即 503」口径，属 task-001 §3 非目标，需放行。 | 步骤 4 | OPEN | | |

状态：`OPEN` / `ANSWERED` / `CLOSED` / `BLOCKED`
`BLOCKED` = 缺上游事实 → 转 `UNKNOWN.md` 立案。
答复后由 implementer 在下一提交 trailer 的 `Answer: Q-00N: 结论` 闭环。

## 台账说明

- 初始化时清除模板示例行 `Q-001（示例）ANSWERED` 与空行 `Q-002`，未消耗任何编号。
- 后续真实提问自 `Q-002` 起。
