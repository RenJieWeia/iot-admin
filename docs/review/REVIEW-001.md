# REVIEW-001 · 前端（React SPA）预审 · MODE: REVIEW

- `basis:` `rv-06`（5dabe0d）+ 工作区未提交的 `web/**`、`internal/ui/dist`（fe-web 步 2–5 进行中）
- `reviewer:` Lead（只读；未改业务代码、未跑 git 写操作）
- `spec:` `admin-ui-spec`（通用 §2/§5–§8/§11/§13/§16–§18 + `table-query` §2–§7/§11/§13）
- `verdict:` **后端契约可放行**；**前端暂不放行**（WIP，四态/抽屉/异常页断言未齐）

## 1. 后端契约（`8208525`/`d117983`/`03ca27e`）

复核结果：与 task-004 §4 一致 —— `title`（不再下发 `label`）、顶层 `enums`/`booleans`、关联字段 `ref{entity,valueKey,labelKey}`、`GET /admin/v1/options/{entity}`（上限 1000、未知实体/异常页 404）、`exceptions/{slot}` 复用 QueryState + `meta`；`admin.Entities` 仍 6，`nongoal_test.go`、`go.mod`、`migrations/` 零改动。**无阻断项。**

建议（不阻断）：`_meta` 同时下发 `operatorLabels`/`pagination`/`defaultSort` 属附加字段，前端已消费，保留即可；后续若改 `_meta` 形状需按编号重新裁决，不得静默变更键名。

## 2. 前端工作区（`web/**`）发现

| # | 位置 | 问题 | 违反条款 | 建议 | 严重度 |
|---|---|---|---|---|---|
| F1 | `web/src/router.tsx:12`、`:20` | 用内联 `style={{ padding: 'var(--space-6)' }}` 承载间距，且加载态是一个裸 `<div>` 文本「正在加载…」 | 通用 §2（令牌集中）、§6（骨架与真实行高一致）、§18（禁止项：样式散落） | 改成 `AppShell` + `StatePanel`/`Skeleton`；间距回归 CSS Modules | 重要 |
| F2 | `web/src/lib/queryState.ts:146-154` | `setSort` 里 `field === defaultSort` 分支与末尾 `return` 完全等价，注释声称「保留 URL 精简」，实现却仍写入 `sort=id` | 通用 §16（一词一义/注释与实现一致）、可维护性 | 删掉死分支；或真正在等于默认值时省略 `sort`，二选一并同步注释 | 建议 |
| F3 | `web/src/styles/tokens.test.ts:70` | 间距网格显式放过 `1px/2px`，与 task-004 §5「间距 4px 网格」有偏离 | task-004 §5、通用 §2 | 保留例外可以，但**必须在交付报告里声明豁免理由**（边框/视觉隐藏），否则按未申报偏离处理 | 重要 |
| F4 | `web/src/App.tsx:12-20` | QueryClient 只配 `retry: 1`，`invalid_query`（400）也会重试一次，白屏/噪声风险 | 通用 §13（网络异常）、§16（错误文案含原因与恢复路径） | `retry` 改为对 4xx 不重试（`ApiError && status < 500`），5xx/网络错误才重试 | 建议 |
| F5 | 待最终审——`web/src/components/DataTable.tsx`、`FilterBar.tsx`、`Drawer.tsx`、`pages/*.tsx` | 需逐条验证：`table-layout: fixed` + `widthStrategy`、单元格 `title` 截断、四态文案互不相同、抽屉 `role="dialog"`+Esc+焦点回归、异常页无写请求、`aria-current` | `table-query` §3/§5/§6/§7/§11、通用 §5/§8/§11/§17 | 用 RTL 断言 + 手验（见 §3） | 需确认 |
| F6 | 待最终审——`web/src/api/meta.ts:22-42` | `enums` 按 `field.key` 查字典（`payload.enums?.[field.key]`），而 `_meta` 下发的字典键是 `value_type`/`rule_type`，键名恰好等于字段 key；若后续出现与字段 key 不同名的枚举字典会静默丢标签 | 通用 §16（状态文案唯一来源） | 在类型里加断言/回退：`enums[field.key] ?? undefined` 并在测试里锁住（已有 `normalizeMeta` 用例则补一条反例） | 建议 |

无颜色字面量外泄：`tokens.css` 之外的颜色检查已由 `tokens.test.ts` 与 stylelint 双覆盖，通过。

## 3. 最终审核清单（task-004 §8 九条 → 可执行断言）

| 验收项 | 断言方式 |
| --- | --- |
| 产物入仓、无 node 也能构建 | `git ls-files internal/ui/dist/index.html` 非空；`env -u NODE_PATH PATH=/usr/bin:/bin GOCACHE=/tmp/gocache go build ./... && go test ./...` |
| 产物可复现 | `cd web && npm ci && npm run build && git diff --exit-code ../internal/ui/dist`（退出码 0） |
| `_meta` 覆盖 6+2、未知 slot 404、options 上限 1000 | `go test ./internal/admin/...` + `curl /admin/v1/_meta`、`/admin/v1/exceptions/nope`、插 1001 行实测 |
| QueryState ↔ URL、回第一页、标签、四态文案互不相同 | `cd web && npx vitest run`（4 条断言逐条对应用例名） |
| 抽屉 `role="dialog"` + Esc + 焦点回归 | RTL 用例（`Drawer`/`EntityListPage.test.tsx`） |
| 列表只渲染服务端返回的 N 行 | RTL：mock 10 行/总数 128 → DOM 行数 10 |
| `:root` 之外无颜色 | `npx stylelint "src/**/*.css"` + `npx vitest run src/styles/tokens.test.ts` |
| 8 个页面可访问、刷新/后退保留状态 | 手验（`/ui/terminals` 等 8 条 + 刷新 + 后退） |
| 有无 DSN 两套 Go 测试全绿 | `go test ./...` 各跑一遍；DSN 用例**单进程**跑（Q-005） |

附加（本审核自加，均来自规范硬约束）：

- §18 禁止清单逐条 grep：`!important`、`rgb(`/`hsl(`、脉冲骨架（`animation` + 骨架）、相对时间（`刚刚|分钟前`）、"暂无数据"通用空态文案。
- `table-query` §2：查询带 `AbortSignal`（已核 `pages/EntityListPage.tsx:66`、`api/entities.ts:13` 均有）；无本地 `filter()/sort()/slice()` 冒充全局查询 —— 需 grep 确认。
- `table-query` §5：已生效条件常驻标签、可单删/全清；>5 条进「更多筛选」但条件不外显即阻断。
- 通用 §17：`<table>` + `scope`、`<label for>`/可访问名、抽屉焦点陷阱。
- §10 禁止项：`node_modules`、`.vite`、sourcemap 不入库。

## 4. 审核规则声明（本轮及最终审有效）

1. 只读：除 `docs/review/` 外不改文件、不 commit/tag/push；发现写进本文，不直接改前端代码。
2. 以 diff 为准，commit trailer 仅作线索；未申报偏离（`Deviation` 缺失）按阻断处理。
3. 阻断项按根因合并，单轮 ≤5；每条给 `文件:行号` + 条款号 + 一条可执行修改。
4. 放行门槛：task-004 §8 九条全绿 + 本文 §3 附加项无阻断 + 上述 F1/F3 已处理或已书面豁免。
