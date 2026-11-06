# report-009b · 最新值只读页与质量标识（前端）

- `task:` task-009 Part B（`status: READY`，A 由 `be-api` 交付）
- `baseline:` rv-12（`d909596`）
- `commit:` `c79e5bb` `feat(ui): 最新值只读页与质量标识`（`Base: rv-12`）
- `范围:` `web/**` + `internal/ui/dist`；`internal/admin/**`、`internal/ingest/**` 未改动
- `联调状态:` **后端 Part A 未落地**（仓库内尚无 `internal/admin/readonly.go`），本报告的全部页面证据来自 MSW 假后端 + 静态 mock 实例；联调待 `be-api` 交付后补

## 1. 变更说明

| 落点 | 改动 |
| --- | --- |
| `web/src/api/types.ts` | 新增 `ReadonlyMeta`；`MetaPayload.readonly?: ReadonlyMeta[]`（缺省按空数组，fail-closed 不出多余导航） |
| `web/src/api/meta.ts` | `normalizeMeta` 对 `readonly[].fields` 做与实体一致的 options 归一 |
| `web/src/api/exceptions.ts` | 新增 `useSlotList(source, slot, query)`：`exceptions` / `readonly` 共用一套查询键与前缀 |
| `web/src/pages/ReadonlySlotPage.tsx`（新） | 通用只读页：列/中文/枚举色调来自 `_meta`；`quality` 用 options 渲染状态标签；`quality_reason` 单行截断 + `title`；时间绝对 `+08:00`；三态 + 空态；零写入口 |
| `web/src/pages/ExceptionListPage.tsx`（删） | 与只读页合并，避免两套几乎相同的实现 |
| `web/src/pages/LazyRoutes.tsx` | 新增 `LazyReadonlyPage`；`LazyExceptionPage` 与它共用 `LazySlotPage`（按 source 取 meta 段） |
| `web/src/router.tsx` | 新增 `/readonly/:slot` |
| `web/src/components/AppShell.tsx` | 导航新增「数据」分组（slot 来自 `_meta.readonly`），「异常」分组保持原样 |
| `web/src/lib/format.ts` | **字段带 `options` 就按状态标签渲染**（此前只按 `type` 判断，`quality` 这种 number+options 会走数字格式） |
| `web/src/components/{Toolbar.module.css,FilterBar.tsx}` | 范围控件两个输入改为并排、筛选条改顶部对齐（见 §5 偏离②） |
| `web/src/styles/global.css` | `.preset-row`（「仅看有问题」开关） |

**「仅看有问题」**：`filter.quality.gte=1`（不新增 SQL 操作符）。开关状态由 URL 派生（`filters` 里 `quality:gte` 是否含 `1`），勾选/取消与条件标签单删/全清双向同步，不存本地状态。

## 2. 规范覆盖（`admin-ui-spec` + `table-query`）

| 条款 | 落点 |
| --- | --- |
| 通用 §3 布局与导航 | 导航「数据」分组下「最新值」，文案来自 `_meta.readonly[].title` |
| 通用 §4 信息分层 | 8 列全部可见；`value_num`/`value_text`/`unit`/`quality_reason` 进「更多筛选」 |
| 通用 §6 布局稳定性 | 列宽走 `widthStrategy`；`quality_reason` 单行截断 + `title`；范围控件两输入并排、标签同排对齐 |
| 通用 §8 状态保持 | `q/filters/sort/page/pageSize` 全进 URL；「仅看有问题」也由 URL 派生 |
| 通用 §11 搜索与筛选 | 常驻 `terminal_id`/`item_code`/`value_time`/`quality`，其余进「更多筛选」；条件标签常驻、可单删/全清 |
| 通用 §13 网络异常 | 失败态含后端 detail + 重试 + 清除筛选 |
| 通用 §16 语义规范 | `quality` 标签 = 文字 + 颜色（`options[].tone`）；时间绝对 `+08:00` |
| 通用 §17 可访问性 | `scope="col"` + `aria-sort`、`<label for>`、开关有可见文本、`aria-live` 提示（复用） |
| `table-query` §1/§2 | 服务端查询；前端不做本地过滤/排序/分页；`quality.gte` 走同一 QueryState |
| `table-query` §3/§4 | 字段能力（sortable/filterable/operators）全部来自 `_meta` |
| `table-query` §5 | 常驻 ≤5、其余进抽屉、条件外显 |
| `table-query` §9 | `widthStrategy` 列宽、横向滚动不压列宽 |
| `table-query` §8 / §9 导出 | 仍整体豁免（R-018，只读页同样不做批量与导出） |

## 3. 测试（实测）

```
$ cd web && npx vitest run
 Test Files  7 passed (7)
      Tests  40 passed (40)

$ npx tsc --noEmit      → 无输出
$ npx stylelint "src/**/*.css" → 无输出，exit 0
$ npm run build && git diff --exit-code internal/ui/dist → exit 0
```

新增 6 条用例（`web/src/pages/ReadonlySlotPage.test.tsx`，全部基于 MSW）：

| 用例 | 断言 |
| --- | --- |
| 渲染当前页 10 行、总数 128 | `第 1–10 条，共 128 条`；8 个 quality 标签文案全部来自 `_meta.options`；`quality_reason` 带 `title`；时间为 `YYYY-MM-DD HH:mm:ss +08:00` |
| 只读 | 无「新建/编辑/删除」按钮、`[data-action=new|edit]` 不存在、无 `role=dialog`、翻页后零写请求、页面有「只读页面…」说明 |
| 仅看有问题 | 勾选 → URL 出现 `filter.quality.gte=1`、总数 128→112、条件标签「质量 ≥ 1」；单删标签 → URL 去掉条件且开关回未勾选 |
| 一键全清 | `q` + `filter.quality.gte` 同时清空，URL 不含 `filter.`/`q=` |
| 三态互不相同 | `暂无最新值` / `没有匹配的记录` / `列表加载失败`（含 `db_unavailable` + 重试），`Set.size === 3` |
| 导航与未知 slot | 导航出现「最新值」；`/readonly/unknown-slot` 给「未知只读页」+「返回上一页」 |

零写请求的断言方式：MSW 的写处理器把每次 POST/PUT/DELETE 记进 `writeCalls`，整页用例结束时断言 `writeCalls.length === 0`；CDP 侧另用 `Network.responseReceived` 统计（见 §4，`/admin/*` 只有 `_meta` 与只读列表两类 GET）。

## 4. 浏览器层证据（CDP，1440 视口）

**环境**：后端 Part A 未落地，用静态 mock 实例 `127.0.0.1:19099`（`_meta` 取自 MSW fixture 的 JSON、`/admin/v1/readonly/latest-values` 返回 128 行）+ 仓库 `internal/ui/dist` 产物；Chromium 156 + CDP `Emulation.setDeviceMetricsOverride`。

| 项 | 实测 |
| --- | --- |
| `main`（1440×900） | `x=200, y=0, w=1225`；`documentElement.clientWidth=1425`、竖向滚动条 15px（`docHeight=992`） |
| `main`（1440×1200，页面不滚动） | `x=200, w=1240`；`shell` 列 = `200px 1240px` |
| `aside` | `x=0, w=200` |
| `table` | `x=225, w=1305`（8 列，容器内横向滚动），10 行 |
| 导航 | 「数据」分组下「最新值」存在且高亮 |
| quality 标签 | 正常 / 越界 / 规则命中 / 越界+规则 / 单位闭集外 / 越界+单位 / 规则+单位 / 越界+规则+单位（全部来自 options） |
| 写入口 | `[data-action=new|edit|delete]` 均不存在；按文案统计「新建/编辑/删除」按钮 = 0 |
| 抽屉 | `role=dialog` 数量 0（不渲染） |
| 「仅看有问题」 | 勾选后 `location.search === "?filter.quality.gte=1"`，标签区「质量 ≥ 1」，总数 112 |
| 写请求 | `/admin/*` 请求仅 `_meta` 与 `readonly/latest-values`，**零 POST/PUT/DELETE** |
| console | error / CSP 违规 **0** |

> 1440×900 下 `main.w=1225` 是**竖向滚动条**（15px）所致：8 列 × 10 行 + 筛选区高度 992px > 900px。同一页面在 1440×1200（不产生滚动条）实测 `w=1240`，两列布局 `200px 1240px` 未被破坏。这条同时说明验收里的「`w=1240`」应当配合「页面不出现竖向滚动条」的视口高度来判定。
>
> 截图：`.devcache/shots/task009b-1440.png`。

## 5. 偏离与假设

| 类型 | 内容 |
| --- | --- |
| Assumption | 「仅看有问题」的字段键 `quality` 与阈值 `1` 是本批固定产品语义（task-009 §2），集中在 `ReadonlySlotPage` 的 `QUALITY_FIELD_KEY` / `TROUBLE_THRESHOLD` 两个常量；页面只在「存在 `quality` 字段且支持 `gte`」时才渲染该开关，其余只读页不受影响 |
| Deviation ① | 异常两页与通用只读页合并为同一实现（删除 `ExceptionListPage.tsx`），未知页文案统一为「未知只读页」。理由：两者只差数据源前缀与标题，双份实现会导致后续字段/筛选改动两处打补丁 |
| Deviation ② | 顺带修正范围控件（`numberRange`/`dateRange`）在筛选条内被 `.input { width:100% }` 挤成上下两行、且筛选条底部对齐导致同排标签错位的问题（`.optionGroup > input` 改并排、`.toolbar` 改顶部对齐）。这是 R2「控件高度参差」的同类缺陷，由本页首次出现的两个范围常驻筛选暴露；未改其他模块 |

## 6. 风险

| 风险 | 说明 |
| --- | --- |
| 后端未落地 | 真实实例目前不会出现「最新值」导航与页面；`be-api` 交付 `_meta.readonly` 后需联调复核一次（`curl /admin/v1/_meta` 有 `readonly[0].slot=latest-values`、8 字段、`quality.options` 8 项） |
| `quality` 是 number 带 options | 渲染层已改为「有 options 即按标签渲染」，若后续新增「数值+枚举混合」字段会一并生效，需要确认语义一致 |
| 「有问题」阈值写死 1 | 若质量标记位重新编号（例如新增 bit），阈值需要跟着改；届时建议由后端下发 `troubleThreshold` |
| 表格横向滚动 | 8 列在 1440 下超出内容区（1305 > 1225），按规范走容器内横向滚动，不压列宽 |
| 只读页无导出/批量 | R-018 豁免，与实体页一致 |
