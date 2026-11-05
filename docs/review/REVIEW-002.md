# REVIEW-002 · task-004 最终审核 · MODE: REVIEW

- `basis:` `rv-06`（5dabe0d）..`da4a4a5`（10 个提交，未 push、未打 tag）
- `verdict:` **放行** —— 建议打 `rv-07`
- `spec:` `admin-ui-spec` 通用 §2–§8/§11/§13–§18 + `table-query` §1–§7/§9–§13；`task-004` §5/§8/§10
- `method:` 全部由本审核方**独立复跑**（不是执行者自述），命令与结果见 §2

## 1. 放行判断

| 检查面 | 结论 |
| --- | --- |
| 需求一致性（task-004 §2/§4/§7） | 通过：6 实体 + 异常两页、`_meta`/`exceptions`/`options` 三端点、产物入仓、SSR 删除 |
| 契约一致性（§4 键名） | 通过：`title`/`enums`/`booleans`/`ref`/`pagination` 与后端 `03ca27e` 逐一对应；前端零硬编码列与业务枚举 |
| 硬约束（§5） | 通过：URL 唯一事实源、服务端查询、四态、令牌、CSP、AbortSignal |
| 非目标（§3/§10） | 通过：无批量/导出/登录/i18n/暗色；`go.mod` 未动；SSR 无双轨 |
| 测试与可复现（§8） | 通过：见 §2 全绿 + 产物 `git diff --exit-code` = 0 |

## 2. 独立复跑证据

```
$ GOCACHE=<ws>/.devcache/gocache go build ./... && go vet ./...          # 无输出
$ GOCACHE=... go test ./... -count=1                                    # 14 个包，全绿（无 DSN）
$ GOCACHE=... IOT_TEST_DSN=postgres://…:55432/iot go test ./... -count=1 # 全绿（含 ingest 2.333s）
$ cd web && npm ci && npx tsc --noEmit && npx stylelint "src/**/*.css"   # 干净，exit 0
$ npm run build && git diff --exit-code internal/ui/dist                 # exit 0（产物可复现）
$ npx vitest run                                                         # 4 files / 24 tests passed
```

vitest 关键用例（逐条对应 §8）：只渲染服务端返回 10 行（总数 128 不落 DOM）、四态文案互不相同、抽屉 `role=dialog` + Esc + 焦点回触发按钮、新建保存后关抽屉刷新 + 轻提示、删除二次确认后才发 DELETE、写失败就地提示且抽屉不关。

红线核对（`git diff --name-only rv-06..da4a4a5`）：越界文件 **0**；`go.mod`/`go.sum`/`migrations/`/`nongoal_test.go` **零改动**；`node_modules`/`.vite`/`*.map` **未入库**。

组件级条款核对（读码确认，非自述）：`DataTable` 用 `table-layout: fixed` + `<colgroup>` 按 `widthStrategy` 取类名、`scope="col"`、`aria-sort`、单元格 `title` 截断（通用 §6、`table-query` §3/§9）；`FilterBar` 常驻筛选取 `quickFilters`、其余进「更多筛选」Dialog、`ActiveFilterChips` 带 `aria-label` + 单删/`清除全部筛选`（§5）；抽屉走 Radix `Dialog`（§5/§17）；Toast `role=status aria-live=polite`（§15）。

禁止清单 grep（§18）：`style={{` 0 处、`tokens.css` 之外颜色字面量 0、`!important` 0、相对时间（`刚刚|分钟前|小时前`）0、通用「暂无数据」0；`rows.filter/sort/slice` 仅出现在 `web/src/test/server.ts`（MSW 假后端，不构成前端本地过滤）。

## 3. 非阻断发现（不拦放行，进下一轮或记账）

| # | 位置 | 问题 | 条款 | 建议 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| R1 | `web/src/api/types.ts`（`FieldMeta`） | 缺 `editable`/`required`，抽屉可编辑列靠 `id && filterable` 推断；当前 6 实体成立，但属**推断而非契约** | task-004 §4 闭集、`table-query` §3 | 下一轮让后端补 `editable`/`required`，前端 `formFields()` 改读契约 | 建议（已登记 Open-Q） |
| R2 | 构建输出 | 单个 chunk > 500 kB（Vite 警告），首屏体积偏大 | — | `manualChunks` 拆 `react`/`tanstack`，或路由级 `import()`；不影响功能 | 建议 |
| R3 | `web/node_modules` 存在时 | `go test ./...` 会多扫到 `flatted` 之类非 Go 包条目（无测试文件） | — | `web/.gitignore` 已 ignore，不影响结果；CI 里先 `npm ci` 再跑 Go 测试时注意顺序 | 建议 |
| R4 | `docs/design.md:160` | 提前写成「rv-07」，而 tag 由审核后打 | — | 放行后命名一致，无需改；若最终改名需同步 | 说明 |
| R5 | 沙箱环境 | 8080 上有早期 SSR 版本进程残留（`/admin/v1/_meta` 404），且 MQTT broker 抖动导致完整 `server` 进程起不来 | — | 属环境，不是代码问题；SPA 服务语义已由 `TestRoutes`/`TestSPAHandler` 覆盖（入口/深链 fallback/哈希资源/CSP） | 说明 |

## 4. 结论

task-004 达到 §8 九条验收 + 本审核追加项；`REVIEW-001` 的 F1–F4 均已处置（F1 顺带让 CSP 的 `default-src 'self'` 真正可生效，F3 已书面豁免）。**同意打 `rv-07`**。

`STATUS: UNKNOWN=0 阻塞 | BLOCKERS=0 | DEFAULT=0 | DEFERRED=0（本轮） | SETTLED=0（本轮） | REVOKED=0`
