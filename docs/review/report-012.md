# report-012 · task-012 F4 后端（options 远程搜索 + 精确批量取）

> **范围声明**：本报告**只覆盖 F4 的后端部分**（`internal/admin/options.go`）。
> F2（分页自动修正）、F3（表格 caption 文案）、F5（`Makefile`）以及 F4 的**前端 combobox**
> （`web/src/components/RelationSelect.tsx`、`web/src/api/entities.ts`）均由 `fe-web` 交付，
> 不在本报告内。

- `task:` task-012 第 3 步（`docs/review/task-012.md` §F4）
- `baseline:` `rv-16`（`f22be73`）
- `commits:`
  | 内容 | hash |
  |---|---|
  | F4 后端 + 用例 | `55c879d` |
  | 本报告 | 见 `git log -- docs/review/report-012.md` |
- `范围:` 只改 `internal/admin/{options.go,options_test.go}`；未动 `web/**`、`Makefile`、
  `internal/{ingest,settle,validate}`、`admin.Entities`、迁移、`nongoal_test.go`

## 1. 变更说明

`GET /admin/v1/options/{entity}` 在保留「无参 = 全量 ≤1000」兼容行为的前提下，增加两种取法：

| 参数 | 语义 | 约束 |
| --- | --- | --- |
| `ids=1,2,3` | 按 id 精确取 label（表格列回填） | ≤200 个（按**去重后**的 distinct id 计）；忽略 `q`/`limit`；不存在的 id 直接跳过；**返回顺序 = 请求顺序** |
| `q=<关键词>` | label `ilike` 模糊搜索（下拉远程搜索） | ≤200 **字符**（rune，中文算 1）；`limit` 默认 50、上限 1000（超出静默收敛） |
| 无参 / `q=` 空串 | 维持现状：全量，上限 1000 | 兼容既有调用 |
| `ids` + `q` 同时 | 400 `invalid_query`（「ids 与 q 互斥」） | — |

- 返回结构不变：`{"code":0,"message":"ok","data":[{"value":"7","label":"泵站"}]}`，`value` 仍是 `id::text`。
- 未知实体仍 404 `unknown_entity`。
- 搜索模式串经既有 `escapeLike` 转义 `% _ \`，避免关键词被当成通配符放大成全表扫描。
- 实现分三支：`parseOptionQuery`（校验 + 三选一）→ `runOptionQuery` →
  `listOptionsByIDs` / `searchOptions` / `listOptions`；SQL 列表达式继续复用 `optionLabelExpr`
  （`name → code → id` 降级链），列名全部来自 `Entity.Cols` 白名单。

## 2. 测试结果

### 2.1 干净 worktree（`git worktree add .devcache/wt-t12 55c879d`）

```
$ git rev-parse --short HEAD
55c879d
$ git status --short
(空=干净树)

$ go build ./... && go vet ./...
(无输出)

$ go test ./... -count=1   # 无 DSN
ok  cmd/server 0.008s | internal/admin 0.016s | internal/clean 0.005s | internal/ingest 0.010s
ok  internal/meta 0.005s | internal/staging 0.005s | internal/ui 0.007s | internal/validate 0.005s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1
ok  cmd/server 0.007s | internal/admin 0.240s | internal/clean 0.003s | internal/ingest 3.453s
ok  internal/meta 0.004s | internal/staging 0.003s | internal/ui 0.006s | internal/validate 0.003s
```

### 2.2 定向用例

| 用例 | 覆盖的验收项（task-012 §7） | 结果 |
| --- | --- | --- |
| `TestOptionsQueryValidation` | `ids+Q` → 400；`ids` 201 个 → 400；`q` 201 字符 → 400；`ids` 全空 → 400；未知实体 → 404；恰好 200 个通过；`q=` 空串等同无参（`Q=""`、`limit=1000`）；`limit=99999` 收敛到 1000 | PASS（无 DSN） |
| `TestOptionsQueryWithDB` | 请求 `[丙站, 999999999, 甲站]` → 返回 2 项且顺序为 `[丙站, 甲站]`（跳过不存在）；`q=乙站` 只命中 1 项；`q=站&limit=1` 只回 1 项；`q=` 空串返回全量（≥3 项） | PASS（有 DSN） |

## 3. 决策与已知限制

1. **`ids` 个数按去重后计**：重复 id（如 201 个相同 id）不算超限，避免前端拼串时被误拒。
2. **`q` 长度按字符计**：中文关键词 200 个汉字仍可提交（字节数会是 600）。
3. **`limit` 只对 `q` 生效**：无参时忽略 `limit`（保持「全量 ≤1000」的既有语义）。
4. **表格列回填不走远程搜索**（书里已列）：列表页仍用全量 options（≤1000），超过 1000 行时列显示原始 id，
   属已知限制；`ids` 分支已就绪，将来切远程回填不需要再动后端。
5. **`q` 无索引**：label 是 `coalesce(...)` 表达式，`ilike` 走全表扫。当前 6 实体量级（`data_domain`/
   `device_type`/`terminal` 等）可控；规模化前需要表达式索引，属容量项。

## 4. 风险

1. `q` 搜索在超大表上是全表扫（见 §3.5）。
2. `ids` 一次最多 200 个，超过即 400——前端批量回填需要自行分批。
3. 三者（无参/ids/q）共用同一响应结构，前端无法从响应判断走的是哪条路径；如需区分，应看请求而非响应。

## 5. 未闭环

- 无新增 `Q-NNN`。
- F4 前端（combobox 防抖 + 键盘 + 空态）与 F2/F3/F5 由 `fe-web` 交付，本报告不覆盖。
