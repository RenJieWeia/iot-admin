# report-010a · 运行时两页只读（staging + snapshot）

- `task:` task-010 Part A（`docs/review/task-010.md` §3）
- `baseline:` `rv-14`（`a95249d`）
- `commits:`
  | 内容 | hash |
  |---|---|
  | Part A 代码 + 测试 + design 契约行 | `f8decbb` |
  | 本报告 | 见 `git log -- docs/review/report-010a.md` |
- `范围:` 只碰 `internal/admin/**` 与 `docs/design.md`；未动 `web/**`、`internal/ui/**`、`cmd/server/**`、
  `migrations/`、`nongoal_test.go`、`internal/{ingest,settle,store,latest,staging,validate}`、`go.mod`

## 1. 变更说明

### 1.1 只读源两项 + 分页方式（`internal/admin/readonly.go`）

| slot | 表 | 分页 | 默认排序 | 字段数 |
| --- | --- | --- | --- | --- |
| `latest-values` | `latest_value` | `offset` | `value_time desc` | 8 |
| `hourly-staging` | `hourly_staging` | `offset` | `hour_start desc`（全列 tie-breaker） | 9 |
| `hourly-snapshot` | `hourly_snapshot` | `cursor` | `settled_at desc`（命中 `hourly_snapshot_settled_idx`） | 11 |

- `ReadonlyPage` 新增 `Pagination`（`offset`|`cursor`）与 `DefaultSort`（覆盖字段推导，快照页用
  `settled_at` 而不是被字段顺序推导成 `hour_start`）；`ExceptionPage`/异常两页未动。
- `_meta.readonly[].pagination` 随之下发，前端按它分支选择分页器。

### 1.2 字段元数据（`fields.go`）

抽出 `runtimeCommonFields()`（8 个公共列，每次调用返回独立切片）+ `hour_start`/`revision`/`settled_at`
三个列定义；三页共用同一组公共列，暂存追加 `hour_start`，快照追加 `hour_start`/`settled_at`/`revision`
（顺序：`hour_start, terminal_id, item_code, value_num, value_text, unit, value_time, quality,
quality_reason, revision, settled_at`）。

- `revision`：number，`gte/lte`，常驻、可排序（覆盖 task-008 §3 的「UI 不展示 revision」——该条只适用列表页，
  `changes` 端点未动）。
- `settled_at`：datetime，`gte/lte`，常驻、可排序。
- `hour_start`：datetime，`gte/lte`，常驻、可排序（作为收窄筛选）。
- 只读页字段 `editable=false`、`required=false`（`latest-values` 同款，未扩 `TestRequiredMatchesDDL`）。

### 1.3 分页实现

- **offset 页**（`latest-values` / `hourly-staging`）：沿用 `writeList`，`meta{total,page,pageSize,sort,order}`。
- **cursor 页**（`hourly-snapshot`）：
  - `where` 复用 `buildWhere`（字段/操作符白名单不变），顺序固定
    `settled_at desc, terminal_id asc, item_code asc, hour_start asc`；
  - 下一页条件按行值比较：
    `settled_at < $c OR (settled_at = $c AND (terminal_id,item_code,hour_start) > ($t,$i,$h))`；
  - 多取一行判 `hasMore`；`nextCursor` 复用 `changes.go` 的 base64 编码（`settled_at ␟ terminal ␟ item ␟ hour`）；
  - 响应 `{code,message,data,meta:{hasMore,nextCursor,limit}}`，**没有 `total`**；
  - `limit` 默认 200、上限 1000（超出收敛）；非法 `cursor` → 400 `invalid_query`（消息含「cursor 非法」）；
  - 游标页固定排序，传 `sort`/`order` → 400（排序键必须与游标键一致，否则会重/漏行）。

## 2. 测试（干净 worktree：`git worktree add .devcache/wt-t10a f8decbb`）

```
$ git rev-parse --short HEAD
f8decbb
$ git status --short
(空=干净树)

$ go build ./... && go vet ./...
(无输出)

$ go test ./... -count=1   # 无 DSN
ok  cmd/server 0.011s | internal/admin 0.013s | internal/clean 0.005s | internal/ingest 0.008s
ok  internal/meta 0.006s | internal/staging 0.005s | internal/ui 0.008s | internal/validate 0.006s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1
ok  cmd/server 0.006s | internal/admin 0.282s | internal/clean 0.004s | internal/ingest 2.964s
ok  internal/meta 0.003s | internal/staging 0.004s | internal/ui 0.005s | internal/validate 0.003s

$ go test -p 1 ./internal/admin -run 'TestReadonly|TestMetaReadonly|TestMetaSections|TestMetaFieldShape' -v -count=1
--- PASS: TestMetaSections (0.00s)
--- PASS: TestMetaFieldShape (0.00s)
--- PASS: TestReadonlyPagesShape (0.00s)
--- PASS: TestReadonlyRouteWhitelist (0.00s)
--- PASS: TestMetaReadonlySection (0.00s)
--- PASS: TestMetaReadonlyWorksWithoutPool (0.00s)
--- PASS: TestReadonlyListWithDB (0.02s)
--- PASS: TestReadonlyCursorValidation (0.00s)
--- PASS: TestReadonlyCursorWithDB (0.03s)
--- PASS: TestReadonlyStagingOffsetWithDB (0.01s)
PASS
ok  	github.com/RenJieWeia/iot-admin/internal/admin	0.079s
```

验收对照（task-010 §6）：

| 验收项 | 结果 |
| --- | --- |
| `_meta.readonly` 三项，`pagination` = offset/offset/cursor | ✅ `TestMetaReadonlySection` + 真机（§3） |
| `hourly-snapshot` 含 `revision`/`settled_at` | ✅ 11 字段，两列都在 |
| `staging?pageSize=2` → 200 + `meta.total` | ✅ `TestReadonlyStagingOffsetWithDB` |
| `snapshot?limit=2` → 200 + `hasMore/nextCursor`，无 `total` | ✅ `TestReadonlyCursorWithDB` + 真机 |
| 游标翻页不重叠、`nextCursor` 可回放（幂等） | ✅ 5 行按 `limit=2` 翻完不重不漏；同一 cursor 连续两次结果一致且与首页零重叠 |
| 非法 cursor → 400；`limit>1000` 收敛 | ✅ `TestReadonlyCursorValidation` / `TestReadonlyCursorWithDB`（`limit=5000` → `meta.limit=1000`） |
| `revision.gte=1` 只回被修正行 | ✅ 种子里同格二次 upsert 造出 `revision=1` 的样本，筛选只回该行 |
| 无 DSN 8 包 / 有 DSN `-p 1` 8 包 | ✅ §2 |
| `nongoal_test.go` 未改仍通过；`admin.Entities` 仍 6 | ✅ `TestReadonlyPagesShape` 断言 + 全量测试 |

## 3. 真机响应（临时实例 `127.0.0.1:18105`，测试库）

```
$ curl /admin/v1/_meta
entities: 6  exceptions: 2  readonly: 3
 latest-values    title=最新值    pagination=offset defaultSort=value_time  fields=8
 hourly-staging   title=小时暂存  pagination=offset defaultSort=hour_start  fields=9
 hourly-snapshot  title=小时快照  pagination=cursor defaultSort=settled_at  fields=11

 snapshot 字段: ['hour_start','terminal_id','item_code','value_num','value_text','unit',
                'value_time','quality','quality_reason','revision','settled_at']
 revision : {"key":"revision","label":"修正次数","type":"number","sortable":true,"filterable":true,
             "filterType":"numberRange","operators":["gte","lte"],"advancedOnly":false,
             "editable":false,"required":false}
 settled_at: {"key":"settled_at","label":"落库时间","type":"datetime","sortable":true,"filterable":true,
             "filterType":"dateRange","operators":["gte","lte"],"advancedOnly":false,
             "editable":false,"required":false}

$ curl '/admin/v1/readonly/hourly-staging?pageSize=2'
meta: {'total': 0, 'page': 1, 'pageSize': 2, 'sort': 'hour_start', 'order': 'desc'}（开发库当前无未结算暂存行）

$ curl '/admin/v1/readonly/hourly-snapshot?limit=2'
meta: {'hasMore': False, 'limit': 2}   rows: 2   'total' in meta: False

$ curl '/admin/v1/readonly/hourly-snapshot?cursor=@@@'          → 400
$ curl '/admin/v1/readonly/hourly-snapshot?sort=hour_start'     → 400
  {"code":400,"message":"invalid_query: hourly-snapshot 页按 settled_at 固定排序（游标分页），不支持 sort/order"}
```

> `staging` 的 `total: 0` 是开发库现状（测试收尾会清暂存），不是缺陷；正例由
> `TestReadonlyStagingOffsetWithDB` 用种子数据覆盖（3 行、`pageSize=2` → `total=3`、返回 2 行）。

## 4. 给 `fe-web` 的契约摘要

```
GET /admin/v1/readonly/hourly-staging?q&sort&order&page&pageSize&filter.*
 → {code,message,data:[row],meta:{total,page,pageSize,sort,order}}

GET /admin/v1/readonly/hourly-snapshot?q&limit&cursor&filter.*
 → {code,message,data:[row],meta:{hasMore,nextCursor,limit}}
 · 顺序固定 settled_at desc + (terminal_id,item_code,hour_start) asc
 · 传 sort/order → 400；非法 cursor → 400
 · 行含 revision / settled_at / quality / quality_reason
```

## 5. 风险

1. **快照页没有 `total`**：前端必须按 `hasMore` 渲染「加载更多」，不能显示总数/页码。
2. **游标页不接受 `sort`**：排序固定为 `settled_at desc`（与游标键一致），用户无法改排序。
3. **`settled_at` 相同的事务内多行**：同一事务写入的多行 `settled_at` 相同，靠
   `(terminal_id, item_code, hour_start)` tie-breaker 保证不重不漏（`TestReadonlyCursorWithDB` 覆盖）。
4. **staging 页 offset 深页**：行数受未结算小时约束，量级可控，暂不引入游标。
5. **字段顺序即列顺序**：快照页把 `revision`/`settled_at` 放在末尾，前端若要用 `revision` 做徽标需自行排序展示。

## 6. 未闭环

- 无新增 `Q-NNN`。`hourly-staging` 页在开发库当前为空（无未结算数据），如需 UI 走查可先发一批窗口内报文造数据。
