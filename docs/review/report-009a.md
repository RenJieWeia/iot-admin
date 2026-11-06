# report-009a · quality 读路径（latest-values 只读源 + `_meta.readonly`）

- `task:` task-009 Part A（`docs/review/task-009.md` §2）
- `baseline:` `rv-13`（`5f4d8b2`）
- `commits:`
  | 内容 | hash |
  |---|---|
  | Part A 代码 + 测试 + design 契约行 | `13ba869` |
  | 本报告 | 见 `git log -- docs/review/report-009a.md` |
- `范围:` 只碰 `internal/admin/**` 与 `docs/design.md`；未动 `internal/ingest/**`、`internal/validate|clean|store|latest|staging|settle|publish|meta`、`web/**`、`internal/ui/**`、`cmd/server/**`、`migrations/`、`nongoal_test.go`、`go.mod`

## 1. 变更说明

### 1.1 通用只读页机制（`internal/admin/readonly.go`，新增）

- `readonlyPages` 与 `exceptionPages` 平级（结构复用 `ExceptionPage`）：
  `slot=latest-values`、`name=latest-values`、`table=latest_value`，
  `cols = terminal_id, item_code, value_num, value_text, unit, value_time, quality, quality_reason`。
- `ReadonlyPages()` / `ReadonlyBySlot()` 导出口径。
- 路由 `GET /admin/v1/readonly/{slot}` → `readonlyList`，复用 `writeList`
  （`q`/`sort`/`order`/`page`/`pageSize`/`filter.*` + `meta{total,page,pageSize,sort,order}`）；
  未知 slot → 404 `unknown_slot`。
- `admin.Entities` 仍 6、`nongoal_test.go` 一行未动；只读源不进 `Entities`。

### 1.2 字段元数据（`fields.go` 新增 `fieldSets["latest-values"]`）

| key | 类型 | 可见 | 可排序 | 筛选 |
| --- | --- | --- | --- | --- |
| `terminal_id` | text | ✓ | ✓ | contains/eq（常驻） |
| `item_code` | text | ✓ | ✓ | contains（常驻） |
| `value_num` | number | ✓ | ✓ | gte/lte（高级） |
| `value_text` | text | ✓ | ✓ | contains（高级） |
| `unit` | text | ✓ | ✓ | contains（高级） |
| `value_time` | datetime | ✓ | ✓ | gte/lte（常驻） |
| `quality` | number | ✓ | ✓ | gte/lte（**常驻**），`options` 8 种组合 |
| `quality_reason` | text | ✓ | ✗ | contains（高级） |

`quality.options`：`0 正常(neutral)`、`1 越界`、`2 规则命中`、`3 越界+规则`、`4 单位闭集外`、
`5 越界+单位闭集外`、`6 规则命中+单位闭集外`、`7 越界+规则+单位闭集外`，除 0 外均为 `warning`。
**不新增 SQL 操作符**：「仅看有问题」= `filter.quality.gte=1`。

只读页字段 `editable`/`required` 恒 `false`——`Editable` 由 `Entity.Cols` 推导而 `latest-values`
不在 `Entities` 内，天然为 false；`TestRequiredMatchesDDL` **未扩**（仍只解析 `0001`）。

### 1.3 `_meta` 扩展（只加不删）

- `data.readonly: [{slot, name, title, route, readOnly, defaultSort, defaultOrder, fields[]}]`，本批 1 项
  （`latest-values` / `最新值` / `/admin/v1/readonly/latest-values` / `readOnly=true` / 默认 `value_time desc`）。
- `data.exceptions` 原样保留（仍 2 项，前端在用）；`MetaException` 改名为 `MetaReadonly` 并保留别名，
  代码注释写明「`exceptions` 是 `readonly` 的特例，后续可合并，本期不动」。
- `docs/design.md` 的管理后台契约行补上 `readonly/{slot}` 与 `snapshot/changes`。

## 2. 测试结果（干净 worktree：`git worktree add .devcache/wt-t9a 13ba869`）

```
$ git rev-parse --short HEAD
13ba869
$ git status --short
(空=干净树)

$ go build ./... && go vet ./...
(无输出)

$ go test ./... -count=1   # 无 DSN
ok  	github.com/RenJieWeia/iot-admin/cmd/server	0.006s
ok  	github.com/RenJieWeia/iot-admin/internal/admin	0.010s
ok  	github.com/RenJieWeia/iot-admin/internal/clean	0.003s
ok  	github.com/RenJieWeia/iot-admin/internal/ingest	0.007s
ok  	github.com/RenJieWeia/iot-admin/internal/meta	0.004s
ok  	github.com/RenJieWeia/iot-admin/internal/staging	0.004s
ok  	github.com/RenJieWeia/iot-admin/internal/ui	0.006s
ok  	github.com/RenJieWeia/iot-admin/internal/validate	0.004s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1
ok  	github.com/RenJieWeia/iot-admin/cmd/server	0.005s
ok  	github.com/RenJieWeia/iot-admin/internal/admin	0.184s
ok  	github.com/RenJieWeia/iot-admin/internal/clean	0.003s
ok  	github.com/RenJieWeia/iot-admin/internal/ingest	2.934s
ok  	github.com/RenJieWeia/iot-admin/internal/meta	0.003s
ok  	github.com/RenJieWeia/iot-admin/internal/staging	0.003s
ok  	github.com/RenJieWeia/iot-admin/internal/ui	0.005s
ok  	github.com/RenJieWeia/iot-admin/internal/validate	0.003s

$ go test -p 1 ./internal/admin -run 'TestReadonly|TestMetaReadonly|TestMetaSections|TestMetaFieldShape' -v -count=1
--- PASS: TestMetaSections
--- PASS: TestMetaFieldShape
--- PASS: TestReadonlyPagesShape
--- PASS: TestReadonlyRouteWhitelist
--- PASS: TestMetaReadonlySection
--- PASS: TestMetaReadonlyWorksWithoutPool
--- PASS: TestReadonlyListWithDB
PASS
ok  	github.com/RenJieWeia/iot-admin/internal/admin	0.022s
```

### 验收对照（task-009 §2 A4）

| 验收项 | 结果 |
| --- | --- |
| `_meta.readonly` 1 项、8 字段、`quality.options` 8 项 | ✅ `TestMetaReadonlySection`；真机输出见 §3 |
| `exceptions` 仍 2 项（防回归） | ✅ 同上断言 |
| `filter.quality.gte=1` 只返回 `quality>=1` | ✅ `TestReadonlyListWithDB`（种子 4 行 → gte1=3 行、gte2=2 行、gte3 行 `quality_reason` 非空） |
| `GET /readonly/latest-values?pageSize=2` → 200 + `meta.total` | ✅ 同上 |
| 未知 slot 404 | ✅ `TestReadonlyRouteWhitelist`（`unknown_slot`） |
| `nil pool` 下 `_meta` 200 | ✅ `TestMetaReadonlyWorksWithoutPool` |
| `admin.Entities` 仍 6 | ✅ `TestReadonlyPagesShape` 断言 + `nongoal_test` 未改仍通过 |

## 3. 真机响应片段（临时实例 `127.0.0.1:18103`，测试库）

```
$ curl /admin/v1/_meta      # readonly 段摘要
readonly 段数: 1  exceptions 段数: 2
slot/name/title/route/readOnly: latest-values latest-values 最新值 /admin/v1/readonly/latest-values True
defaultSort/Order: value_time desc  字段数: 8
  terminal_id    text      editable=False required=False filterable=True advanced=False ops=['contains','eq']
  item_code      text      editable=False required=False filterable=True advanced=False ops=['contains']
  value_num      number    editable=False required=False filterable=True advanced=True  ops=['gte','lte']
  value_text     text      editable=False required=False filterable=True advanced=True  ops=['contains']
  unit           text      editable=False required=False filterable=True advanced=True  ops=['contains']
  value_time     datetime  editable=False required=False filterable=True advanced=False ops=['gte','lte']
  quality        number    editable=False required=False filterable=True advanced=False ops=['gte','lte']
  quality_reason text      editable=False required=False filterable=True advanced=True  ops=['contains']

$ curl /admin/v1/readonly/latest-values?pageSize=2
{"code":0,"message":"ok","data":[{"item_code":"power","quality":0,"quality_reason":null,"terminal_id":"T1",
 "unit":"kW","updated_at":"2026-11-06T09:19:11.149603+08:00","value_num":2,"value_text":null,
 "value_time":"2026-11-05T11:10:00+08:00"}, …]}

$ curl /admin/v1/readonly/nope
404（unknown_slot）
```

> 说明：开发库现有行 `quality` 全为 0，所以 `filter.quality.gte=1` 在真机上返回空集；
> 正例由 `TestReadonlyListWithDB` 用种子数据覆盖（1/2/3/4 四种取值）。

## 4. 风险

1. **只读页只有 offset 分页**：`latest_value` 行数随「终端 × 数据项」增长，深页会慢（与异常页同口径，
   不在本批范围）。
2. **`quality` 用 `gte` 表达"有问题"**：无法表达"仅规则命中（2）"这种精确位查询——书里明确不做位运算操作符。
3. **两套只读键并存**：`_meta.exceptions` 与 `_meta.readonly` 形状相同，前端要各处理一遍；后续可合并。
4. **`value_text`/`quality_reason` 为 NULL 时 JSON 是 `null`**：前端需按可空处理（与既有
   `exceptions` 的 nullable 列一致）。

## 5. 未闭环

- 无新增 `Q-NNN`。`hourly_staging` / `hourly_snapshot` 只读页按书留到下一批。
