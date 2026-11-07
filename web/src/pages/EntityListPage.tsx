import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Button, Card as RadixCard } from '@radix-ui/themes';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router';
import { RESET_PAGE_NOTICE, computeListState, shouldResetPage } from '../lib/listState';
import { groupCrumb } from '../lib/navGroups';
import { ApiError, createRow, deleteRow, fetchList, fetchOne, updateRow } from '../api/client';
import { toLabelMap, useFieldOptions } from '../api/entities';
import { useMeta } from '../api/meta';
import type { ListMeta, RowData } from '../api/types';
import { AppShell } from '../components/AppShell';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { DataTable } from '../components/DataTable';
import { EmptyState } from '../components/EmptyState';
import { ErrorState } from '../components/ErrorState';
import { Card, CardHeader } from '../components/Card';
import { FilterBar } from '../components/FilterBar';
import { Pagination } from '../components/Pagination';
import { RecordDrawer, objectLabel } from '../components/RecordDrawer';
import { Toast, type ToastMessage } from '../components/Toast';
import { defaultSort, isWritable, relationTarget } from '../lib/fieldMeta';
import {
  clearFilters,
  hasActiveConditions,
  parseQueryState,
  patchQuery,
  resetQuery,
  setSort,
  toListQuery,
  toSearchParams,
  withDrawer,
  type QueryState,
} from '../lib/queryState';
import { rememberScroll, takeScroll } from '../lib/scroll';
import { getScrollPane } from '../lib/scrollPane';

/**
 * P2 试点适配层（1107018）：页面级按钮与卡片的取值范围开关。
 *
 * `EntityListPage` 是所有实体列表共用的路由（/:entity），所以试点控件必须按实体开，
 * 不能无条件换 —— 否则 data-domains / quality-rules / device-types / snapshot-slots 的列表
 * 会跟着换成 Radix 控件（验收就是这么发现的）。开关值见组件内的 radixPilot。
 */
const PilotUiContext = createContext(false);

function ListCard({ children }: { children: ReactNode }) {
  const pilot = useContext(PilotUiContext);
  if (pilot) return <RadixCard size="3" data-card>{children}</RadixCard>;
  return <Card>{children}</Card>;
}

function rangeText(meta: ListMeta, rowCount: number): string {
  if (meta.total === 0) return '共 0 条';
  if (rowCount === 0) return `第 ${meta.page} 页无数据，共 ${meta.total} 条`;
  const start = (meta.page - 1) * meta.pageSize + 1;
  return `第 ${start}–${start + rowCount - 1} 条，共 ${meta.total} 条`;
}

function errorDetail(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) return error.detail;
  return String(error);
}

/**
 * 实体列表页（6 个实体共用）：字段元数据驱动，服务端筛选/排序/分页，
 * QueryState 全量进 URL，刷新/后退/抽屉关闭后状态还原。
 */
export function EntityListPage() {
  const params = useParams();
  const navigate = useNavigate();
  const entityName = params.entity ?? '';
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const metaQuery = useMeta();
  const entity = metaQuery.data?.entities.find((item) => item.name === entityName);

  const query = useMemo(() => parseQueryState(searchParams), [searchParams]);
  // 抽屉参数不参与列表查询键：打开/关闭抽屉不应触发重新请求。
  const listParams = useMemo(() => {
    const next = new URLSearchParams(searchParams);
    next.delete('drawer');
    return next;
  }, [searchParams]);

  const list = useQuery({
    queryKey: ['list', entityName, listParams.toString()],
    queryFn: ({ signal }) => fetchList(entityName, toListQuery(parseQueryState(listParams)), signal),
    enabled: Boolean(entity),
    placeholderData: (prev) => prev,
  });

  const relationTargets = useMemo(() => {
    if (!entity) return [];
    return Array.from(
      new Set(
        entity.fields
          .map((field) => relationTarget(field))
          .filter((target): target is string => Boolean(target)),
      ),
    );
  }, [entity]);
  const optionsQuery = useFieldOptions(relationTargets);

  /** 字段名 → (value → label)，供关系列渲染与筛选标签。 */
  const relationOptions = useMemo(() => {
    const map: Record<string, Record<string, string>> = {};
    for (const field of entity?.fields ?? []) {
      const target = relationTarget(field);
      if (target) map[field.key] = toLabelMap(optionsQuery.data?.[target]);
    }
    return map;
  }, [entity, optionsQuery.data]);

  const drawerParam = searchParams.get('drawer');
  const drawerMode: 'new' | 'edit' = drawerParam === 'new' ? 'new' : 'edit';
  const rows = list.data?.rows ?? [];
  const listMeta: ListMeta = list.data?.meta ?? {
    total: 0,
    page: query.page,
    pageSize: query.pageSize,
    sort: query.sort,
    order: query.order,
  };
  // 默认排序由后端下发（_meta.defaultSort），本地的 defaultSort 只做兜底。
  const fallbackSort = entity ? entity.defaultSort || defaultSort(entity) : '';

  const rowFromPage =
    drawerParam && drawerParam !== 'new'
      ? rows.find((row) => String(row.id) === drawerParam)
      : undefined;
  const editRowQuery = useQuery({
    queryKey: ['row', entityName, drawerParam],
    queryFn: ({ signal }) => fetchOne(entityName, drawerParam ?? '', signal),
    enabled: Boolean(entity) && Boolean(drawerParam) && drawerParam !== 'new' && !rowFromPage,
  });
  const activeRow: RowData | null =
    drawerMode === 'new' ? null : (rowFromPage ?? editRowQuery.data ?? null);

  const [toast, setToast] = useState<ToastMessage | null>(null);
  /** 页码越界纠偏是"系统替你调了页"，不是成功事件：用嵌入式中性提示，不弹成功样式 toast（§16.4）。 */
  const [pageNotice, setPageNotice] = useState<string | null>(null);
  // 旧深链重定向带过来的中性提示（/ui/thing-model-properties → /ui/device-types）：
  // 显示一次后清掉 history state，刷新不会重复出现。
  const location = useLocation();
  useEffect(() => {
    const notice = (location.state as { notice?: string } | null)?.notice;
    if (!notice) return;
    setPageNotice(notice);
    navigate(`${location.pathname}${location.search}`, { replace: true, state: null });
  }, [location, navigate]);
  const [pendingDelete, setPendingDelete] = useState<RowData | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  /** 越界自动修正只做一次：回到第一页后置位，避免反复跳转。 */
  const correctedRef = useRef(false);

  const setQuery = useCallback(
    (next: QueryState, replace = false) => {
      setSearchParams(toSearchParams(next), { replace });
    },
    [setSearchParams],
  );

  /** 关抽屉后要归还焦点的行（null 表示新建按钮）。 */
  const focusRowRef = useRef<string | null>(null);

  const openDrawer = useCallback(
    (value: string, options: { trigger?: HTMLElement | null; rowId?: string | null } = {}) => {
      // 触发元素决定焦点归还目标：优先用事件目标，其次用当前焦点元素。
      const active = options.trigger ?? (document.activeElement as HTMLElement | null);
      triggerRef.current = active instanceof HTMLElement ? active : null;
      focusRowRef.current = options.rowId ?? null;
      setSearchParams(withDrawer(searchParams, value), { replace: false });
    },
    [searchParams, setSearchParams],
  );

  /**
   * 焦点归还：必须等抽屉真正从 DOM 卸下、且列表重渲染完成之后再聚焦。
   * 用 rAF 会落在 React 的 transition 提交之前（该次导航是 transition），焦点会被
   * Radix FocusScope 的卸载清理覆盖回 body；改为「drawer 参数由非空变为空的提交后」执行。
   * 列表重渲染会换掉旧节点，所以按 data-action/data-row-id 重新取当前节点。
   */
  const prevDrawerRef = useRef<string | null>(drawerParam);
  useEffect(() => {
    const previous = prevDrawerRef.current;
    prevDrawerRef.current = drawerParam;
    if (previous === null || drawerParam !== null) return;
    const rowId = focusRowRef.current;
    const target =
      rowId === null
        ? document.querySelector<HTMLElement>('[data-action="new"]')
        : Array.from(document.querySelectorAll<HTMLElement>('[data-action="edit"]')).find(
            (node) => node.dataset.rowId === rowId,
          );
    (target ?? triggerRef.current)?.focus();
  }, [drawerParam]);

  const closeDrawer = useCallback(() => {
    // 焦点归还在 drawer 参数落为 null 的提交后统一处理（见上方 effect）。
    setSearchParams(withDrawer(searchParams, null), { replace: false });
  }, [searchParams, setSearchParams]);

  const saveMutation = useMutation({
    mutationFn: async (payload: { mode: 'new' | 'edit'; body: RowData; id: string | null }) => {
      if (payload.mode === 'new') return createRow(entityName, payload.body);
      return updateRow(entityName, payload.id ?? '', payload.body);
    },
    onSuccess: (_data, payload) => {
      void queryClient.invalidateQueries({ queryKey: ['list', entityName] });
      void queryClient.invalidateQueries({ queryKey: ['options'] });
      setSearchParams(withDrawer(searchParams, null), { replace: true });
      setToast({
        kind: 'ok',
        text: payload.mode === 'new' ? '已创建记录，列表已刷新。' : '已保存修改，列表已刷新。',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteRow(entityName, id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['list', entityName] });
      void queryClient.invalidateQueries({ queryKey: ['options'] });
      setPendingDelete(null);
      if (searchParams.get('drawer')) {
        setSearchParams(withDrawer(searchParams, null), { replace: true });
      }
      setToast({ kind: 'ok', text: '已删除记录，列表已刷新。' });
    },
  });

  useEffect(() => {
    document.title = entity ? `${entity.title} · IoT 管理后台` : 'IoT 管理后台';
  }, [entity]);

  // F2：页码越界（当前页无行、但总数 > 0）时用 replace 回到第一页，筛选条件全部保留。
  // 用 ref 去重：回到第一页后不再触发；第一页仍为空则按「无匹配」态展示。
  useEffect(() => {
    if (!entity) return;
    if (query.page === 1) {
      correctedRef.current = false;
      return;
    }
    const need = shouldResetPage({
      page: query.page,
      rowCount: rows.length,
      total: listMeta.total,
      isPending: list.isPending,
      isError: list.isError,
      alreadyCorrected: correctedRef.current,
    });
    if (!need) return;
    correctedRef.current = true;
    // replace：历史里不留下"无数据的页"这一跳；page=1 不落 URL。
    setSearchParams(toSearchParams(patchQuery(query, { page: 1 })), { replace: true });
    setPageNotice(RESET_PAGE_NOTICE);
  }, [entity, list, query, rows.length, listMeta.total, setSearchParams]);

  // 滚动位置：按「列表地址」记忆，返回/刷新后恢复阅读位置（§8 状态保持）。
  const scrollKey = `${window.location.pathname}?${listParams.toString()}`;
  useEffect(() => {
    if (!list.data) return;
    const pane = getScrollPane();
    const y = takeScroll(scrollKey);
    if (y !== null && y > 0) pane.scrollTop = y;
  }, [scrollKey, list.data]);

  useEffect(() => {
    const pane = getScrollPane();
    const onScroll = () => rememberScroll(scrollKey, pane.scrollTop);
    pane.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      pane.removeEventListener('scroll', onScroll);
      rememberScroll(scrollKey, pane.scrollTop);
    };
  }, [scrollKey]);

  // 元数据未就绪/失败由 MetaGate 在外层处理，这里只会拿到已就绪的 meta。
  if (!entity) {
    return (
      <AppShell title="页面不存在">
        <ErrorState
          title="未知实体"
          text={`当前地址的实体「${entityName}」不在字段元数据里。请从左侧导航重新进入。`}
          onRetry={() => window.history.back()}
          retryLabel="返回上一页"
        />
      </AppShell>
    );
  }

  const fields = entity.fields.filter((field) => field.visible);
  const activeFilters = hasActiveConditions(query);
  /** 地址里除默认视图外还有别的条件：错误态才需要「重置查询」这个出口。 */
  const dirtyQuery = activeFilters || query.sort !== '' || query.page > 1;
  const outOfRange = listMeta.total > 0 && rows.length === 0;
  /** 已有数据时的刷新失败：TanStack 保留了 rows，不能把整表换成整页错误态（§13.4）。 */
  const staleRefreshError = list.isError && Boolean(list.data);
  // 四态判定与详情页同源（lib/listState.ts）：只有确实设了条件才算「无匹配」；
  // total > 0 但没有条件，说明是这一页暂时取不到数据。
  const listState = computeListState({
    isError: list.isError,
    hasData: Boolean(list.data),
    isPending: list.isPending,
    rowCount: rows.length,
    total: listMeta.total,
    hasConditions: activeFilters,
  });

  const saveError = errorDetail(saveMutation.error);
  const deleteError = errorDetail(deleteMutation.error);

  // 可写性完全由 _meta 派生（readOnly + 是否存在 editable 字段），不看路由。
  const writable = isWritable(entity);

  /**
   * P2 试点范围（1107018）：**只有终端列表** /terminals 用 Radix 控件。
   * 本组件是所有实体列表共用的路由（/:entity → data-domains / device-types / terminals /
   * quality-rules / snapshot-slots），按实体 key 判定而不是「这个组件就是试点页」——
   * 无条件传 pilot 会把其它 4 个实体列表一起换掉（验收就是这么发现的）。
   * 用 _meta 下发的 entity.name（= 路由段 = 接口路径段），不依赖 location 字符串，最稳。
   */
  const radixPilot = entity.name === 'terminals';
  const pageSizeOptions = metaQuery.data?.pagination?.options ?? [10, 20, 50];

  return (
    <AppShell
      title={entity.title}
      // 一级页：只显示分组名，页面名由 h1 承担，避免与标题重复。
      breadcrumb={groupCrumb('entities')}
    >
      {/* 试点开关作用域：只影响本页自有的按钮/卡片取值，见 radixPilot 注释。 */}
      <PilotUiContext.Provider value={radixPilot}>
      {/* 摘要行：只消费现成数据（meta.total + 是否有筛选），不新增接口。
          高度固定（h-6 = 24px）：0 条 / 有数据 / 有筛选三种状态都占位，避免布局跳动。 */}
      <p
        className="m-0 mb-3 flex h-6 items-center gap-2 text-sm text-muted"
        data-testid="list-summary"
      >
        <span>共 {listMeta.total} 条</span>
        {activeFilters && (
          <>
            <span aria-hidden="true">·</span>
            <span>已筛选</span>
          </>
        )}
      </p>

      {/* P2 试点：卡片换 Radix <Card size="3">；data-card 必须保留（card-hierarchy 用例依赖）。 */}
      <ListCard>
        <CardHeader
          title="筛选"
          actions={
            <Button
              type="button" variant="surface" size="3"
              data-action="clear-filters"
              disabled={!activeFilters}
              onClick={() => setQuery(patchQuery(clearFilters(query), { page: 1 }))}
            >
              清除筛选
            </Button>
          }
        />
        <FilterBar
          entity={entity}
          query={query}
          relationOptions={relationOptions}
          onChange={setQuery}
          optionsError={optionsQuery.isError}
          onRetryOptions={() => void optionsQuery.refetch()}
          pilot={radixPilot}
        />
      </ListCard>

      <ListCard>
        <CardHeader
          title="数据"
          actions={
            writable ? (
              <Button
                type="button" variant="solid" size="3"
                data-action="new"
                onClick={(event) => {
                  saveMutation.reset();
                  openDrawer('new', { trigger: event.currentTarget });
                }}
              >
                新建
              </Button>
            ) : undefined
          }
        />
      {pageNotice && (
        <div className="mb-4 flex flex-wrap items-center gap-3 rounded-sm border border-border bg-neutral-bg px-3 py-2">
          <span className="text-sm" role="status">
            {pageNotice}
          </span>
          <Button
            type="button" variant="ghost" size="3"
            className="ml-auto h-10"
            aria-label="关闭纠偏提示"
            onClick={() => setPageNotice(null)}
          >
            知道了
          </Button>
        </div>
      )}

      {staleRefreshError && (
        <div
          role="alert"
          className="mb-4 flex flex-wrap items-center gap-3 rounded-sm border border-warning bg-warning-weak px-3 py-2 text-warning"
        >
          <span>刷新失败，显示的是上次数据</span>
          <span className="text-muted">{errorDetail(list.error) ?? '未知错误'}</span>
          <Button type="button" variant="surface" size="3" onClick={() => void list.refetch()}>
            重试
          </Button>
        </div>
      )}

      {listState === 'error' ? (
        <ErrorState
          title="列表加载失败"
          text={`${errorDetail(list.error) ?? '未知错误'}。数据未丢失，可重试；若地址里的筛选或排序参数非法，清空条件后重试。`}
          onRetry={() => void list.refetch()}
          extra={
            <>
              {activeFilters ? (
                <Button type="button" variant="surface" size="3" onClick={() => setQuery(clearFilters(query))}>
                  清除筛选
                </Button>
              ) : null}
              {dirtyQuery ? (
                <Button type="button" variant="surface" size="3" onClick={() => setQuery(resetQuery(query))}>
                  重置查询
                </Button>
              ) : null}
            </>
          }
        />
      ) : listState === 'empty' ? (
        <EmptyState
          title={`还没有${entity.title}`}
          text="该实体下暂无记录。可用右上角「新建」录入第一条，或通过管理接口写入后再刷新。"
          action={
            writable ? (
              <Button
                type="button" variant="surface" size="3"
                data-action="new"
                onClick={(event) => openDrawer('new', { trigger: event.currentTarget })}
              >
                新建{entity.title}
              </Button>
            ) : undefined
          }
        />
      ) : listState === 'nomatch' ? (
        <EmptyState
          title="没有匹配的记录"
          text="当前关键词或筛选条件排除了全部结果。可逐个移除条件，或清除全部筛选后重新查询。"
          action={
            <Button type="button" variant="surface" size="3" onClick={() => setQuery(clearFilters(query))}>
              清除筛选
            </Button>
          }
        />
      ) : listState === 'stale' ? (
        // 没有设任何条件却取不到行，但总数仍 > 0：多半是数据刚被改/删，不能说「筛选排除了全部结果」。
        <EmptyState
          title="当前页暂时没有数据"
          text={`总数显示仍有 ${listMeta.total} 条，但这一页取不到数据：可能刚被删除，或数据在变化，请刷新或重试。`}
          action={
            <Button type="button" variant="surface" size="3" onClick={() => void list.refetch()}>
              刷新
            </Button>
          }
        />
      ) : (
        <>
          <DataTable
            fields={fields}
            rows={rows}
            state={listState === 'loading' ? 'loading' : 'ready'}
            sort={query.sort || fallbackSort}
            order={query.order}
            onSort={(field) => setQuery(setSort(query, field))}
            relationOptions={relationOptions}
            writable={writable}
            onEdit={(row, trigger) => {
              saveMutation.reset();
              openDrawer(String(row.id), { trigger, rowId: String(row.id) });
            }}
            onDelete={(row) => {
              deleteMutation.reset();
              setPendingDelete(row);
            }}
            // 终端类型：进入「终端类型详情页」（属性子列表）。其它实体不出这个入口。
            onOpen={
              entity.name === 'device-types'
                ? (row) =>
                    navigate(`/device-types/${encodeURIComponent(String(row.id ?? ''))}`, {
                      // 把来源 URL（含筛选/排序/分页）交给详情页，
                      // 面包屑父级用它当 href，用户返回时上下文不丢。
                      // 用路由自带的 location，而不是 window.location：
                      // 前者在测试（MemoryRouter）与真实浏览器里都指向当前页。
                      state: { from: `${location.pathname}${location.search}` },
                    })
                : undefined
            }
            openLabel="查看属性"
            skeletonRows={query.pageSize}
            // §16.1：总数只在分页区报一次（「第 1–10 条，共 N 条」），这里用不含数字的表意文案。
            caption="当前筛选结果"
          />
          <Pagination
            meta={listMeta}
            rangeText={rangeText(listMeta, rows.length)}
            outOfRange={outOfRange}
            pageSizeOptions={pageSizeOptions}
            onPage={(page) => setQuery(patchQuery(query, { page }))}
            onPageSize={(pageSize) => setQuery(patchQuery(query, { pageSize }))}
          />
        </>
      )}
      </ListCard>

      {drawerParam && (
        <RecordDrawer
          entity={entity}
          writable={writable}
          mode={drawerMode}
          row={drawerMode === 'edit' ? activeRow : null}
          saving={saveMutation.isPending}
          deleting={deleteMutation.isPending}
          error={saveError ?? deleteError}
          onClose={closeDrawer}
          onSave={(body) =>
            saveMutation.mutate({
              mode: drawerMode,
              body,
              id: drawerParam === 'new' ? null : drawerParam,
            })
          }
          onDelete={() => setPendingDelete(activeRow)}
        />
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        objectName={pendingDelete ? objectLabel(entity, pendingDelete) : entity.title}
        busy={deleteMutation.isPending}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          if (pendingDelete) deleteMutation.mutate(String(pendingDelete.id));
        }}
      />

      <Toast message={toast} onDismiss={() => setToast(null)} />
      </PilotUiContext.Provider>
    </AppShell>
  );
}
