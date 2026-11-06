# report-011 · 前端样式层迁移：Radix primitives + Tailwind CSS v4

- `task:` task-011（`status: READY`，`Decision: R-033 v2`）
- `baseline:` `rv-15`（`88e3b66`）
- `commits:` 4 个（步 4 与步 5 合并，理由见 §5）
- `范围:` `web/**` + `internal/ui/dist`；`internal/**`、`cmd/server`、`go.mod` 未改动

## 1. 五步落地

| 步 | 提交 | 内容 | 验收 |
| --- | --- | --- | --- |
| 1 | `5d8b507` | 装 `tailwindcss`/`@tailwindcss/vite`、`clsx`/`tailwind-merge`、CVA、Radix 补充件；加 Vite 插件；`styles/tailwind.css`（`@theme inline` 映射 + 不引 preflight）；`lib/cn.ts` | `npm run build` 通过；**CDP 四图对比 AE=0**（仍全 Modules） |
| 2 | `7273881` | `StatusTag`（CVA 变体）、`Skeleton`、`StatePanel`、`Toast`、`Pagination`、`CursorPager` → 工具类；删 `StatusTag.module.css`/`Skeleton.module.css` | vitest 50 用例；**AE=0** |
| 3 | `6015965` | `Drawer`→工具类、`ConfirmDialog`→Radix AlertDialog、`RelationSelect`→Radix Select、布尔字段→Radix Checkbox；顺手修 chunk 循环依赖（见 §5） | 键盘/Esc/焦点用例通过；`role=dialog`/`alertdialog` 不变；抽屉 AE=469（0.027%，仅控件外观） |
| 4+5 | `8b92f04` | `AppShell`/`DataTable`/`FilterBar` → 工具类；删余下 4 个 `.module.css`；`global.css` 拆 `@layer base`/`components`；lint 重写 | `git grep "\.module\.css" web/src` = 0；`dist` 可复现；bundle 对比见 §4 |

**未改**：`_meta` 契约、路由、查询语义、字段元数据。

## 2. 令牌映射表（`tailwind.css` 的 `@theme inline`）

| Tailwind 命名空间 | 映射来源（`tokens.css`） | 生成的工具类示例 |
| --- | --- | --- |
| `--color-*` | `--color-bg/surface/text/muted/border/primary/primary-hover/on-primary/positive-*/negative-*/warning-*/neutral-*/skeleton/overlay` | `bg-surface`、`text-muted`、`border-border`、`bg-primary` |
| `--text-*` | `--font-sm/md/lg/xl` | `text-sm`(12px)、`text-base`(14px)、`text-lg`(16px)、`text-xl`(20px) |
| `--leading-*` | `--line-md/lg` | `leading-md`、`leading-lg` |
| `--radius-*` | `--radius-sm/md` | `rounded-sm`、`rounded-md` |
| `--font-mono` | `--font-mono` | `font-mono` |
| 间距 | Tailwind v4 默认 `--spacing: 0.25rem` = 4px | `p-3`=12px、`gap-2`=8px、`h-12`=48px、`min-h-11`=44px |

**没有**定义 `--background`/`--card`/`--popover` 等第二套语义变量；颜色字面量仍只出现在 `tokens.css` 的 `:root`（lint 第 4 条守护）。

精确像素不进 TSX，收进 `@utility`：`state-text`(720px)、`range-text`(200px+tabular-nums)、`toast-box`(z-index/240/480/阴影)、`drawer-panel`、`confirm-panel`、`shell`、`skip-link`、`col-fixed/col-min/col-flex/col-actions`、`inherit-font`、`option-group-field`。

## 3. lint 重写与自证

`tokens.test.ts`（扫 CSS）与 `moduleClasses.test.ts`（扫 `styles.x`）已删除 —— 迁移后它们空转。
新增 `src/styles/style-rules.test.ts`，扫 `src/**/*.tsx`（提取 `className=` 与 `cn()` 里的字符串）与 `src/**/*.css`：

| # | 断言 | 实现 |
| --- | --- | --- |
| 1 | 颜色字面量不得出现在类名/`cn()` 参数/非 tokens 样式 | `#hex`、`rgb(`、`hsl(`、具名色（前后加边界避免误伤 `white-space`） |
| 2 | 禁 arbitrary value | `\[[^\]]+\]`，白名单仅 `data-[…]`/`aria-[…]`/`has-[…]`/`group-[…]`/`peer-[…]` |
| 3 | 禁 Tailwind 默认调色板 | `(bg\|text\|border\|ring\|…)-(slate\|gray\|…\|rose)-\d{2,3}` |
| 4 | 颜色令牌只在 `tokens.css` | 其它 CSS 里 `--color-*` 的**值**含颜色字面量即报错（`var(--color-x)` 映射放行） |
| 5 | 间距守 4px | `p-[Npx]`/`m-[Npx]`/`gap-[Npx]` 且 `N % 4 != 0` |
| 6 | 迁移完成 | 源码中不得再出现 CSS Modules 引用 |

**自证（临时插入后已移除）**：在 `Skeleton.tsx` 的 `cn()` 里加入 `bg-slate-100 p-[13px]`：

```
× 不使用 arbitrary value（Radix 变体选择器白名单除外）
× 不使用 Tailwind 默认调色板
× 间距 arbitrary value 必须落在 4px 网格上
Tests  3 failed | 3 passed (6)      ← 恢复后 Tests 6 passed
```

## 4. 构建产物前后对比（诚实列增减）

| 指标 | 迁移前 `rv-15` | 迁移后 | 变化 |
| --- | --- | --- | --- |
| 首屏 chunk 数 | 5（4 js + 1 css） | 5（4 js + 1 css） | 0 |
| 首屏 raw | 386,188 B | 441,569 B | **+14.3%** |
| 首屏 gzip | 119,873 B | 139,050 B | **+16.0%** |
| 全部 chunk 数 | 12 | 11 | −1（`vendor-misc` 合并，见 §5） |
| 全部 raw | 536,470 B | 634,675 B | **+18.3%** |
| 全部 gzip | 169,119 B | 201,314 B | **+19.0%** |

原因：Radix Select/AlertDialog/Checkbox 新增约 51 KB（`vendor-radix` 27.6→78.4 KB）、`vendor-react` 223.6→246.7 KB（React 19 + 新包的副作用）、工具类名进入 JSX 后入口 chunk 22.4→50.6 KB。**这是换样式载体的代价，不是回归**；若需回收，可优先把 `zod` 从抽屉路径按需加载（当前 `vendor-zod` 82 KB 只在抽屉里用到）。

## 5. 偏离与假设

| 类型 | 内容 |
| --- | --- |
| Deviation ① | **步 4 与步 5 合并为一个提交**：删除 `.module.css` 与组件替换必须同时完成，中间态编译不过，无法给出「独立可回滚的一半 Modules 一半工具类」版本 |
| Deviation ② | `vendor-misc` 分组取消：新增的 `clsx`/CVA/floating-ui 落入 `vendor-misc` 后会先于 `vendor-react` 求值，运行时 `P.useLayoutEffect` 读 undefined 导致白屏；改为未命中的依赖跟随引用方进 chunk |
| Assumption | `@utility` 只承载精确像素与结构布局；业务样式一律用指向令牌的工具类 |
| Assumption | Radix Select 的「未设置」用哨兵值 `__empty__` 映射回空串（Radix 不允许空字符串选项） |
| Assumption | 布尔字段空值渲染为 indeterminate，交互后写 `true`/`false`（本批布尔字段均 NOT NULL） |
| Open-Q | 是否需要把 `zod` 移出首屏（见 §4 末尾），等下一轮决定 |

## 6. 视觉回归（CDP，1440×1200，`Emulation.setDeviceMetricsOverride`）

以迁移前同一实例的四张截图（terminals / latest-values / hourly-snapshot / 抽屉打开态）为基线：

| 截图 | AE（不同像素） | 结论 |
| --- | --- | --- |
| 步 1 后四图 | **0 / 0 / 0 / 0** | 引入 Tailwind 本身零漂移 |
| 步 2 后四图 | **0 / 0 / 0 / 0** | 低风险组件替换零漂移 |
| 步 3 后 | 0 / 0 / 0 / **469** | 抽屉内布尔 select→checkbox、关系下拉触发器外观 |
| 最终 | 17,925 / 24,033 / 35,500 / 6,880（≈1–2%） | **差异仅限文字抗锯齿**：diff 图中边框、背景、列宽、行高均无变化；CDP 几何实测与迁移前一致 |

几何实测（最终）：

| 页面 | `main.x / w` | 滚动条 | `table.x / w` | 行高 | 列宽 |
| --- | --- | --- | --- | --- | --- |
| terminals | 200 / 1240 | 0 | 225 / 1190 | 48 | `[180,180,180,370,120,160]` |
| latest-values | 200 / 1240 | 0 | 225 / 1305 | 48 | 与迁移前一致 |
| hourly-snapshot | 200 / 1240 | 0 | 225 / 1823 | 48 | 与迁移前一致 |

console 无异常、无 CSP 违规；三页均无写入口。

## 7. 测试与复跑

```
$ cd web && npx vitest run
 Test Files  7 passed (7)
      Tests  50 passed (50)

$ npx tsc --noEmit                → 无输出
$ npx stylelint "src/**/*.css"    → 无输出，exit 0
$ npm run build && git diff --exit-code internal/ui/dist → exit 0
$ git grep -c "\.module\.css" -- web/src → 0 处命中
$ GOCACHE=/tmp/gocache go build ./... && go test ./... -count=1
  cmd/server、internal/{admin,clean,ingest,meta,staging,ui,validate} 全绿
```

## 8. 风险

| 风险 | 说明 |
| --- | --- |
| 产物体积 +14%（首屏 gzip +19 KB） | Radix 三个新 primitive 与工具类字符串造成；见 §4 的回收建议 |
| 类名审计 | 工具类散在 TSX 中，靠 lint 5 条规则守；新增 Radix 变体选择器时需确认白名单 |
| Tailwind v4 的 layer 优先级 | 未分层 CSS 会压过工具类；`global.css` 已拆进 `@layer base/components`，后续新增全局样式必须同样分层 |
| `@utility` 膨胀 | 精确像素集中在此，若继续增加需及时抽成令牌 |
| JSX 类名可读性 | 长行类名靠 `cn()` 组织；表单/表格是重灾区，后续改动需保持工具类顺序（`twMerge` 处理冲突） |
