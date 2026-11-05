# report-002 · 管理后台 Web UI（单二进制 SSR）

- `task:` task-002（v1，`status: READY`）
- `baseline:` rv-04（72992a8）
- `tag:` **rv-05**
- `commits:`
  | 步 | hash | 内容 |
  |---|---|---|
  | 1 | `12d81ae` | `admin` 字段元数据 + QueryState + 列表 `meta` |
  | 2 | `156e445` | `internal/ui` 骨架 + 列表页（embed/令牌/三态/分页/标签/抽屉壳） |
  | 3 | `ae40468` | 6 实体接线 + 关系列名称 + 抽屉增删改 |
  | 4 | `9e91a2d` | 异常事件/区间只读页 |
  | 5 | （本次报告提交） | `/` 重定向、CSS 令牌 lint、design.md 同步、本报告 |
- `red-lines:` `go.mod`/`go.sum` 零变化、`migrations/` 零变化、`nongoal_test.go` 零改动（`git diff --name-only rv-04..rv-05` 无这些路径）

## 1. 变更说明（按步对应 §8）

| 步 | 落点 | 做了什么 |
| --- | --- | --- |
| 1 | `internal/admin/fields.go`（新增） | 6 实体 + 2 张异常只读表的字段元数据：`Key/Label/Type/Visible/Sortable/Filterable/FilterType/Operators/Options/OptionsSource/AdvancedOnly/WidthStrategy`，另有 `FieldMetas/FieldMetaOf/RelationTarget/VisibleFields/QuickFilters/AdvancedFilters/TextSearchFields/OptionLabel/OptionTone/OperatorLabel` |
| 1 | `internal/admin/query.go`（新增） | `ParseQueryState`（白名单校验、`limit/offset` 兼容、page/pageSize 归一）、`BuildListQuery`/`BuildCountQuery`（同 where）、`ListRows`（列表+总数一体）、`escapeLike`；错误统一 `invalid_query: <detail>` |
| 1 | `internal/admin/admin.go`（改） | `list` 改走 QueryState，响应加 `meta{total,page,pageSize,sort,order}`；`data` 仍是数组 |
| 2 | `internal/ui/*`（新增） | `ui.Handler`/`Deps`/路由、`PoolSource`（List/Get/Create/Update/Delete）、`render.go`（embed 模板 + 局部渲染）、布局与列表模板、`app.css`（`:root` 令牌）、`app.js` |
| 2 | `cmd/server/main.go`（改） | 抽出 `newMux`，挂 `/ui/`，`GET /{$}` → `/ui/` |
| 3 | `internal/ui/ui.go`（改） | POST create/update/delete；`columnKinds` 列类型白名单转换；JSON/303 双响应；`returnTo` 只接受站内前缀 |
| 4 | `internal/admin/fields.go` + `internal/ui/ui.go`（改） | 异常两表字段元数据 + `/ui/exceptions/{events,intervals}` 只读页；`exception-interval.period` 用 `period::text` 子查询展示 |
| 5 | `cmd/server/main_test.go`、`internal/ui/lint_test.go`（新增）、`docs/design.md`（改） | 路由断言、CSS 令牌/模板安全 lint、design.md §七 |

字段元数据驱动是唯一入口：页面控件、SQL 白名单、排序/筛选能力、枚举中文文案与状态颜色都从 `fields.go` 派生，模板里没有业务枚举中文，也没有写死控件。

## 2. 覆盖的规范条款（`admin-ui-spec`）

### 2.1 通用规范

| 条款 | 落点 |
| --- | --- |
| §2 设计令牌 | `app.css` 的 `:root`：颜色/间距/字号/行高/圆角；间距 4px 网格；`lint_test.go` 断言令牌外无颜色字面量 |
| §3 布局与导航 | 左侧固定导航（6 实体 + 2 异常页）、`skip-link`、1024px 单断点 |
| §4 页面模式与信息分层 | 列表行只放状态与关键字段；`params`(json)、`min/max`、`sample_digest` 标 `Visible:false` 进抽屉 |
| §5 组件 | 按钮主次（主按钮 ≤1）、表格、表单 `<label for>`、浮层标题 + 底部固定「取消/保存」 |
| §6 布局稳定性 | `table-layout: fixed` + `colgroup` 按 `widthStrategy`；单元格单行截断 + `title` 全文；分页器/总数固定 `min-width`；骨架行与真实行同高（无脉冲 div） |
| §7 跨页面交互 | 编辑用侧滑抽屉（`?drawer=<id>`）不跳页；关闭回原 URL |
| §8 列表、详情与状态保持 | 筛选/排序/分页全部进 URL；返回/刷新恢复；滚动位置 `sessionStorage` 按 URL 键恢复；操作后原地刷新 + 轻提示 |
| §11 搜索、筛选与空结果 | 关键词、字段筛选、「更多筛选」、条件标签外显、空/无匹配两态分列 |
| §13 网络异常 | 列表加载失败态含原因 + 重试入口；写失败给具体原因 |
| §14 键盘与基础可用 | Esc 关闭浮层、焦点回触发元素、触摸目标 ≥44px、1024px 以上布局正确 |
| §15 通知 | 成功/失败用页面顶部轻提示，不打断、不遮挡 |
| §16 语义规范 | 状态标签 = 文字 + 颜色；时间一律绝对时间 `+08:00`；错误含原因与恢复路径；`启用/停用`、`是/否`、枚举中文集中在 `fields.go` |
| §17 可访问性 | `<table>` + `scope="col"`、`<label for>`、抽屉 `role="dialog" aria-modal="true"`、删除 `role="alertdialog"`、`aria-live` 轻提示、`aria-current` 导航 |
| §18 禁止清单 | 无硬编码颜色/非网格间距、无脉冲 div、筛选进 URL、空状态不复用「暂无数据」、无相对时间、表格不压缩列宽换行 |

### 2.2 表格查询子规范

| 条款 | 落点 |
| --- | --- |
| §1 核心原则 | 单一 QueryState，服务端执行全局查询；前端不做本地过滤/排序/分页 |
| §2 QueryState | 结构化 filters（`field/op/value`）、后端白名单校验、条件变化回第一页 |
| §3 字段元数据 | 每个字段显式声明 `filterable/sortable/filterType/operators/advancedOnly/widthStrategy` |
| §4 类型映射 | 枚举多选 `in`、文本 `contains/eq/prefix`、数值 `gte/lte`、布尔三态 `eq`、关系 `in` |
| §5 筛选区 UI | 常驻 ≤5、其余进「更多筛选」抽屉；**已生效条件始终以标签外显**，可单删/全清 |
| §6 分页 | 默认 10，可选 20/50，上限 50；条件或 pageSize 变化回第一页；显示范围与总数；超范围返回空页并提示修正 |
| §7 排序 | 白名单 + 默认 `id desc` + 唯一 tie-breaker；不可排序列无入口；方向标记 ↑/↓ |
| §9 列展示 | `widthStrategy` 决定列宽、横向滚动不压列宽、单元格单行截断 + 悬浮全文 |
| §10 状态保持 | URL 恢复筛选/排序/分页 + 滚动位置恢复 |
| §11 空/错/可访问性 | 三态区分；控件有标签；抽屉 Esc；颜色非唯一载体 |
| §12 后端约束 | 字段/操作符/排序白名单、`LIKE` 通配符转义、参数类型校验、全部走参数化 SQL |
| §13 验收清单 | 1/3/4/5/7 ✅；2、8、10 部分（见 §2.3）；6/9/11/12 豁免或 N/A |

### 2.3 豁免与未覆盖（逐条给理由）

| 条款 | 状态 | 理由 |
| --- | --- | --- |
| `table-query` §8 全选与批量（L1/L2/L3） | **整体豁免** | task-002 §3 明确不做批量操作：表格无选择列、无批量状态栏 |
| `table-query` §9 导出 | 豁免 | 非目标（§3）；本期无导出入口 |
| `table-query` §6 深分页/cursor | 豁免 | 管理后台数据量小，用 offset + 上限即可；超范围已按规范提示 |
| `table-query` §2 `requestId`/`AbortController` | 未实现 | 页面是整页/片段服务端渲染，没有并发查询竞态；前端只有「就地刷新」一次 fetch |
| `table-query` §13.2 多选「全选当前选项/已选计数」 | 部分实现 | 用原生 `multiple` 选择器（可键盘操作、显示已选高亮），没有自定义全选按钮与计数徽标 |
| `table-query` §13.8 1000+ 条/深分页/count 超时 | 未覆盖 | 无容量压测（task-002 §3 不做容量项）；count 与列表共用 where，超时未单独处理 |
| `table-query` §10.6 实时更新冲突 | N/A | 无 SSE/实时推送（R-013 非目标） |
| 通用 §9 全选三态/状态栏 | 豁免 | 同上，无批量 |
| 通用 §12 长任务与离开回来 | N/A | 无长任务 |
| 通用 §14 移动端专项（筛选抽屉、底部选择栏） | 豁免 | task-002 §3：只保证 1024px 以上 + 44px 触摸目标 |
| 通用 §13 认证/权限 | 豁免 | R-011 `DEFAULT`：沿用 `AllowAll` + 127.0.0.1，不做登录页 |

## 3. 测试结果

### 3.1 无 DSN

```
$ go build ./... && go vet ./...          # 均无输出
$ go test ./... -count=1
ok  cmd/server  ok internal/admin  ok internal/clean  ok internal/ingest
ok  internal/meta  ok internal/staging  ok internal/ui  ok internal/validate   （8 个包）
```

### 3.2 有 DSN（postgres:16-alpine，端口 55432）

```
$ IOT_TEST_DSN=... go test ./... -count=1     # 全绿
$ IOT_TEST_DSN=... go test ./... -count=1 -v  # --- PASS 96，SKIP 2（容量压测、MQTT 双路）
```

`internal/ui` + `internal/admin` + `cmd/server` 合计 30 个用例（新增）。

### 3.3 验收 §9 逐条

| 验收项 | 结果 | 证据 |
| --- | --- | --- |
| 未知筛选字段/排序字段/操作符 → error（三条独立用例） | ✅ | `TestParseQueryStateRejectsUnknownFilterField` / `...SortField` / `...Operator`（另有不可筛选字段拒绝用例） |
| `q="50%_off\"` 的 LIKE 参数已转义 | ✅ | `TestBuildListQueryEscapesLike`：参数 = `%50\%\_off\\%` |
| `page=0→1`、`pageSize=1000→50`、`limit/offset` 映射 | ✅ | `TestParseQueryStatePagination`（含新参数优先、非法值报错） |
| `BuildListQuery` 含 `order by <白名单列> desc, id desc` | ✅ | `TestBuildListQueryOrderBy`：`order by name desc, id asc`；按 id 排序不重复追加 |
| 有 DSN：3 行 terminal 三种组合行序与 `meta.total` | ✅ | `TestListRowsWithDB`（filter 2 行 / q 3 行 / page=2&pageSize=2 → `qtest-3` / 超范围 0 行但 total=3） |
| 列表页含 `<table>`、标签、分页器、total；三态文案不同 | ✅ | `TestListPageRendersTableTagsAndPagination`、`TestThreeStatesAreDistinct` |
| `?drawer=new` 含 `role="dialog"`、标题、取消、保存；`?drawer=<id>` 含该行数据 | ✅ | `TestDrawerNewRendersDialogShell`、`TestDrawerEditRendersRowValues` |
| `lint_test.go` 断言 `:root` 之外无颜色字面量 | ✅ | `TestCSSTokensOnlyInRoot`（另有 4px 网格、模板无 `template.HTML`、可访问性标记三条） |
| `GET /` 302 → `/ui/`；`/ui/` 302 → `/ui/data-domains`；`/ui/static/app.css` 200 | ✅ | `cmd/server` `TestRoutes` |
| 无 DSN 全绿；`TestNoOutOfScopeArtifacts` 未改仍通过 | ✅ | §3.1；`git diff --name-only rv-04..rv-05` 不含 `nongoal_test.go` |
| 有 DSN 现有 63 个 `Test*` 全绿 | ✅ | §3.2（96 PASS 含原有 63） |

### 3.4 端到端手工验证（真实 PG + 真实 UI handler）

用临时进程（`cmd/uismoke`，验证后删除，未提交）挂 `/ui/` 与 `/admin/v1/`：

```
POST /ui/terminals            → 303，DB 出现 1 行（filter.id=smoke-2 读回）
POST /ui/terminals/smoke-2    → {"ok":true}，DB enabled false→true→false 生效
POST /ui/terminals/smoke-2/delete → {"ok":true}，DB 归零
POST /ui/quality-rules (params={"min":0,"max":100}) → jsonb 正确落库
POST /ui/terminals (type_id=abc) → {"ok":false,"message":"保存失败：字段 type_id 需要整数: \"abc\""}
GET  /ui/exceptions/events    → 200，枚举中文标签 + 绝对时间
GET  /ui/exceptions/intervals → 200，period 渲染为 [起,止)
GET  /ui/exceptions/events?filter.period.gte=... → 400（period 不可筛选）
```

## 4. 风险与遗留

1. **关系列回填上限**：为显示 `domain_id/type_id/terminal_id` 的名称，每次渲染都会对目标实体取前 50 行建 id→名称映射。超过 50 个数据域/终端类型时关系列退化为只显示 id（不会错，只是不够可读）。规模化前需要改成按需查询或加缓存。
2. **每次列表两次查询**：`meta.total` 用独立 `count(*)`，大数据量时是额外开销；异常表随时间增长会变慢（属容量项，本期不做）。
3. **异常区间的时间筛选走 `created_at`**：`period` 是 `tstzrange`，直接比较语义有歧义（开闭区间/无限上界），因此声明为不可筛选；若要按区间重叠筛选，需要单独设计操作符。
4. **无并发竞态保护**：页面是服务端渲染 + 单次 fetch，不存在多请求竞态；若日后改成局部异步查询，需要按 `table-query` §2 加 `requestId`/`AbortController`。
5. **写入错误文案透传 Postgres 原文**：约束冲突时用户看到的是英文 SQL 报错（如 `violates check constraint "quality_rule_rule_type_check"`），原因可见但不友好。
6. **Q-003 / R-014 未做**：`write()` 失败仍是 200 + `cache_write_failed`，按 R-014 属 `task-003`，本任务书明确不得混入。
7. **UA 清单未全部勾选**：`docs/review/UA-CHECKLIST.md` 里 M1（terminals 端到端）以外的条目需要人工在浏览器里逐条走查，本次只做了 HTTP 层端到端验证（无截图证据）。

## 5. 未闭环

- 无新增 `Q-NNN`。`Q-003` 已由 reviewer 答复（R-014，另立 `task-003`），本任务书内不实现。
- `R-013` 非目标（批量/导出/列偏好/SSE/图表/i18n/深色主题/登录页/移动端专项）保持 `DEFERRED`。
