# web 重建契约（唯一事实源）

后端 API 已在本机 127.0.0.1:8080 跑着（`bash .dev/run.sh`），dev 期由 vite 把 `/admin` 代理过去。
生产构建产物写进 `internal/ui/dist`，由 Go `embed` 从 `/ui/` 提供。

## 已有文件（Lead 维护，勿改）

- `package.json` / `vite.config.ts` / `tsconfig.json` / `index.html`
- `src/styles.css`：light/dark 语义色令牌（`bg-canvas`、`bg-surface`、`text-fg`、`text-muted`、`border-line`、`bg-primary`、`text-positive`、`bg-warning-soft` …）与 `.tabular`
- `src/api/types.ts`：`_meta` 契约（`FieldMeta`/`PageMeta`/`MetaPayload`/`RowData`/`ListMeta`/`CursorMeta`）
- `src/api/client.ts`：`fetchMeta`、`fetchList(kind,name,params,signal)`、`fetchOne`、`createRow`、`updateRow`、`deleteRow`、`fetchOptions`、`ApiError`
- `src/lib/format.ts`：`isBlank`、`formatDateTime`、`formatNumber`、`briefJson`、`optionLabel`、`emptyValue`、`coerceForSubmit`、`rowTitle`
- `src/lib/query.ts`：`QueryState`、`FilterCond`、`initQuery`、`toParams`、`toCursorParams`、`setFilter`、`removeFilter`
- `src/lib/cn.ts`：`cn`
- `src/meta.tsx`：`MetaProvider`、`useMetaState`、`useMeta`、`useRelationLabels`

## 文件归属

### A / shell-dev

- `src/main.tsx`：`createRoot` + `MetaProvider` + `ToastProvider` + `BrowserRouter basename={import.meta.env.BASE_URL.replace(/\/$/, '')}`
- `src/App.tsx`：路由
  - `/` → 第一个导航项（`<Navigate>` 或直接渲染 ListPage）
  - `/e/:name` → `<ListPage kind="entity" name={name} />`
  - `/r/:slot` → `<ListPage kind="readonly" name={slot} />`
  - `/x/:slot` → `<ListPage kind="exception" name={slot} />`
  - 加载中/失败用 `MetaGate` 覆盖整页（失败给重试按钮）
- `src/components/AppShell.tsx`：侧边栏 + 顶栏 + `<Outlet />`
  - 分组：业务（entities 里非 `snapshot-*`）、快照（`snapshot-*` 实体与 readonly 页）、观测（`latest-values` + exceptions）
  - 当前路由高亮；窄屏折叠成抽屉式侧栏
  - 主题切换（light/dark，localStorage `iot-theme`，默认跟随系统）
- `src/components/MetaGate.tsx`：全屏加载/错误态

### B / list-dev

- `src/hooks/useList.ts`：按 `QueryState` 拉列表，返回 `{rows, meta, cursor, loading, error, reload}`；`AbortController` 取消；cursor 页额外给 `loadMore()`
- `src/components/FieldValue.tsx`：按 `FieldMeta.type` 渲染（enum→状态标签带 tone、boolean→是/否、datetime→`formatDateTime`、number→`formatNumber`、relation→`useRelationLabels`、json→`briefJson`、id→等宽小字）
- `src/components/DataTable.tsx`：列宽取 `field.width`（px）或 flex；表头可排序（`sortable` 才能点）；行点击回调；loading 骨架；空/错状态
- `src/components/FilterBar.tsx`：搜索框（防抖 300ms）+ 按 `filterType` 生成控件（text / multiSelect / boolean / relation / numberRange / dateRange）+ 已选筛选 chips（可删除）
- `src/components/Pagination.tsx`：页码 + 每页条数（`meta.pagination.options`）+ 总数；只读 cursor 页不显示页码
- `src/components/CursorPager.tsx`：`加载更多` + `hasMore`
- `src/pages/ListPage.tsx`：
  - props `{ kind: 'entity' | 'readonly' | 'exception'; name: string }`
  - 标题/字段来自 `_meta`（entity → `entities`；readonly → `readonly`；exception → `exceptions`）
  - 工具栏：`新建`（仅 entity 且非只读，且非 fixed-row 实体由后端 400 兜底）、刷新、结果计数
  - 行点击 → `<RecordDrawer mode="view" row={row} …>`；`新建` → `mode="create"`；`onSaved` 触发 reload

### C / record-dev + 交付链路

- `src/components/Drawer.tsx`：Radix `Dialog` 右侧抽屉（宽 520px，遮罩，Esc 关闭，进入/退出过渡）
- `src/components/ConfirmDialog.tsx`：Radix `AlertDialog` 删除确认
- `src/components/Toast.tsx`：Radix `Toast`，导出 `ToastProvider` 与 `useToast(): {success, error, info}`
- `src/components/RelationSelect.tsx`：Radix `Popover` 组合按钮 + 搜索输入 + 选项列表（`fetchOptions` 防抖，选中回填 label）
- `src/components/RecordDrawer.tsx`：props
  ```ts
  { entity: { name: string; title: string; fields: FieldMeta[]; readOnly?: boolean };
    row: RowData | null;
    mode: 'view' | 'edit' | 'create';
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onSaved?: () => void }
  ```
  - view：全部 `visible` 字段的详情网格；`entity.readOnly` 时只读；可写实体给「编辑」按钮切到 edit
  - edit：只渲染 `field.editable` 字段；`required` 前端校验；`json` 用 textarea，`number` 用数字输入，`boolean` 用 checkbox/switch，`datetime` 文本输入
  - create：空白表单，提交 `createRow`；edit 提交 `updateRow(entity, row.id, body)`
  - 删除：`ConfirmDialog` + `deleteRow`，成功 `onSaved()`
  - 错误：`ApiError.detail` 原文显示（后端会把 400 原因写清楚）
- Go 交付链路：
  - `internal/ui/embed.go` + `internal/ui/spa.go`（`//go:embed all:dist`，`/ui` 下 SPA fallback + 哈希资源长缓存）
  - `cmd/server/main.go` 挂 `ui.Handler()` 到 `/ui/`，`GET /` 302 → `/ui/`；CSP 允许 `style-src 'self' 'unsafe-inline'`（Radix 内联 style）
  - `cmd/server/main_test.go` 覆盖 `/` 跳转、`/ui/` 200、深链 fallback
  - `Makefile` 恢复 `web-check` / `web-build` / `dist-check`，`check` 依赖二者
  - `README.md` 补前端开发/构建说明

## 硬约束

- 只用已声明的依赖；不新增依赖、不新增 npm 包。
- 文案中文；字段名、枚举、列宽、分页档位一律从 `_meta` 读，不硬编码业务词。
- 每个文件一个组件，函数组件 + hooks，不做 class 组件。
- 不跑 git 写操作；不改 `.dev/`；不动别人的文件。
- 完成后 `cd web && npx tsc --noEmit` 必须通过（自己负责的文件）。
