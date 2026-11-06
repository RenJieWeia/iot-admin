# report-010b · 运行时两页只读（staging + snapshot）· 前端

- `task:` task-010 Part B（`status: READY`；A 由 `be-api` 交付，`f8decbb` + `report-010a`）
- `baseline:` `fa1c1f2`（Lead 指定）
- `commit:` `d2b9e80` `feat(ui): 小时暂存与小时快照只读页`（`Base: fa1c1f2`）
- `范围:` `web/**` + `internal/ui/dist`；`internal/admin/**`、`internal/ingest/**` 未改动
- `联调状态:` 后端 A 已落地；本报告的页面证据来自「按真实契约构造的静态 mock + 仓库 `dist`」，真实实例联调由 Lead 复核

## 1. 变更说明

| 落点 | 改动 |
| --- | --- |
| `web/src/api/types.ts` | `ReadonlyMeta.pagination?: 'offset' \| 'cursor'`；新增 `CursorMeta{hasMore,nextCursor,limit}` |
| `web/src/api/client.ts` | 新增 `fetchCursorPage()`：请求 `cursor`/`limit`，响应 meta 只有 `hasMore/nextCursor/limit`，**无 total** |
| `web/src/api/exceptions.ts` | 新增 `useSlotCursorList()`；与 `useSlotList()` 共用 `SlotSource` |
| `web/src/lib/queryState.ts` | QueryState 增加 `cursor`；URL 序列化/解析；条件（q/filters/sort/order/pageSize）变化时**同时清空 page 与 cursor** |
| `web/src/lib/presets.ts`（新） | 快捷开关的唯一定义：`quality ≥ 1`（仅看有问题）、`revision ≥ 1`（仅看被修正过）；按字段存在 + 支持 gte 才渲染 |
| `web/src/lib/format.ts` | `revision`：0/空 → `—`，>0 → 数字且 `title="已修正 N 次"` |
| `web/src/components/CursorPager.tsx`（新） | 「加载更多」+ `hasMore=false` 时隐藏按钮并提示「已到末尾」；显示「已加载 N 条 · 每批 M 条（游标分页，不显示总数）」 |
| `web/src/components/DataTable.tsx` | 新增 `sortable` 开关：关闭时不渲染排序按钮，仅保留默认排序列的静态方向（`aria-sort=descending` + `↓`） |
| `web/src/pages/ReadonlySlotPage.tsx` | 按 `pagination` 分支 offset/cursor；preset 由 `lib/presets.ts` 驱动（替换原先内联的 quality 逻辑） |
| `web/src/test/{fixtures,server}.ts` | 三项 `_meta.readonly`；staging 25 行、snapshot 45 行（每 4 行 1 行 `revision>0`）；MSW 增加 cursor 处理器（非法 cursor → 400） |

**字段顺序按后端实测对齐**（`hour_start` 打头，`revision` 在 `settled_at` 之前），快照 11 列。

**排序入口**：快照页后端固定 `settled_at desc`，传 `sort`/`order` 会 400 —— 因此该页不渲染任何排序按钮，只在「结算时间」表头显示静态 `↓` 与 `aria-sort=descending`。

## 2. 规范覆盖（`admin-ui-spec` / `table-query`）

| 条款 | 落点 |
| --- | --- |
| 通用 §6 布局稳定性 | 列宽 `widthStrategy`；表格容器内横向滚动（9/11 列）；`revision` 数字等宽 |
| 通用 §8 状态保持 | `q/filters/sort/page/pageSize/cursor` 全进 URL；游标刷新可复原 |
| 通用 §11 筛选与空结果 | 常驻 ≤5 + 「更多筛选」；条件标签常驻、可单删/全清；preset 与标签双向同步 |
| 通用 §16 语义 | `quality`/`revision` 文字 + `title`；时间绝对 `+08:00` |
| 通用 §17 可访问性 | `aria-sort`、`scope="col"`、`<label for>`、开关有可见文本 |
| `table-query` §6 分页 | offset：页码 + 总数 + 每页档位；cursor：加载更多 + `hasMore`，**不显示总数**，不提供跳页 |
| `table-query` §7 排序 | 白名单 + 服务端默认排序；**不可排序页不渲染排序入口**（避免点了 400） |
| `table-query` §8 / §9 | 仍整体豁免（R-018） |

## 3. 测试（实测）

```
$ cd web && npx vitest run
 Test Files  8 passed (8)
      Tests  50 passed (50)

$ npx tsc --noEmit               → 无输出
$ npx stylelint "src/**/*.css"   → 无输出，exit 0
$ npm run build && git diff --exit-code internal/ui/dist → exit 0
$ 两次构建 sha256 逐文件比对      → 完全一致
```

新增 9 条用例（`web/src/pages/RuntimeReadonly.test.tsx`）：

| 用例 | 断言 |
| --- | --- |
| staging 页码分页 | `第 1–10 条，共 25 条`；下一页 → `第 11–20 条`、URL `page=2`；**不出现「加载更多」** |
| snapshot 首批 | 20 行；`已加载 20 条 · 每批 20 条（游标分页，不显示总数）`；「加载更多」存在；**整页无「共 N 条」**、无「下一页」 |
| 加载更多 + hasMore 边界 | 点击后 URL 含 `cursor=off…`；第二/三批与上一批**无重复行**；第三批 5 行、`hasMore=false` → 按钮消失并显示「已到末尾」 |
| 无排序入口 | 快照页没有 `按…排序` 按钮；`结算时间` 表头 `aria-sort=descending` 且带 `↓` |
| revision 徽标 | `title="已修正 1 次"` 存在；`—` 存在（`revision=0` 行） |
| 仅看被修正过 | 勾选 → URL `filter.revision.gte=1`；45→12 行；标签「修正次数 ≥ 1」 |
| 翻页不重置筛选 | `revision.gte` 与 `quality.gte` 同时在 URL 中共存 |
| 只读约束 | 两页均无「新建/编辑/删除」、`[data-action]` 0、`role=dialog` 0、`writeCalls` 0 |
| 导航顺序 | 「数据」分组严格为 最新值 → 小时暂存 → 小时快照（与 `_meta.readonly` 顺序一致） |

## 4. 浏览器层证据（CDP，1440×1200，`Emulation.setDeviceMetricsOverride`）

环境：静态 mock（`_meta` 取 MSW fixture 的 JSON、staging 25 行、snapshot 45 行且对 `sort`/`order` 返回 400）+ 仓库 `dist`。

| 项 | 小时暂存 | 小时快照 |
| --- | --- | --- |
| `main.x` | 200 | 200 |
| `main.w` | **1240**（无滚动条，`docHeight=1200`） | **1225** + 15px 滚动条 = 内容宽 1240（20 行，`docHeight=1434`） |
| 表头 | 9 列，8 个排序按钮 | 11 列，**0 个排序按钮**；`结算时间 ↓`、`aria-sort=descending` |
| 分页区 | `第 1–10 条，共 25 条` | `已加载 20 条 · 每批 20 条（游标分页，不显示总数）` + 「加载更多」 |
| 写入口 / 抽屉 | `[data-action]` 0 / dialog 0 | 同左 |
| 加载更多 | — | URL `?cursor=off%3A20`，行数仍 20（下一批） |
| 预设 | — | 勾「仅看被修正过」→ `?filter.revision.gte=1`，`已加载 12 条`，标签「修正次数≥1」 |
| console / CSP | 0 / 0 | 0 / 0 |
| 写请求 | 0 条 | 0 条 |

补充：把快照批大小调到 5 行（贴近开发库现状）后同视口实测 `main.x=200 / w=1240 / scrollbar=0` —— 说明 1225 完全由行数引起的 15px 滚动条造成，布局列宽仍是 `200px + 1240px`。

截图：`.devcache/shots/task010b-snapshot.png`。

## 5. 假设与偏离

| 类型 | 内容 |
| --- | --- |
| Assumption | preset 的字段键与阈值（`quality`/`revision` ≥ 1）取自 task-009 §2 与 task-010 §4.3，集中定义在 `lib/presets.ts`；页面只在字段存在且支持 `gte` 时渲染 |
| Assumption | `cursor` 变化不触发「回第一页」语义（它本身就是翻页），但条件变化会清空 `cursor`，两者由 `patchQuery` 统一处理 |
| Deviation | 无（`exceptions` 段与既有两页未改；`latest-values` 行为未变） |
| Open-Q | 无 |

## 6. 风险

| 风险 | 说明 |
| --- | --- |
| 游标页行数 | 单批条数由后端 `limit` 决定（默认 200），行多时页面必然滚动；1440×1200 下 `main.w` 会因 15px 滚动条显示 1225。判据建议用「`main.w + 滚动条宽 == 视口宽 - 侧栏宽`」 |
| 游标不可跳页 | 这是分区表上的设计取舍（无 total），用户只能顺序「加载更多」；若后续要跳页需另立任务 |
| `revision` 展示规则写死字段键 | 与 preset 同源，集中一处；若字段改名需同步 |
| mock 与真实数据差异 | 本报告 CDP 用 45 行合成数据；真实实例只有 1 行 `revision=1`，联调时需复核「已修正」徽标与 `revision.gte=1` 过滤各 1 行 |
