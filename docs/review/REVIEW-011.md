# REVIEW-011 · rv-15 复核（task-010 运行时两页只读，A+B）

- `basis:` `rv-14..0654911`：A = `f8decbb`+`fa1c1f2`；B = `d2b9e80`+`0654911`
- `verdict:` **放行**，打 `rv-15`
- `method:` 干净 worktree 跑测试 + 真机 curl + 无头 Chromium CDP（1440×1200，真实后端非 MSW）

## 1. Part A 复核（已在前一轮完成）

| 项 | 结果 |
| --- | --- |
| 干净 worktree `fa1c1f2` | 树干净、build/vet 干净、10 条定向用例 PASS、无 DSN 8 包、有 DSN `-p 1` 8 包 |
| `_meta` | `entities=6`、`exceptions=2`、`readonly=3`，`pagination` = `offset/offset/cursor` |
| staging | `meta.total` 存在（当前 0 行，环境事实） |
| snapshot | `meta={hasMore,nextCursor,limit}`，**无 total**；非法 cursor → 400；传 `sort` → 400；`limit=5000` → 1000 |
| 游标回放 | 同一 `nextCursor` 连打两次结果完全一致 |
| `revision.gte=1` | 命中种子行 `T1/power revision=1` |

## 2. Part B 复核

| 项 | 结果 |
| --- | --- |
| 范围 | `fa1c1f2..HEAD` 仅 `web/**`、`internal/ui/dist/**`、`docs/review/report-010b.md`，无越界 |
| 前端用例 | **8 files / 50 tests passed**；`tsc`/`stylelint` 干净 |
| 可复现 | `npm run build && git diff --exit-code internal/ui/dist` = 0 |
| 分页分支 | offset 页沿用页码；cursor 页「加载更多」+ `hasMore`，无总数 |
| 排序入口 | 快照页 `thead button` = **0**，仅「落库时间」带 `aria-sort="descending"` |

## 3. A/B 联调（真实后端 + CDP 1440×1200）

**小时暂存页** `/ui/readonly/hourly-staging`
- `main.x=200 / w=1240`、无滚动条；0 行 → **「暂无小时暂存」空态**（非错误态）；0 个排序入口。

**小时快照页** `/ui/readonly/hourly-snapshot`
- 11 列，表头顺序 `归属小时 / 终端 / 数据项 / 数值 / 文本值 / 单位 / 采集时间 / 质量 / 质量说明 / 修正次数 / 落库时间 ↓`；
- `revision` 渲染：`power` → **「已修正 1 次」**、`temp` → 「未修正」；
- 分页文案：`已加载 2 条 · 每批 200 条（游标分页，不显示总数）` + **「已到末尾」**；
- 勾「仅看被修正过」→ URL `?filter.revision.gte=1`、条件标签 `修正次数≥1`、行数 1；
- `main.x=200 / w=1240`（2 行无滚动条）、写入口 0、写请求 0、console/CSP 0 违规。

导航顺序：`最新值 → 小时暂存 → 小时快照`（与 `_meta.readonly` 一致），异常两页在其后。

截图：`.devcache/shots/task010-staging.png`、`.devcache/shots/task010-snapshot.png`。

## 4. 非阻断建议

| # | 项 | 说明 |
| --- | --- | --- |
| 1 | 快照页批大小不可调 | 后端 `limit` 默认 200、上限 1000，前端只展示「每批 200 条」，无选择器。建议后续给 cursor 页加 20/50/200 档位（`table-query` §6 允许），当前不影响正确性 |
| 2 | `revision=0` 的文案 | 实现为「未修正」（比书里写的「—」更清楚），采纳实现 |
| 3 | 开发库 `hourly-staging` 为空 | 需先发窗口内报文才有数据；空态已验证正确，不是缺陷 |

## 5. 结论

两页在真实后端下行为与契约一致，游标分页、`hasMore`、`revision` 徽标、零写入口全部成立。**打 `rv-15`**。

遗留：`task-010` 非目标项（导出、图表、快照写入口）保持不做；批大小档位进下一轮打磨清单。
