# REVIEW-009 · rv-13 复核（task-008 两段写入 + task-009 Part B）

- `basis:` `rv-12..7b0f8bb`：task-008 三提交（`0aa098e`/`acb8bf1`/`605a240`）+ 修复（`7b0f8bb`）+ task-009B 两提交（`c79e5bb`/`537e8f3`）
- `verdict:` **放行**，打 `rv-13`；窗口同时包含 task-008 与 task-009 Part B（时间线交错，任务书 A 部分待补）
- `method:` **只在 `git worktree add <提交>` 的干净树上取数**（工作区绿一律不算），外加定向用例与静态核对

## 1. 打回与修复（过程记录）

第一次复核在 `605a240` 干净树上稳定失败：

```
--- FAIL: TestWindowBoundaryIsClosedAtCloseTime
    settle_test.go:47: 关闭时刻结算行数 = 0，期望 2
```

根因：`Settler.Run` 返回的是 `UpsertSnapshot` 的**受影响行数**，而 R-030 后窗口外同 `value_time` 的数据已直写快照，结算时被 `WHERE excluded.value_time > …` 守卫跳过 → 0 行受影响。执行者报告声称"既有用例一行未改通过"，依据是**工作区**而非提交树（`settle.go` 的改动没进 `605a240`）。

修复 `7b0f8bb`：`settle()` 拆成 `(processed, affected, err)`，`Run` 返回 `processed`（本轮处理的暂存行数），`affected` 只进日志；`settle_test.go` 断言未改。

**流程改进（已生效于双方）**：报告里写"通过"只认干净 worktree 输出；提交一律 `git add <显式路径>`，禁用 `git add -A`/`.`/`commit -a`/裸 `git rm`（本批发生过一次共享 index 夹带，已 reset 修复）。

## 2. 独立复核（`7b0f8bb` 干净树）

```
$ git rev-parse --short HEAD → 7b0f8bb；git status --short → 空
$ go build ./... && go vet ./...                     → 无输出
$ go test ./internal/ingest -run TestWindowBoundaryIsClosedAtCloseTime → PASS
$ go test ./...（无 DSN）                            → 8 包全 ok
$ IOT_TEST_DSN=… go test -p 1 ./...                 → 8 包全 ok（ingest 2.785s）
```

`7b0f8bb` 只改 `internal/settle/settle.go`（+17/−10），`settle_test.go` 一行未动。

## 3. task-008 静态核对

| 约束 | 结果 |
| --- | --- |
| 快照写入唯一实现 | `grep "insert into hourly_snapshot"` 仅 `settle.go:156` 一处；结算与 `pipeline` 窗口外分流都调 `settle.UpsertSnapshot` |
| `revision` 原子递增 | `revision = hourly_snapshot.revision + 1, settled_at = now()` 在 `ON CONFLICT DO UPDATE` 内，配 `WHERE excluded.value_time >` 守卫 |
| 迁移 | `0006` 只加两列 + 一个索引；`0002` 注释同步 |
| 路由 | `GET /admin/v1/snapshot/changes` 注册在字面量路径，不影响 `{entity}` 路由；`admin.Entities` 仍 6 |
| 红线 | `nongoal_test.go`、`go.mod`、`migrations/0001-0005` 零改动 |

## 4. task-009 Part B 复核（前端）

- `c79e5bb` 只碰 `web/**` + `internal/ui/dist/**`，含 4 个旧哈希产物的删除（`emptyOutDir` 生效，`dist` 剩 12 个文件，无陈旧残留）。
- `npx vitest run` → **7 files / 40 tests passed**；`tsc` / `stylelint` 干净；`npm run build && git diff --exit-code internal/ui/dist` = 0。
- 只读约束：无新建/编辑/删除、不渲染 `<role=dialog>`、MSW 写处理器计数为 0。
- `quality` 用通用规则「字段带 options 即按状态标签渲染」，8 种组合文案全部来自后端。

**未验证**：真实实例联调——`/admin/v1/readonly/{slot}` 与 `_meta.readonly` 属 task-009 Part A，尚未实现，B 目前只对 MSW 生效。

## 5. 结论与后续

- 打 `rv-13`（`7b0f8bb` + 本复核提交）。
- **下一步是 task-009 Part A**（`be-api`）：`readonly` 只读源 + `_meta.readonly` + `latest-values` 字段元数据；落地后做 A/B 联调复核，再打 `rv-14`。
- 遗留：`Settler.Run` 返回值语义已变（`processed`），若有外部按该数值做阈值告警需同步——目前仅日志使用。
