# REVIEW-003 · rv-05→rv-07 独立复核（MODE: REVIEW）

- `basis:` `rv-05`（d5fd48b）..`rv-07`（4abaccd）：`b82a788` 503 小修 + `8208525`/`03ca27e` admin 契约 + `f393a21`..`f3bcc13` React SPA + `d117983` 测试隔离
- `verdict:` **放行，同意 rv-07 作为基线** —— 无阻断项
- `spec:` `admin-ui-spec` 通用 §2/§5/§6/§8/§11/§16/§17/§18 + `table-query` §1/§2/§3/§6/§7/§10/§13
- `与 REVIEW-002 的关系:` 结论一致，但本文件是**独立取数**（未沿用其结论），并追加 5 条它未记录的发现

## 1. 红线核对（我自己跑的 grep/diff，不看报告）

| 检查 | 结果 |
| --- | --- |
| 越界文件 | `rv-05..rv-07` 仅触及 `internal/admin`、`internal/ingest/{pipeline.go,exception_test.go}`、`internal/ui`、`web/`、`cmd/server`、`docs` —— 0 越界 |
| `go.mod` / `go.sum` / `migrations/` / `nongoal_test.go` | **零改动** |
| `node_modules` / `*.map` 入库 | 无 |
| `internal/ui/dist` 入仓 | 3 个文件（index.html + 2 个哈希资源） |
| SSR 双轨残留 | 无：`templates/`、`static/`、`render.go`、`ui.go`、`store.go` 全部删除；`/` 302 → `/ui/` |
| CSP | `cmd/server/main.go:112` 下发 `default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'` |
| 内联样式 | `style=` / `dangerouslySetInnerHTML` 在 `web/src/**/*.tsx` 中 **0 处**（CSP 真正可生效） |

## 2. 独立功能探测（只读 + 一次性探针，事后已清理）

| 探针 | 结果 |
| --- | --- |
| 关系字段提交字符串（`POST device-types {"domain_id":"1"}`） | 200，落库为 `1` —— 前端 `toSubmitValue` 输出字符串，pgx 能编码，**不是 bug**（我先怀疑过） |
| 同字段提交数字 | 200，一致 |
| 多值 `in` 筛选 `filter.x=1&filter.x=2` | 服务端 `internal/admin/query.go` 的 `parseFilters` 对 `in` 汇集全部同名参数再逗号拆分，与前端"重复参数"序列化**一致** |
| 探针清理 | `DELETE /admin/v1/device-types/2`、`/3` 均 200，测试库无残留 |
| 列表 `?pageSize=2` 响应 | `data` 仍是数组 + `meta{total,page,pageSize,sort,order}`，旧消费者不破 |

## 3. 规范硬约束抽检

| 条款 | 判据 | 结果 |
| --- | --- | --- |
| `table-query` §1/§2 | 前端不做本地过滤/排序/分页 | `grep rows\.(filter\|sort\|slice)` 仅命中 `web/src/test/`，生产代码 0 |
| §6 | 分页回第一页 | `queryState.ts:112-124` 的 `patchQuery` 对 q/filters/sort/order/pageSize 变化强制 `page=1` |
| §6 | 范围与总数 | `EntityListPage.tsx:32-35` 输出「第 a–b 条，共 N 条」 |
| §5 | 已生效条件外显 | `ActiveFilterChips`（`aria-label="已生效筛选条件"` + 单删 + 清除全部） |
| §5 | 常驻筛选 ≤5 | `fieldMeta.ts` `quickFilters` = `filterable && !advancedOnly` 取前 N，其余进「更多筛选」Dialog |
| §9 | 列宽/截断 | `DataTable.module.css:10` `table-layout: fixed` + `<colgroup>` 按 `widthStrategy` 取类名；`text-overflow: ellipsis` + `title` |
| §17 | 抽屉可访问性 | Radix `Dialog` 提供 `role=dialog`/`aria-modal`/焦点陷阱；`aria-describedby` 显式置空防误读 |
| §18 | 禁止清单 | `!important` 0、`tokens.css` 之外无颜色字面量（`tokens.test.ts` 用例守）、无相对时间 |
| §16 | 四态文案 | 加载/无数据/无匹配/失败四段文案互不相同，失败含原因 + 修复路径 |

## 4. 本轮新发现（REVIEW-002 未记录）

| # | 位置 | 问题 | 条款 | 建议 | 严重度 |
| --- | --- | --- | --- | --- | --- |
| F1 | `web/src/pages/EntityListPage.tsx:159`（`closeDrawer`） | 关闭抽屉用 `replace:false`（push），保存却用 `replace:true`。Esc/取消后按浏览器返回会**重新打开抽屉**，历史翻倍且行为不一致 | §8 返回语义、§10 状态保持 | 关闭统一 `{replace:true}`，或 open 用 push + close 用 `navigate(-1)` | 建议 |
| F2 | `web/src/components/Pagination.tsx:32` | 页超范围只给文案「请返回有效页」，未按规范自动修正 | `table-query` §6 | `total>0 && rows.length===0` 时自动 `patchQuery({page:1})` 并轻提示「已回到第一页，筛选条件保留」 | 建议 |
| F3 | `web/src/components/DataTable.tsx:137-139` | `<caption>` 常显且文案「排序与分页由服务端执行」是开发者语言，对运维零信息量 | §16 文案 | 改为「第 X 页，共 Y 条」或转 `visually-hidden` | 建议 |
| F4 | `internal/admin/options.go`（`MaxOptionRows=1000`）+ `RelationSelect` | 终端/类型超过 1000 行时下拉取不全且无远程搜索，建质量规则时可能选不到终端 | `table-query` §4（关系字段远程搜索、选项可搜索） | 下一期给 `options` 加 `q` 过滤 + 前端远程搜索；当前数据量不触发 | 重要（容量边界） |
| F5 | 仓库根 | 「产物可复现」只靠人工执行 `npm run build && git diff --exit-code internal/ui/dist`，没有 CI/脚本兜住，下次有人改了 `web/src` 忘记重建就会静默漂移 | R-016 | 加 `Makefile` 目标（`web-build` + `dist-check`），CI 里调用 | 建议 |

## 5. 放行判断

- 阻断项 0；`F4` 属容量边界（>1000 行才触发），本期数据量不兑现，记为下一期任务输入；`F1/F2/F3/F5` 均为体验与工程护栏，不影响正确性。
- `b82a788` 的 503 口径与 `R-014` 一致：写失败返回 `internal_error`、异常仍记 `cache_write_failed`、`http.go` 判定未动。
- `internal/admin` 三端点与 `task-004 §4` 契约逐项对上；`admin.Entities` 仍为 6，异常走独立路由，`nongoal_test.go` 未改。

**结论：rv-07 可作为基线继续推进；F1–F5 进下一轮任务输入，不返工。**
