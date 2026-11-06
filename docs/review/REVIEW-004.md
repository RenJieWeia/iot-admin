# REVIEW-004 · 浏览器渲染层审核（补做，推翻 REVIEW-002/003 的放行依据）

- `basis:` `rv-07`（`4abaccd`）+ 实跑实例 `127.0.0.1:8083`（本机 PG + 测试实例 broker）
- `verdict:` **rv-07 不可放行** —— 浏览器层发现 1 个 P0 布局缺陷 + 1 个重要 CSP 缺陷
- `method:` 无头 Chromium（`/usr/bin/chromium`）+ CDP：1440×900 视口，读 `getBoundingClientRect` / `getComputedStyle` / console+Log 事件，截图与 DOM 存 `.devcache/shots/`
- `关系:` `REVIEW-002`（我）、`REVIEW-003`（reviewer）都只做了 HTTP 层 + 读码，结论"放行"**在渲染层不成立**；本文件补充该维度，不改写已打的 tag

## 0. 先更正我自己

我第一张截图（`terminals.png`）是在默认 800px 视口拍的，命中 `@media(max-width:1024px)` 的 `grid-template-columns:1fr`，我据此误判成"响应式断点问题"——**该判断撤回**。真正的问题是下面 F1，在 1440 视口下同样存在，只是表现不同。

## 1. F1（阻断）skip 链接缺 CSS 类，整页 grid 被重排

**实测（1440×900，`/ui/terminals`）**

| 元素 | 实测 | 应有 |
| --- | --- | --- |
| `<main class="_main_wnt95_69">` | `x=0, y=484, w=200` | `x=200, y=0, w=1240` |
| `<table>` | `x=25, w=1020`（溢出 200px 容器被裁切） | 内容区满宽 |
| `<aside>` | 占满第 1 行整宽 | 200px 首列 |

**根因**：`AppShell.tsx:23` 的 `<a className={styles.skipLink}>` 在 `AppShell.module.css` 中**没有对应 `.skipLink` 规则**（构建产物 CSS 里也搜不到）→ `styles.skipLink === undefined` → 该 `<a>` 无 class、非定位元素，作为普通 grid item 占了 `.shell` 首格，把 `<aside>` 挤到第 2 列、`<main>` 挤到第 2 行第 1 列。

**违反**：通用 §17（跳转链接只在聚焦时可见）、§6（内容区塌陷 + 表格裁切而非横向滚动）、§18（表格压缩列宽/裁切）。

**修复**
1. `AppShell.module.css` 补 `.skipLink`：`position: absolute; top: var(--space-2); left: -9999px;`，`:focus-visible` 时回到 `left: var(--space-2)`（定位元素不参与 grid）。
2. 防御性约束：`.sidebar { grid-column: 1; }`、`.main { grid-column: 2; }`。
3. 护栏：加一条单测——`*.module.css` 导出的类名集合 vs TSX 里 `styles.<key>` 引用集合比对（可捕获本类缺陷）；再加浏览器冒烟断言 `main.getBoundingClientRect().x >= 200`。

## 2. F2（重要）CSP 拦截 Radix 注入的内联样式

打开抽屉时 console：`Applying inline style violates … 'default-src 'self'' … The action has been blocked.`

抽屉当前仍定位正确（`position: fixed; inset: 0 0 0 960px`，`hasInlineStyle=true`），说明拦截的是 Radix 的部分注入样式（滚动锁/焦点守卫一类），属**不确定行为**。`REVIEW-001` F1 的"去掉内联 style 让 CSP 真生效"只覆盖了业务代码，没算第三方 primitive。

**修复（推荐 A）**
- A：CSP 显式放开样式内联、脚本保持收紧：`style-src 'self' 'unsafe-inline'; script-src 'self'`（其余不变），报告里声明取舍。
- B：保留严格 CSP，改 Radix 用法（`modal={false}` + 自绘遮罩/滚动锁），成本更高。
- 验收：抽屉开合全程 console 无 CSP 违规。

## 3. F3（建议）favicon 404

每次加载 `GET /favicon.ico → 404`（console 可见）。补 `web/public/favicon.svg` + `<link rel="icon">`，或显式声明接受。

## 4. 浏览器层已通过的部分

| 项 | 结果 |
| --- | --- |
| React 挂载 | `#root` 渲染完成，`document.title = "终端 · IoT 管理后台"`，无未捕获异常 |
| 表格 | `table-layout: fixed`、`<colgroup>` 生效（首列 180px）、表头含排序箭头、`<tbody>` 4 行 |
| 分页 | 「第 1–4 条，共 4 条」 |
| 导航 | 8 项、当前项高亮 |
| 抽屉 | `role="dialog"`、标题「新建终端」、「取消/保存」、Portal 到 body、右侧 480px |
| 深链 | `?sort=name&order=asc&pageSize=2`、`?drawer=new`、`/ui/exceptions/events` 均直达且状态由 URL 还原 |

## 5. 处置

- `rv-07` 保持不动（不改写历史）；F1 修复后单独提交并打 `rv-08`。
- 修复必须附浏览器层证据：1440 视口下 `main.x === 200` 且 `main.width === 1240`、表格宽度等于内容区、抽屉开合 console 无 CSP 违规、截图一张。
- 我方（Lead）后续审核流程增加固定一步：**HTTP 层通过 ≠ 放行**，必须跑一次无头浏览器渲染检查（本文件的 CDP 脚本可复用）。
