# report-004 · 管理后台 UI 改写为 React SPA

- `task:` task-004（`status: READY`）
- `baseline:` rv-06（`5dabe0d`，含 `03ca27e` 契约最终版 + `b82a788` 写失败 503）
- `tag:` 未打（按 dev-loop，由审核方在审后打 `rv-07`）
- `commits:` 本报告前共 9 个提交，逐一可单独 revert
- `red-lines:` `go.mod`/`go.sum`/`migrations/`/`internal/ingest/nongoal_test.go` **零变化**；改动路径全部落在 `web/**`、`internal/ui/**`、`cmd/server/**`、`docs/design.md`

## 1. 变更说明（按 task-004 §7 步骤）

| 步 | 提交 | 内容 |
| --- | --- | --- |
| 1 | （`be-api`，rv-06 `03ca27e`） | `_meta`/`exceptions`/`options` 三个端点；本任务未改 `internal/admin/**` |
| 2 | `8dcdadf` `f393a21` `f569e76` `22fcb21` | `web/` 脚手架（Vite 6 + TS + React 19，`base:/ui/`、`outDir=../internal/ui/dist`、`emptyOutDir`）；令牌迁移；API 客户端（`_meta`/列表/`options`/异常/CRUD）；QueryState ↔ URL |
| 3 | `b0cdbe9` `572c895` | 元数据驱动列表：筛选/排序/分页/四态/条件标签；`terminals` 与其余 5 实体同一条路由 |
| 4 | `b0cdbe9` `572c895` | 抽屉增删改（Radix Dialog + 关系选择器 + 删除二次确认 + 写后就地刷新） |
| 5 | `572c895` | 异常事件/异常区间只读两页（无写入口、无写请求） |
| 6 | `f3bcc13` `0671391` | `//go:embed all:dist` + SPA fallback；删除 SSR 资产与旧测试；`main.go` 挂 SPA + CSP；`design.md` §一/§七 同步 |
| 测试 | `7f83b7a` | Vitest + RTL + MSW 假后端（24 用例） |

目录落点：

- `web/src/api/`：`types.ts`（§4 契约逐字映射）、`client.ts`（统一信封 + `ApiError` + `filter.<col>[.<op>]` 序列化 + AbortSignal）、`meta.ts`（`enums`/`booleans` 归一到字段 `options`）、`entities.ts`（`options/{entity}`）、`exceptions.ts`
- `web/src/lib/`：`queryState.ts`（URL 唯一事实源、回第一页规则）、`fieldMeta.ts`（可见列/常速筛选/默认排序/`ref` 目标）、`format.ts`（绝对时间 `+08:00`、状态标签、空值 `—`）、`scroll.ts`
- `web/src/components/`：`AppShell` `DataTable` `FilterBar`+`ActiveFilterChips` `Pagination` `Drawer` `ConfirmDialog` `RecordDrawer` `RelationSelect` `StatePanel` `EmptyState` `ErrorState` `Skeleton` `StatusTag` `Toast`
- `web/src/pages/`：`EntityListPage`（6 实体共用，`readOnly` 决定写入口）、`ExceptionListPage`（只读）
- `internal/ui/`：`embed.go` + `spa.go`（fallback / 缓存头 / 405）+ `dist/**`

## 2. 规范覆盖清单（`admin-ui-spec`）

### 2.1 通用规范

| 条款 | 落点 |
| --- | --- |
| §2 设计令牌 | `web/src/styles/tokens.css` 的 `:root`；其余 CSS 只允许 `var(--…)`；stylelint `color-no-hex`（`tokens.css` override 豁免）+ 自定义 Vitest 断言 |
| §3 布局与导航 | `AppShell`：左侧固定导航（6 实体 + 异常两页，文案来自 `_meta`）、skip-link、1024px 单断点 |
| §4 页面模式与信息分层 | 列表只放可见列；`visible:false` 字段（`precision`/`params`/`sample_digest`）只进抽屉；异常页只读 |
| §5 组件 | 主按钮 ≤1（新建 / 保存）；浮层都有标题；抽屉底部固定「取消 + 保存」；删除用 `alertdialog` 且确认按钮含对象名 |
| §6 布局稳定性 | `table-layout: fixed` + `<colgroup>` 类名按 `widthStrategy`；单元格单行截断 + `title`；骨架行高 = 真实行高、无脉冲动画；分页/总数固定 `min-width` |
| §7 跨页面交互 | 编辑用侧滑抽屉（`?drawer=<id>`）不跳页；关闭后 URL 与滚动位置还原 |
| §8 列表与状态保持 | 筛选/排序/分页全进 URL；`sessionStorage` 按地址记忆滚动；写成功 `invalidateQueries` 就地刷新 + 轻提示 |
| §11 搜索与空结果 | 关键词 300ms 防抖 + 回车立即；常驻筛选 ≤5 + 「更多筛选」；已生效条件常驻标签（单删/全清）；无数据/无匹配两态分列 |
| §13 网络异常 | 列表失败态含后端 detail + 重试（+ 清除筛选）；抽屉写失败就地显示原因且不关闭 |
| §14 键盘与基础可用 | Esc 关闭浮层、焦点回触发按钮、触摸目标 ≥44px、1024px+ 布局正确 |
| §15 通知 | 成功/失败用 `aria-live` 轻提示，不打断阅读 |
| §16 语义规范 | 状态 = 文字 + 颜色；时间绝对时间 `+08:00`；错误含原因与恢复路径 |
| §17 可访问性 | `<table>` + `scope="col"` + `aria-sort`；`<label for>`；`role="dialog"`；`role="alertdialog"`；`aria-live`；`aria-current` 导航 |
| §18 禁止清单 | 无颜色字面量（tokens 除外）、无内联样式、无 `!important`、无脉冲 div、筛选进 URL、空态不复用「暂无数据」、无相对时间、表格不压列宽 |

### 2.2 table-query 子规范（§2–§8、§13 重点）

| 条款 | 落点 / 证据 |
| --- | --- |
| §1 核心原则 | 唯一 QueryState，服务端全局查询；表格只渲染服务端返回的当前页（RTL：mock 10 行 / total 128 → `<tbody>` 10 行） |
| §2 QueryState | 结构化 filters（`field/op/values`）+ `q/sort/order/page/pageSize` 全量进 URL；变化回第一页（用例 `筛选/关键词/排序/pageSize 变化回第一页`）；`AbortSignal` 取消 |
| §3 字段元数据 | 列/中文/枚举/`widthStrategy`/`ref`/`operatorLabels` 全部来自 `_meta`，页面零硬编码 |
| §4 类型映射 | 枚举多选 `in`、文本 `contains/eq/prefix`、数值 `gte/lte`、布尔三态 `eq`、关联 `in`（取 `options` 端点） |
| §5 筛选区 UI | 常驻 ≤5、其余进「更多筛选」抽屉；条件常驻标签，可单删/全清（RTL：标签单删后 URL 去掉条件并回第一页） |
| §6 分页 | 默认 10、档位 `pagination.options`（10/20/50）、上限 50、条件或 pageSize 变化回第一页、显示范围与总数、超范围提示保留筛选 |
| §7 排序 | 白名单 + `defaultSort`/`defaultOrder` + 唯一 tie-breaker（后端）；不可排序列无入口；方向标记 ↑/↓ |
| §9 列展示 | `widthStrategy` 决定列宽（类名，非内联样式）、横向滚动不压列宽、单行截断 + `title` |
| §10 状态保持 | URL 恢复筛选/排序/分页 + 滚动位置恢复（`scroll.ts`） |
| §11 空/错/可访问性 | 四态区分、控件有标签、Esc、颜色非唯一载体 |
| §12 后端约束 | 字段/操作符白名单、LIKE 转义、参数化 SQL（后端既有，前端只用白名单参数） |
| §13 验收清单 | 1 ✅ 3 ✅ 4 ✅ 5 ✅ 7 ✅ 10 ✅；2 部分（原生 multiple select）、6/8/9/11/12 豁免或 N/A（见 2.3） |

### 2.3 豁免与未覆盖（逐条给理由）

| 条款 | 状态 | 理由 |
| --- | --- | --- |
| `table-query` §8 全选与批量（L1/L2/L3） | **整体豁免** | R-018 只做等价重构，不做批量；表格无选择列、无批量状态栏 |
| `table-query` §9 导出 | 豁免 | 非目标（R-018 / task-004 §3） |
| `table-query` §6 深分页/cursor | 豁免 | 管理后台数据量小，offset + 上限 50 足够 |
| `table-query` §13.2 多选「全选当前选项 / 已选计数」 | 部分实现 | 沿用 rv-05 的原生 `multiple` select（可键盘操作、已选高亮），未做自定义全选与计数徽标 |
| `table-query` §13.8 1000+ 条 / 深分页 / count 超时 | 未覆盖 | 无容量项（非目标）；`meta.total` 与列表共用 where（后端保证） |
| `table-query` §10.6 实时更新冲突 | N/A | 无 SSE/实时推送（R-013） |
| 通用 §9 全选三态 / 状态栏 | 豁免 | 同上，无批量 |
| 通用 §12 长任务与离开回来 | N/A | 无长任务 |
| 通用 §14 移动端专项（筛选抽屉、底部选择栏） | 豁免 | task-004 §3：只保证 1024px+ 与 44px 触摸目标 |
| 通用 §13 认证/权限 | 豁免 | R-011 `DEFAULT`：`AllowAll` + 仅 127.0.0.1 |
| **间距 4px 网格的例外**（`tokens.test.ts`） | 显式豁免 | 仅允许 `0`、`±1px`、`2px`：1px 是边框、2px 是 focus 描边、-1px 是 `.visually-hidden` 的标准 clip 写法；其余 padding/margin/gap 的 px 值必须是 4 的倍数（F3 处置） |

## 3. 测试结果（实测命令与输出）

### 3.1 前端

```
$ cd web && npx vitest run
 Test Files  4 passed (4)
      Tests  24 passed (24)
   Duration  4.98s

$ npx stylelint "src/**/*.css"
（无输出，exit 0）

$ npx tsc --noEmit
（无输出，exit 0）
```

用例清单（对齐 §8 与 REVIEW-001）：

| 用例 | 断言 |
| --- | --- |
| `只渲染服务端返回的当前页 10 行，总数 128 不落成 DOM` | `<tbody>` 恰 10 行；页脚「第 1–10 条，共 128 条」 |
| `四态文案互不相同` | 加载「正在加载列表…」/ 无数据「还没有终端」/ 无匹配「没有匹配的记录」/ 失败「列表加载失败」→ `Set.size === 4`；失败态含 `db_unavailable` 与「重试」 |
| `已生效筛选以标签外显，可单删并回第一页` | 标签含 字段/操作符/中文值；单删后 URL 无 `filter.enabled.eq` 且 `page` 被清 |
| `翻页写 URL 并请求下一页，不重置筛选` | URL 出现 `page=2`，页脚变「第 11–20 条」 |
| `抽屉：role=dialog、Esc 关闭、焦点回到触发按钮` | `role=dialog`、标题「编辑终端」、底部「取消/保存」、Esc 关闭、焦点回「编辑」 |
| `新建保存后关闭抽屉、刷新列表并给轻提示` | POST body 含 `id/name`；抽屉关闭；出现「已创建记录，列表已刷新。」 |
| `删除走二次确认，确认按钮含对象标识` | 先 `alertdialog`、确认按钮「删除 终端 1（t-001）」、确认前零写请求、确认后 DELETE `terminals/t-001` |
| `写失败就地提示原因，抽屉不关闭` | 显示「保存失败：column "name" too long」，`role=dialog` 仍在 |
| 异常两页 ×4 | 枚举中文来自 `_meta.enums`；无「新建/编辑/删除」；`writeCalls.length === 0`；未知 slot 给原因 + 返回路径 |
| `queryState.test.ts` ×8 | 编码往返（多值重复参数）、归一（page/pageSize）、回第一页、排序切换、单删/全清 |

### 3.2 构建可复现（§8 加码项）

```
$ cd web && rm -rf node_modules && npm ci && npm run build
✓ built in 2.86s
$ cd .. && git diff --exit-code internal/ui/dist
[git diff exit=0]
```

### 3.3 Go（无 DSN，`GOCACHE=/tmp/gocache`）

```
$ go build ./...            # 无输出
$ go vet ./...              # 无输出
$ go test ./... -count=1
ok  cmd/server   ok internal/admin  ok internal/clean  ok internal/ingest
ok  internal/meta  ok internal/staging  ok internal/ui  ok internal/validate
?   internal/{exception,latest,publish,settle,store}  [no test files]
?   migrations  [no test files]
```

有 DSN 的整套用例**未运行**（Lead 指示：`internal/ingest` 的 DSN 用例不支持同库并发，需要时另行安排）。

新增 Go 用例：`internal/ui/spa_test.go`（入口 / 深链 fallback / HEAD / POST 405 / 入口 `no-cache` / 哈希资源 `immutable` / `dist/index.html` 已入仓）、`cmd/server/main_test.go` 改写为 SPA 路由 + CSP + 真实资源可取。

### 3.4 REVIEW-001 §3 逐条证据

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| 产物可复现 | `npm ci && npm run build && git diff --exit-code internal/ui/dist` | exit 0 |
| vitest | `npx vitest run` | 4 文件 24 用例通过 |
| 四态 4 断言 | 用例 `四态文案互不相同` | `Set.size === 4` |
| 抽屉 Esc + 焦点回归 | 用例 `抽屉：role=dialog、Esc 关闭、焦点回到触发按钮` | 通过 |
| 只渲染 10 行 | 用例 `只渲染服务端返回的当前页 10 行` | 通过 |
| stylelint | `npx stylelint "src/**/*.css"` | exit 0 |
| 无 node 的 Go | `go build ./... && go test ./...`（产物已入仓 + embed） | 全绿 |
| §18 禁止清单 grep | 见下 | 无命中 |
| table-query §2 无本地 filter/sort/slice | 见下 | 只对「筛选条件列表 / 字段列表 / 关联目标集合」做数组运算，不对 `rows` 做任何本地过滤/排序/截断 |

```
$ grep -rnE "#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(" web/src --include=*.ts --include=*.tsx   # 无命中（tokens.test.ts 除外）
$ grep -rn "style={{" web/src --include=*.tsx                                                # 无命中
$ grep -rn "!important" web/src --include=*.css                                               # 无命中
$ grep -rnE "\.(filter|sort|slice)\(" web/src --include=*.tsx                                  # 仅 query.filters/fields/relationTargets 的数组运算
$ grep -rn "html/template\|gohtml\|NewWithPool\|static/app" --include=*.go cmd internal       # 无命中
$ git diff --stat rv-06..HEAD -- go.mod go.sum migrations internal/ingest/nongoal_test.go      # 空
$ git diff --name-only rv-06..HEAD | grep -vE "^(web/|internal/ui/|cmd/server/|docs/design\.md)"  # 无越界
```

## 4. REVIEW-001 前端 4 项处置

| 项 | 处置 |
| --- | --- |
| F1 内联样式 + 裸 div | `router.tsx` 的 `IndexRedirect` 改为 `AppShell + StatePanel/ErrorState`，去掉全部 `style={{}}`；`DataTable` 的列宽从内联样式改为 `widthStrategy` 类名（`colFixed/colMin/colFlex/colActions`）。这也让 CSP `default-src 'self'` 不需要 `style-src 'unsafe-inline'` |
| F2 `setSort` 死分支 | 删除与末尾等价的 `field === defaultSort` 分支，签名从 `(state, field, defaultSort)` 收敛为 `(state, field)`，用例同步更新 |
| F3 4px 网格例外 | 保留 0/±1px/2px 例外（边框、focus 描边、视觉隐藏），并在本报告 §2.3 显式声明理由 |
| F4 4xx 重试 | `retry: (failureCount, error) => failureCount < 1 && !(error instanceof ApiError && error.status < 500)`：网络/5xx 重试一次，`invalid_query` 等 4xx 不重试 |

## 5. 风险

| 风险 | 说明 / 缓解 |
| --- | --- |
| bundle 体积 | `index-*.js` 571KB（gzip 178.7KB）+ CSS 11.5KB。首屏为内网管理面，先接受；若需优化按路由懒加载（`data-domains` 等实体页无独立 chunk 价值，主要是 Radix/TanStack/zod 体积） |
| 产物入仓的 diff 噪声 | 每次前端变更会产生整文件 diff；缓解：`npm ci && npm run build && git diff --exit-code internal/ui/dist` 作为漂移闸门（本次实测 exit 0） |
| XSS 面 | React 默认转义、无 `dangerouslySetInnerHTML`、无内联脚本/样式；后端统一下发 CSP `default-src 'self'`（`cmd/server` 用例断言）。仍然信任后端返回的字符串仅作文本渲染 |
| `options` 上限 1000 | 超过 1000 个数据域/终端类型时，关系下拉与关系列名称会缺项（显示 id）。与 task-004 §4 的端点约定一致；规模化前需要远程搜索 |
| 抽屉字段可编辑性靠推断 | `_meta` 的 `FieldMeta` 没有 `editable`/`required`，前端按「`id` 且 `filterable` ⇒ 用户自填主键」推断（`terminals` 成立，其余自增主键被排除）。契约若调整需同步 `formFields()` |
| `web/node_modules` 干扰 `go test ./...` | 安装依赖后 `go test ./...` 会额外列出 `web/node_modules/flatted/...` 的 Go 包（无测试、不影响结果）。建议 CI 在干净树跑 Go 用例，或后续加 `web/go.mod` 把前端目录隔离出模块 |
| npm install script 被环境策略拦截 | `npm ci` 会提示 esbuild/msw 的 postinstall 被 allowScripts 策略拦截；实测构建与测试均正常（esbuild 平台二进制走 optionalDependencies）。换机器时若构建失败，先 `npm install-scripts approve esbuild` |
| 有 DSN 用例未跑 | 本次只跑无 DSN 全套；`internal/admin` 的 DSN 用例属于 `be-api` 交付范围 |

## 6. 未闭环 / 需审核方回答

- `Q-004` 已闭环：删除 `internal/ui/ui.go` 后，`entityLabels`/`exceptionEntities`/`exceptionRoutes` 不复存在，标签与异常页映射的唯一来源是 `admin.EntityLabel` / `admin.ExceptionPages` / `admin.ExceptionBySlot|ByEntity`，未新增重复定义。
- `Open-Q（新增，非阻塞）`：`_meta` 的 `FieldMeta` 建议补 `editable`（抽屉可编辑列）与 `required`（前端必填提示）两个布尔位，前端目前用推断（见 §5）。若采纳，`formFields()`/`buildSchema()` 改为读契约即可，页面无需改动。
- 无其他未闭环项。批量/导出/列偏好/保存视图按 R-018 整体豁免，已在本报告 §2.3 声明。

---

## 7. REVIEW-004（浏览器渲染层）处置与证据

- `fix-commit:` `63b0daa`（`Base: rv-07`，单独提交，未改写 `rv-07`）
- `verdict-before:` `REVIEW-004` 判定 rv-07 不可放行（F1 阻断、F2 重要、F3 建议）
- `method:` 无头 Chromium（`/usr/bin/chromium`，Chrome 156）+ CDP，`Emulation.setDeviceMetricsOverride` 强制 1440×900 CSS 视口（不用 `--window-size`，避免落入 ≤1024 断点）
- `instances:` 临时冒烟实例 `127.0.0.1:8085`（本机 PG `55432` + 真实 `internal/admin`/`internal/ui`，验证后删除，未入库）

### 7.1 F1 skip 链接缺类导致整页 grid 重排（阻断）

**根因**：`AppShell.module.css` 里是 `.skip-link`（kebab），`AppShell.tsx` 用 `styles.skipLink`；CSS Modules 不做驼峰转换 → `undefined` → 该 `<a>` 无 class、非定位元素，作为普通 grid item 占掉 `.shell` 首格。

**修复**：`.skipLink`（含 `:focus-visible { left: var(--space-2) }`）；防御性 `.sidebar{grid-column:1}` / `.main{grid-column:2}`（1024 断点内同为 `grid-column:1`）；新增 `web/src/styles/moduleClasses.test.ts` 比对 `*.module.css` 导出类名与应用代码里的 `styles.<key>` 引用。

**浏览器实测（1440×900，`/ui/terminals`）**

| 指标 | rv-07（REVIEW-004 实测） | 修复后（本次实测） |
| --- | --- | --- |
| `main.x / main.width` | `0 / 200` | **`200 / 1240`** |
| `main.y` | `484` | **`0`** |
| `.shell` grid 列 | 单列（被挤） | **`200px 1240px`** |
| `aside` | 占满首行整宽 | **`x=0, w=200, h=900`** |
| `<table>` | `x=25, w=1020`（被裁切） | **`x=225, w=1190`**（内容区满宽，`table-layout: fixed`，6 列） |
| `<a href="#main">` | 无 class | **`_skipLink_g3iyy_7`**，`position: absolute`，`left: -9999px` |

**skip 链接聚焦可见性**（真实 Tab 键，非程序化 focus）：`document.activeElement` 为该链接、`getBoundingClientRect().x = 8`、`getComputedStyle().left = "8px"`、`matches(':focus-visible') = true` —— 平时移出视口、聚焦时回到可视区，符合 §17。

**护栏用例有效性验证**：把 `.skipLink` 临时改回 `.skip-link` 后 `moduleClasses.test.ts` 两条用例同时失败，报错定位到
`AppShell.tsx → styles.skipLink（AppShell.module.css 无此类）`；改回即恢复通过。

### 7.2 F2 CSP 拦截 Radix 注入的内联样式（重要）

**修复**：CSP 改为 `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'`。
**取舍声明**：放开的是 `style-src` 的 `'unsafe-inline'`——Radix 的 Dialog/Popper primitive 用注入式内联样式做定位、滚动锁与焦点守卫，拦掉属不确定行为；`script-src` 仍为 `'self'`，产物没有任何内联脚本，XSS 面没有被这条放开。`cmd/server/main_test.go` 增加断言：CSP 必须含 `style-src 'self' 'unsafe-inline'` 且 **不得**出现 `script-src 'self' 'unsafe-inline'`。

**浏览器实测**：抽屉开、关全流程 `Log.entryAdded` / `Runtime.consoleAPICalled` / `Runtime.exceptionThrown` 三类事件中 **CSP 违规 0 条、console error 0 条**。
抽屉本体：`x=960, w=480, position: fixed`，`body.overflow=hidden`（滚动锁生效），`hasInlineStyle=true`（Radix 注入不再被拦）。

### 7.3 F3 favicon 404（建议）

补 `web/public/favicon.svg` + `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`；Vite 按 `base: '/ui/'` 重写为 `/ui/favicon.svg`，`go:embed` 随产物提供。
**浏览器实测**：`/ui/favicon.svg` 三次请求全部 200，`/favicon.ico` 无请求；整页 `status >= 400` 的请求 **0 条**（共 9 个请求）。

### 7.4 附带修复：Esc 关闭抽屉后的焦点归还（浏览器层才发现）

**现象**：真实浏览器里 Esc 关闭抽屉后 `document.activeElement` 为 `BODY`（t=0/60/250/800ms 采样一致），而 jsdom 用例通过。
**根因**：原实现用 `requestAnimationFrame` 延迟聚焦；该次导航是 React transition，rAF 落在提交之前，随后 Radix `FocusScope` 的卸载清理把焦点重置回 body。
**修复**：改成「`drawer` 参数由非空变为空的提交之后」在 effect 里聚焦，仍按 `data-action`/`data-row-id` 重新取当前节点（列表重渲染会换掉旧节点）。
**浏览器实测（真实鼠标点击「编辑」开抽屉 → Esc）**：关闭后 `activeElement = BUTTON("编辑")`，`data-action="edit"`，t=0/60/250/800ms 四次采样一致；由「新建」打开的抽屉关闭后焦点回到「新建」。

### 7.5 复跑结果（本次）

```
$ npx vitest run            → Test Files 5 passed / Tests 26 passed（新增 moduleClasses 2 条）
$ npx stylelint "src/**/*.css" → 无输出，exit 0
$ npm run build && git diff --exit-code internal/ui/dist → exit 0（产物仍可复现）
$ GOCACHE=/tmp/gocache go build ./... && go vet ./...   → 无输出
$ GOCACHE=/tmp/gocache go test ./... -count=1
ok cmd/server  ok internal/admin  ok internal/clean  ok internal/ingest
ok internal/meta  ok internal/staging  ok internal/ui  ok internal/validate
```

### 7.6 未处理（留给下一轮，非阻塞）

- 多选筛选控件仍是原生 `multiple` select（既有豁免 §13.2）；在 1440 视口下它比单行控件高、与同排标签不齐平。属视觉打磨，未在本轮顺手改（避免未申报的范围扩大），建议随 `table-query` §13.2 一起改成自定义多选或移入「更多筛选」。
- R1（`FieldMeta.editable/required`）、R2（路由级 lazy + vendor 拆分）按 Lead 安排留到 `task-005`。

---

## 8. 本轮增量：`editable`/`required` + 路由级 lazy + vendor 拆包 + R2（`Base: rv-08`）

- `commits:` `8df86ca`（契约驱动抽屉）、`8e985ae`（路由级 lazy）、`0607b6d`（vendor 拆包）、`6f1c595`（多选移入更多筛选）
- `backend:` `cf5bb41`（`_meta` 补 `editable`/`required`，`be-api`），本轮前端按实测值对齐 MSW
- `red-lines:` `internal/admin/**`、`cmd/server/**`、`go.mod`、`migrations/` 均未改动

### 8.1 契约驱动：抽屉可编辑列与必填

| 项 | 改动 |
| --- | --- |
| 类型 | `FieldMeta` 新增 `editable: boolean` / `required: boolean`；缺省按 `false`（fail-closed），**不**回退到 `id && filterable` 推断 |
| `formFields()` | 改为 `fields.filter((f) => f.editable)` |
| `buildSchema()` | 必填由 `field.required` 驱动；`(必填)` 提示与提交前校验同源；删除按字段类型猜必填 |
| 可写性 | 新增 `isWritable()`：`readOnly` **或**没有任何 `editable` 字段 → 不渲染写入口；异常两页全部 `editable=false`，因此只读判定不依赖路由 |
| MSW | 按后端实测值：`terminals` id `T/T`、type_id `T/T`、gateway_id `T/T`、name `T/F`、enabled `T/F`；`data-domains` id `F/F`、code/name `T/T`；`quality-rules` terminal_id/item_code/rule_type `T/T`、enabled/params `T/F`；异常两页全 `F/F` |

**真实后端核对**（临时实例 `127.0.0.1:18999`，本机 PG，验证后删除）：

```
terminals   [('id', True, True), ('type_id', True, True), ('gateway_id', True, True), ('name', True, False)]
data-domains[('id', False, False), ('code', True, True), ('name', True, True)]
exceptions  editable 集合 = {False}
```

CDP 实测抽屉表单：5 个控件，标签为 `终端 ID（必填）`、`终端类型（必填）`、`网关 ID（必填）`、`名称`、`状态` —— 星号只跟 `required` 走，与后端口径一致。

### 8.2 路由级 lazy

- `EntityListPage` / `ExceptionListPage` 改 `React.lazy`；边界放在 `MetaGate`（`_meta` 就绪）之后，`Suspense` fallback 直接复用 `DataTable` 的加载态，因此**列数、`widthStrategy` 列宽、行高与真实表格一致**（放在 meta 之前只能固定列数，等于破坏 §6）。
- `MetaGate` 同时收口「字段元数据加载中 / 失败」两个外壳态；页面内重复分支删除。
- **四态判据逐字未改**（只改等待时机）。改前/改后表达式：

```
改前（rv-08）: renderApp → findByRole('table') → waitFor(10 行 + 首行 t-001)
改后（本轮） : renderApp →（lazy 解析）→ findByRole('table') → 同一组 waitFor 表达式
判据（两版完全相同）:
  expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
  expect(bodyRows()).toHaveLength(10);
  expect(bodyRows()[0]).toHaveTextContent('t-001');
  expect(seen.size).toBe(4);
```
无需改动用例：`waitForReady()` 本来就在等「表格就绪」，lazy 解析完成后自然满足。

### 8.3 vendor 拆包与体积对照（1440×900，CDP 实际网络记录）

| 阶段 | 首屏文件数 | 首屏 raw | 首屏 gzip | 进入列表页后累计文件数 | 累计 raw | 累计 gzip |
| --- | --- | --- | --- | --- | --- | --- |
| `rv-08` 单 chunk | 2（1 js + 1 css） | 529,357 | 162,114 | 2 | 529,357 | 162,114 |
| 路由级 lazy | 2（1 js + 1 css） | 382,252 | 118,085 | 8 | 526,494 | 164,451 |
| + vendor 拆包 | 5（4 js + 1 css） | 384,179 | 119,382 | 11（9 js + 2 css） | 529,072 | 165,864 |

- **首屏**：`-27.4%` raw（529,357 → 384,179）、`-26.4%` gzip（162,114 → 119,382）；首屏文件数 2 → 5（HTTP/1.1 下多几个 RTT，内网可接受）。
- **累计**（列表页完全就绪）：raw 基本持平（−285 B），gzip `+2.3%` —— 拆分失去了跨 chunk 的压缩共享，这是拆包的固有代价，已如实列出；vendor 拆分的收益按约定是**长缓存与警告消除，不降首屏字节**。
- 单文件最大值 517,609 B → 223,623 B（`vendor-react`），Vite 的 `>500 kB` 警告消失。

首屏加载顺序（CDP `Network.responseReceived`）：`index.js` → `vendor-react` / `vendor-router` / `vendor-tanstack`；随后按需 `EntityListPage` / `Pagination` / `vendor-misc` / `vendor-radix` / `vendor-zod`。

### 8.4 R2：多选与关联筛选移入「更多筛选」

- 常驻筛选只收单行控件（`text`/`boolean`/`numberRange`/`dateRange`）；`multiSelect`/`relation` 一律进抽屉，`advancedOnly` 仍由后端字段决定。
- CDP 实测：常驻区标签 = `关键词 / 终端 ID / 网关 ID / 名称 / 状态`，**无「终端类型」**；打开「更多筛选」后 `#filter_type_id` 存在。
- 已生效条件标签外显、单删/全清逻辑未变（用例仍通过）。

### 8.5 本轮复跑

```
$ npx vitest run            → Test Files 6 passed / Tests 34 passed
$ npx stylelint "src/**/*.css" → 无输出，exit 0
$ npx tsc --noEmit          → 无输出
$ npm run build && git diff --exit-code internal/ui/dist → exit 0
$ GOCACHE=/tmp/gocache go build ./... && go vet ./...   → 无输出
$ GOCACHE=/tmp/gocache go test ./... -count=1           → 8 包全绿（无 DSN；本轮未改 Go 代码）
```

新增前端用例：`formFields` 只返回 `editable:true` / fail-closed（缺键即不可编辑）/ 异常两页不可写 / `required` 阻止空提交并显示 `(必填)` / 非 required 字段不被前端拦下 / 常驻区无关联筛选（单元 + RTL 两层）。

### 8.6 CDP 冒烟（1440×900，`Emulation.setDeviceMetricsOverride`）

| 项 | 结果 |
| --- | --- |
| `main` | `x=200, y=0, w=1240`；`aside w=200` |
| `table` | `x=225, w=1190, table-layout: fixed`，6 列 4 行 |
| 抽屉 | 标题「新建终端」，5 个控件，`(必填)` 标记 3 个；Esc 后 `dialogs=0`，焦点回 `BUTTON("新建")`、`data-action="new"` |
| console | CSP 违规 **0**、error/exception **0** |
| 截图 | `.devcache/shots/round4-1440.png` |

### 8.7 风险

- vendor 拆分后**累计** gzip 比单 chunk 高 2.3%（压缩共享丢失），换来的是首屏 −26% 与长缓存；如果后续更在意总量，可合并 `vendor-misc`（12.8 KB）回入口。
- 首屏文件数 2 → 5，HTTP/1.1 + 高 RTT 环境下收益会打折；内网同源部署按现状接受。
- `editable`/`required` 是 fail-closed：后端若不发这两个键，抽屉将没有可编辑字段（本轮后端已落地，属契约依赖）。
