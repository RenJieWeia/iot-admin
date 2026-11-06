# REVIEW-010 · rv-14 复核（task-009 Part A + A/B 联调）

- `basis:` `rv-13..6383647`（`13ba869` 代码 + `6383647` 报告）+ 实跑实例 `0.0.0.0:8084`（`rv-14` 构建，本机 PG）
- `verdict:` **放行**，打 `rv-14`
- `method:` 干净 worktree 跑测试 + 真机 `curl` + 无头 Chromium CDP（1440×1200）

## 1. 干净 worktree 复核（`6383647`）

```
$ git status --short → 空
$ go build ./... && go vet ./...        → 无输出
$ go test ./internal/admin -run 'TestReadonly|TestMetaReadonly|TestMetaSections|TestMetaFieldShape' → PASS
$ go test ./...（无 DSN）               → 8 包全 ok
$ IOT_TEST_DSN=… go test -p 1 ./...     → 8 包全 ok
```

## 2. 真机契约实测（`/admin/v1/_meta`）

```
exceptions: ['events', 'intervals']                      ← 未变
readonly:   [('latest-values', '最新值', 8)]             ← 新增 1 项，8 字段
quality:    number, ops=[gte,lte], options=8 项
            ['正常','越界','规则命中','越界+规则','单位闭集外','越界+单位闭集外','规则命中+单位闭集外','越界+规则+单位闭集外']
entities:   6                                            ← 未变
```

端点行为：
- `GET /admin/v1/readonly/latest-values?pageSize=3` → 200，`meta{total:2,page:1,pageSize:3,sort:value_time,order:desc}`。
- `filter.quality.gte=1` → 2 行（`power=1`、`temp=3`）；`filter.quality.lte=0` → total 0。**未新增 SQL 操作符**，复用 `gte/lte`。
- `GET /admin/v1/readonly/nope` → **404 `unknown_slot`**；`/admin/v1/exceptions/events` → 200（旧路由未破坏）。

## 3. A/B 联调（CDP，1440×1200）

| 检查 | 结果 |
| --- | --- |
| 布局 | `main.x=200`、`main.width=1240`、`viewport=clientWidth=1440`（无滚动条口径） |
| 导航 | 「最新值」出现在导航；`/ui/readonly/latest-values` 直达 200 |
| 表格 | 8 列、2 行；`quality` 渲染为状态标签：`越界` / `越界+规则`；`quality_reason` 单行 + `title="source=thing_model item=power 低于下限"` |
| 时间 | 绝对时间 `2026-11-05 11:10:00 +08:00` |
| 只读 | `[data-action]` 0 个、`role=dialog` 0 个、**写请求 0 条** |
| 快捷筛选 | 勾选「仅看有问题（质量 ≥ 1）」→ URL `?filter.quality.gte=1`、条件标签 `质量≥1`、行数仍 2（两行都 ≥1） |
| 控制台 | CSP 违规 0 / console error 0 |

截图：`.devcache/shots/task009-readonly.png`。

**为联调造的种子数据**：`latest_value` 里 `power` 置 `quality=1`、`temp` 置 `quality=3`（带 `quality_reason`）。开发库因此能看到 2 行“有问题”数据；不影响任何断言（测试库与开发库分离）。

## 4. 结论

Part A 的契约与 Part B 的实现对得上，读路径从 MSW 变成真实后端后行为一致。**打 `rv-14`**。

遗留（不影响本批）：
1. `hourly_staging` / `hourly_snapshot` 只读页未做（task-009 §4 非目标，留到下一批）。
2. 本地 `1883` 上的 broker 无 provisioning，应用会按 task-005 的 fail-fast 退出；本次联调改用 `1887`（带 `CONFIG_FILE`）的实例，并把 `bromqup.sh` 的正确启动方式重申给运维。
