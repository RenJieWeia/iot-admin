# REVIEW-014 · rv-17 复核（task-012 + task-013 同批）

- `basis:` `rv-16..7d8c9f9`（task-012：`55c879d` 后端 + `12a52e3`/`676b0f3`/`bae9811`+`1d99422`/`199dd76`/`7d8c9f9` 前端；task-013：`d3d61e9`/`dc47438`/`abbf904`/`c8e6c9e`）
- `verdict:` **放行**，打 `rv-17`
- `method:` 干净 worktree 双套测试 + 真机 HTTP/SQL 端到端 + CDP + Makefile 篡改试验

## 1. 范围与红线

| 检查 | 结果 |
| --- | --- |
| 干净 worktree `7d8c9f9` | `git status` 空；build/vet 无输出；无 DSN 8 包、有 DSN `-p 1` 8 包全绿 |
| `nongoal_test.go` | **只 +1 行白名单**（`snapshot_cell_history`），禁词循环与其它断言一字未动 |
| 表名 | 按 Lead 裁决由 `snapshot_version` 改为 `snapshot_cell_history`；`grep snapshot_version` 在 `internal/`、`migrations/` = **0**（残留 5 处全在 docs 的改名说明里） |

## 2. task-012 验收

**F4 后端**（真机）
```
q=T1        → [{"value":"T1","label":"T1"}]
ids=T1,T2   → 2 项且顺序稳定
ids+q       → 400
ids 201 个  → 400
```

**F2 分页越界**（CDP，`/ui/terminals?page=9`）
URL 清空、出现「回到第一页」提示、`tbody` 4 行、`caption=共 4 条，第 1 页`——一次修正，无循环。

**F3 caption**
`共 4 条，第 1 页`；`grep "由服务端执行" web/src` = 0。

**F4 前端 combobox**
抽屉里 `[role=combobox]` 可展开 → `[role=listbox]` + `[role=option]` 渲染「电表」；网络记录出现 `GET /admin/v1/options/device-types?limit=50`。

**F5 Makefile**（主树实测）
```
make web-check   → 9 files / 61 tests passed
make dist-check  → 干净树 0
                  改 AppShell.tsx 文案后 2（检测到产物漂移）
                  还原后 0
make go-check    → 0（无 DSN 8 包）
```
只做校验、不 `git add`。

## 3. task-013 验收（真机 HTTP + SQL）

| 场景 | 结果 |
| --- | --- |
| `msgType=2` + 30 天前 → 补录 | `accepted=true, written=5, published=false`；快照 5 行；`snapshot_cell_history` **+5 行**，`source=1`、`revision=0` |
| 实时（无 `msgType`） | `accepted=true, written=5, published=true`；**版本表 0 新增**（5 → 5） |
| 再补一条更早 `value_time` | 幂等跳过，版本表不变（5 → 5） |
| 再补一条更晚 `value_time` | 版本表 +5（5 → 10） |
| `late_data` 事件 | 保留（`exception_count` 有计数），但 `results[].reason` 不再回 `late_data` |

## 4. 口径变化（影响外部，必须在交付说明里）

1. `results[].written` 变大：窗口外补录从 `0` 变为实际写入行数（每项 1 行）。
2. 窗口外不再回 `accepted=false + late_data`，改为 `accepted=true / reason=""`；迟到只存在于 `exception_count.late_data` 事件（detail 带 `source=`/`msgType=`）。下游若靠 `accepted=false` 识别迟到需改看事件表。

## 5. 非阻断发现

| # | 项 | 处置 |
| --- | --- | --- |
| 1 | `bae9811` 是**红的中间提交**（`user.type(..., {delay})` 不存在、tsc 不过、产物未重建），由 `1d99422` 修正 | **不改写历史**（区间里混着 `be-api` 的提交，rebase 会改写他人 hash）。后续规则：提交前必须 `tsc` 过；`--amend` 只在“干净 index + 该提交是 tip”时用 |
| 2 | `docs/design.md` 的 task-013 文案落进了 `fe-web` 的 `199dd76`（归属错位） | 内容在 HEAD 上正确，只记归属；不改写历史 |
| 3 | `be-api` 的 `Base:` trailer 写成 `rv-16`（应为 `9b95ff6`） | trailer 不准确，无内容影响；下次按“上一本收口提交”填 |
| 4 | F3 的「空列表 `共 0 条`」实际走 `EmptyState`、不渲染表格，故不可达 | **接受**：空/无匹配用状态面板更清楚；caption 只在表格存在时生效，书里的表述以本条为准 |
| 5 | `make go-check` 会扫到 `web/node_modules/flatted/golang/pkg/flatted` | 建议 `go-check` 收窄为 `./cmd/... ./internal/...`，或给 `web/` 加 `go.mod` 阻断遍历 |

## 6. 结论

两本的行为验收与红线全部成立（含版本表只在补录时写入、同事务、幂等跳过三条硬约束）。**打 `rv-17`**。外部影响仅 §4 两条，已要求写进交付说明。
