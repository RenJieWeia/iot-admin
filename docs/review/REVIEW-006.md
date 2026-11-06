# REVIEW-006 · rv-09 复核（editable/required + lazy + vendor + R2）

- `basis:` `rv-08..HEAD`：`cf5bb41`（后端契约）+ `8df86ca`/`8e985ae`/`0607b6d`/`6f1c595`（前端四项）+ `9f6a214`（报告）
- `verdict:` **放行**，同意打 `rv-09`
- `method:` 独立复跑命令 + 线上实例 CDP 实测（1440×900，`Emulation.setDeviceMetricsOverride`）

## 1. 范围与红线

| 检查 | 结果 |
| --- | --- |
| 越界 | `internal/admin/**`（后端契约）+ `web/**` + `internal/ui/dist` + `docs/review/report-004.md`，无越界 |
| `go.mod`/`go.sum`/`migrations`/`nongoal_test.go` | 零改动 |
| 产物 | `dist` 11 个文件（入口 + 5 个 vendor + 2 个页面 chunk + 2 份 CSS），`npm run build && git diff --exit-code` = 0 |

## 2. 命令复核

```
$ npx vitest run                              → 6 files / 34 tests passed
$ npx stylelint "src/**/*.css"                → 0
$ npx tsc --noEmit                            → 0
$ go build ./... && go vet ./...              → 干净
$ go test ./... -count=1                      → 无 DSN 8 包全绿
$ IOT_TEST_DSN=… go test -p 1 ./... -count=1  → 全绿（admin 25 用例、ingest 2.37s）
```

## 3. 线上实例 CDP 实测（`0.0.0.0:8084`，切到含新契约 + 新产物的构建）

| 项 | 实测 |
| --- | --- |
| 入口资源 | `index-C8nsmkdP.js` + `vendor-react/tanstack/router` + `index-lzzIBcF6.css`；服务端引用哈希 == 仓库 `dist` ✓ |
| 布局 | `main x=200 w=1240`、`table x=225 w=1190`、4 行 ✓ |
| 路由级 lazy | 导航到 `/ui/terminals` 后网络记录出现 `EntityListPage-BGDG33kM.js`（真按需加载）✓ |
| R2 多选移入更多筛选 | 打开「更多筛选」后出现 `终端类型` 控件；常驻区不再有 ✓ |
| 抽屉（`?drawer=new`） | 标题「新建终端」、5 个控件、必填标记 3 个：`终端 ID（必填）/终端类型（必填）/网关 ID（必填）` —— 与后端 `_meta` 的 `required` 完全一致 ✓ |
| 异常只读页 | 无 `data-action=new`、0 个 `data-action=edit`（`editable=false` 生效）✓ |
| 控制台 | **CSP 违规 0 / console error 0** ✓ |

截图：`.devcache/shots/rv09-live.png`。

## 4. 性能事实（对方提供 + 我核对 dist 结构一致）

| 阶段 | 首屏 raw | 首屏 gzip | 累计 gzip |
| --- | --- | --- | --- |
| rv-08 单 chunk | 529,357 | 162,114 | 162,114 |
| + 路由级 lazy | 382,252 | 118,085 | 164,451 |
| + vendor 拆分 | 384,179 | 119,382 | 165,864 |

首屏 −27.4% raw / −26.4% gzip，最大单文件 517.6 KB → 223.6 KB；累计 gzip +2.3%（拆包丢失跨 chunk 压缩共享），如实记录。**取舍**：`vendor-misc`（12.8 KB）不并回入口——并回省不到 3 KB gzip，却会破坏“改动业务代码不失效 vendor 缓存”的收益，维持现状。

## 5. 遗留

| # | 项 | 处置 |
| --- | --- | --- |
| R3 | 有 DSN 时 `go test ./...` 默认并行跑包会互踩同库 | **仍未修**；验收统一 `-p 1`（本轮两次验收都用了），需单独立任务书做测试库隔离 |
| — | `task-005` 中的 `R2` | 已完成（本节 §3 实测） |
| — | `editable`/`required` 契约 | 已完成（后端 `cf5bb41`，前端 `8df86ca`，线上实测一致） |

## 6. 结论

四项需求（契约接入、路由级 lazy、vendor 拆包、R2）全部落地且可复现；lazy 的四态判据未被放宽（`Set.size===4`、恰好 10 行、文案逐字保留）。**同意 `rv-09`**。
