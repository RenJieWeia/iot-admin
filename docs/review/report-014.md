# report-014 · 视觉重构（四步）

- `task:` task-014（`Decision: R-036` 视觉基线 v2）
- `baseline:` `rv-17`（`84b9f5a`）
- `commits:` `899832b`（步 1 令牌）、`b9e372b`（步 2 外壳）、`a7f2cdf`（步 3 表格）、`c43f8fb`（步 4 浮层）
- `范围:` `web/**` + `internal/ui/dist`；唯一新增依赖 `lucide-react`
- `截图:` `同角度重拍在 `.devcache/shots/v2/{u-list,u-events,u-drawer,u-readonly}.png`；对照基线 `.devcache/shots/u-*.png`（审计取证图）

## 1. 按步变更

| 步 | 内容 |
| --- | --- |
| 1 | `tokens.css` 全量替换为 v2（表面三档 / 文本三档 / success·warning·danger·info·neutral 语义色 + weak 底 / 四级字号配行高 / radius-control·card·pill / shadow-card·overlay / control-h 32·row-h 44·nav 220·drawer 520）；`tailwind.css` 只做 `@theme inline` 映射；另加一段**显式标注的迁移期别名**把 v1 名字（font-sm/radius-sm/positive-* 等）指向 v2，避免本步就重写 23 个文件 |
| 2 | `AppShell` 重构：220px 侧栏（surface-2、分组小标题 11px/0.04em/text-faint、条目 36px、lucide 图标 16px、选中 primary-weak + 左 2px 主色条）+ 56px 顶栏（标题/副标题 + 右侧主操作 + 底部 1px）+ 内容白卡（radius-card + shadow-card） |
| 3 | `DataTable`：可横向滚动容器、首列 sticky-left、操作列 sticky-right、表头 surface-2、行高 44、行 hover；`format.ts` 时间改 `MM-DD HH:mm:ss` + title 全值、编码列 mono；`StatusTag` 收敛五档（v1 tone 在组件内映射）；编辑=描边、删除=危险文字按钮；`FilterBar` 栅格 + 32px 控件 + 更多筛选同排右对齐 + 日期文本输入（YYYY-MM-DD 占位、无原生日历）+ 数字去 spinner；`Pagination`/`CursorPager` 整组右下 |
| 4 | 抽屉遮罩加深（令牌）、宽度 520、浮层 z-40（修 sticky 列穿透）；必填红星 + 图例；布尔改 switch；只读页提示语改副标题（含「时间以 +08:00 显示」，全页只出现一次） |

## 2. 审计 17 条逐条勾选

| # | 问题 | 修复步 | 证据 |
| --- | --- | --- | --- |
| A1 | 侧栏与内容无层级 | 2 | 侧栏 surface-2 + 白卡，`v2/u-list.png` |
| A2 | 灰阶只 3 档 | 1 | 令牌扩到 text/muted/faint/border/border-strong 五档 |
| A3 | 标题正文字号差 | 1 | 20/16/14/12 四级 + 行高 |
| A4 | 无顶栏 | 2 | 56px header（标题/副标题 + 主操作） |
| B1 | 表格右侧列被裁 | 3 | 容器横向滚动 + 首列 sticky-left；`v2/u-events.png` 8 列可见、底部出现滚动条 |
| B2 | 筛选控件宽度/间距不一 | 3 | `filter-grid`（auto-fit minmax 200px）+ 32px 控件 + 标签 12px/6px |
| B3 | 更多筛选独占一行 | 3 | 与筛选行同排、右对齐 |
| B4 | 页面下方空白 | 2 | 内容白卡撑满主区域宽度、`min-h` 主区（未强行拉伸表格） |
| B5 | 分页器左右分离 | 3 | 整组右下：范围 + 页码 + 上下页 + 每页条数 |
| C1 | 原生日期控件痕迹 | 3 | 改文本输入（占位 YYYY-MM-DD），无 mm/dd/yyy、无双日历图标 |
| C2 | number spin button | 3 | `number-input` 隐藏 spinner |
| C3 | 编辑/删除重量倒挂 | 3 | 编辑=描边 32px；删除=文字按钮，hover 才变危险色 |
| C4 | 状态色语义重叠 | 3 + 修订 | 五档 tone：写入失败=danger、协议错误/迟到数据=warning、启用=success、停用=neutral。后端 `eventTypeOptions` 把 `protocol_error` 标成 negative，任务书不许改 `_meta`，因此在 `lib/format.ts` 增加**展示层** key+value 映射（`protocol_error→warning`、`cache_write_failed→negative`、`late_data→warning`），其余沿用后端 tone |
| C5 | 遮罩太浅、关闭按钮孤悬 | 4 | 遮罩 rgba(16,24,40,.45)；关闭按钮与标题同组 |
| C6 | 必填靠文字、布尔裸复选框 | 4 | 红星 + 图例；布尔 switch |
| D1 | 时间列重复 +08:00 | 3 | 单元格 `MM-DD HH:mm:ss`，完整值进 title，时区说明只在副标题出现一次 |
| D2 | ID/时间与正文同字体 | 3 | id 类型 + 终端/数据项/网关编码列 `font-mono` |
| D3 | 侧栏无图标、分组标题弱 | 2 | lucide 图标 + 11px/0.04em 分组标题 + 选中左条 |
| D4 | 只读页提示悬右侧 | 4 | 改为标题下副标题 |

## 3. 测试与构建

```
$ cd web && npx tsc --noEmit            → 无输出
$ npx stylelint "src/**/*.css"          → 无输出
$ npx vitest run                        → 9 files / 61 tests 全绿
$ npm run build && git diff --exit-code internal/ui/dist → 0
```

**RTL 断言改动（仅两处，均为显示格式变化，未放宽强度）**：

| 用例 | 改前 | 改后 |
| --- | --- | --- |
| `最新值只读页 …` 时间断言 | 单元格匹配 `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \+08:00` | 单元格匹配 `^\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`，另加 title 匹配完整 `YYYY-MM-DD hh:mm:ss +08:00` |
| `required:true 的字段阻止空提交…` | `getByLabelText('终端 ID（必填）')` | `getByRole('textbox', { name: /终端 ID/ })` + 星号计数（`*` 共 2 个：字段 1 + 图例 1） |

## 4. 偏离与说明

| 类型 | 内容 |
| --- | --- |
| Assumption | 日期输入改为文本框后失去浏览器日历面板，换取「无原生痕迹」；如需保留选择器需另做自定义日历 |
| Assumption | 布尔 switch 仍表达三态（空值 = indeterminate 视觉），与既有 `_meta` 语义不变 |
| Deviation | 迁移期别名保留在 `tokens.css` 末尾（v1 名 → v2），后续清理只删这一段 |
| Deviation | 时间/必填两处 RTL 断言因显示格式变化而更新（见 §3 表），行为断言强度未降 |
| 未做 | B4 只做「白卡 + 主区撑满」，未把表格拉满视口高度（会与分页固定位冲突），如需再议 |
| 修订 | 复核发现两处与书不符，已补提交：① C4 只落了一半（协议错误仍是 danger）→ 前端展示层映射；② 数值范围右侧输入框 placeholder 误写成「最小值」→ 改为「最大值」 |

## 5. 风险

| 风险 | 说明 |
| --- | --- |
| 迁移期别名 | v1/v2 令牌名共存，误用 v1 名字仍会生效；清理时需一次性替换并跑四张截图回归 |
| sticky 列 | 首列与操作列都 sticky，横向滚动时两侧同时固定；列很多时中间可视区变窄，属预期 |
| 日期输入 | 手输格式无前端校验，非法值会由后端 `invalid_query` 拒绝并在失败态展示 |
| switch 可访问性 | 星号 `aria-hidden`，必填语义只靠图例文字；需要读屏时可补 sr-only「必填」 |
| 浅色主题 | 本轮未做 dark mode（R-013 非目标），令牌已按语义命名，后续可扩展 |
