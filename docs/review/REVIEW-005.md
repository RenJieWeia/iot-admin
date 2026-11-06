# REVIEW-005 · rv-08 复核（F1/F2/F3 修复 + 浏览器层复审）

- `basis:` `rv-07..HEAD`（`63b0daa` fix + `962cf21` report）
- `verdict:` **放行**，同意打 `rv-08`；另记 1 条测试基础设施缺陷（与本次修复无关）
- `method:` 独立复跑：无头 Chromium/CDP（1440×900，`Emulation.setDeviceMetricsOverride` 强制视口）+ 前端/Go 全量命令

## 1. 浏览器层复核（我自己跑的，不看对方报告）

| 指标 | rv-07 | rv-08 实测 |
| --- | --- | --- |
| `.shell` 列 | 被首格 `<a>` 挤坏 | `200px 1240px` ✓ |
| `main` rect | `x=0, y=484, w=200` | **`x=200, y=0, w=1240`** ✓ |
| `<aside>` | 占满整行 | `x=0, w=200` ✓ |
| `<table>` | `x=25, w=1020`（被裁） | **`x=225, w=1190`**，`table-layout: fixed` ✓ |
| skip 链接（未聚焦） | 常驻可见 | `left=-9999px`、`x=-9999` ✓ |
| skip 链接（真实 Tab） | — | `x=8`、`left=8px`、`:focus-visible=true` ✓ |
| 抽屉（点「编辑」打开） | 1 次 CSP 违规 | `position=fixed, x=960, w=480`、`body.overflow=hidden`、**CSP 违规 0 / console error 0** ✓ |
| Esc 后焦点 | `BODY` | **`BUTTON("编辑")`，`data-action=edit`、`data-row-id=T4`** ✓ |
| favicon | 404 | `/ui/favicon.svg` 200 ✓ |

截图（1440×900）：`.devcache/shots/rv08-verify.png`。

## 2. 命令复核

```
$ npx vitest run                              → 5 files / 26 tests passed
$ npx stylelint "src/**/*.css"                → exit 0
$ npx tsc --noEmit                            → 干净
$ npm run build && git diff --exit-code internal/ui/dist → exit 0（产物可复现）
$ go build ./... && go vet ./... && go test ./...         → 无 DSN 8 包全绿
```

`rv-07..HEAD` 越界文件 0；`go.mod`/`go.sum`/`migrations/`/`nongoal_test.go` 零改动。

## 3. 遗留（不拦 rv-08）

| # | 位置 | 问题 | 严重度 | 处置建议 |
| --- | --- | --- | --- | --- |
| R1 | 列表「操作」列（132px） | 「编辑」「删除」两个按钮在 1440 下换行错位，视觉不齐 | 建议 | 列宽放到 160px 或把「删除」收进行内下拉；顺带核对 `<colgroup>` 的 `colActions` |
| R2 | 原生 `multiple` 多选筛选（终端类型等） | 高度远大于同排输入框，筛选行参差（既有 §13.2 豁免，`fe-web` 已申报） | 建议 | 并入下一轮 `table-query` §13.2：自定义多选或挪进「更多筛选」 |
| R3 | **测试基础设施** | 有 DSN 时 `go test ./...` **默认并行跑包会互踩同一个库**：`internal/admin` 的 DB 用例与 `internal/ingest` 的 `truncate` 用例并发，出现 `TestBatchIsolatesFailingTerminal` / `TestAdminAPICRUD` 随机失败；`go test -p 1 ./...` 全绿（复现 2 次、串行 1 次全绿） | 重要（不影响发布，影响验收口径） | ① 验收命令改为 `go test -p 1 ./...`（写入 docs/test-env.md 与任务书）；② 另立任务书做测试库隔离（独立 schema/库名 per package），`Q-005` 升级为必办 |

## 4. 结论

F1（阻断）已消除并被我复测证实；F2/F3 达标；`fe-web` 额外修的「Esc 后焦点落回 body」在真实浏览器下成立（jsdom 测不出，属加分项）。**同意 `rv-08`**。R1/R2 属视觉打磨、R3 属测试口径，均不影响本次放行，但 R3 必须在下一本任务书里定死。
