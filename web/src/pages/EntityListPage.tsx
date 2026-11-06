import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams, useSearchParams } from 'react-router';
import { ApiError, createRow, deleteRow, fetchList, fetchOne, updateRow } from '../api/client';
import { toLabelMap, useFieldOptions } from '../api/entities';
import { useMeta } from '../api/meta';
import type { ListMeta, RowData } from '../api/types';
import { AppShell } from '../components/AppShell';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { DataTable } from '../components/DataTable';
import { EmptyState } from '../components/EmptyState';
import { ErrorState } from '../components/ErrorState';
import { FilterBar } from '../components/FilterBar';
import { Pagination } from '../components/Pagination';
import { RecordDrawer, objectLabel } from '../components/RecordDrawer';
import { Toast, type ToastMessage } from '../components/Toast';
import { defaultSort, relationTarget } from '../lib/fieldMeta';
import {
  clearFilters,
  hasActiveConditions,
  parseQueryState,
  patchQuery,
  setSort,
  toListQuery,
  toSearchParams,
  withDrawer,
  type QueryState,
} from '../lib/queryState';
import { rememberScroll, takeScroll } from '../lib/scroll';

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
  const [pendingDelete, setPendingDelete] = useState<RowData | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);

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

  // 滚动位置：按「列表地址」记忆，返回/刷新后恢复阅读位置（§8 状态保持）。
  const scrollKey = `${window.location.pathname}?${listParams.toString()}`;
  useEffect(() => {
    if (!list.data) return;
    const y = takeScroll(scrollKey);
    if (y !== null && y > 0) window.scrollTo(0, y);
  }, [scrollKey, list.data]);

  useEffect(() => {
    const onScroll = () => rememberScroll(scrollKey, window.scrollY);
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      window.removeEventListener('scroll', onScroll);
      rememberScroll(scrollKey, window.scrollY);
    };
  }, [scrollKey]);

  if (metaQuery.isPending) {
    return (
      <AppShell title="加载中…">
        <p className="muted">正在加载字段元数据…</p>
      </AppShell>
    );
  }

  if (metaQuery.isError) {
    return (
      <AppShell title="管理后台">
        <ErrorState
          title="字段元数据加载失败"
          text={`${errorDetail(metaQuery.error) ?? '未知错误'}。页面无法确定列与筛选能力，请重试或检查 /admin/v1/_meta 是否可用。`}
          onRetry={() => void metaQuery.refetch()}
        />
      </AppShell>
    );
  }

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
  const outOfRange = listMeta.total > 0 && rows.length === 0;
  const listState = list.isError
    ? 'error'
    : list.isPending
      ? 'loading'
      : rows.length === 0
        ? activeFilters
          ? 'nomatch'
          : 'empty'
        : 'ready';

  const saveError = errorDetail(saveMutation.error);
  const deleteError = errorDetail(deleteMutation.error);

  const writable = !entity.readOnly;
  const pageSizeOptions = metaQuery.data?.pagination?.options ?? [10, 20, 50];

  return (
    <AppShell
      title={entity.title}
      actions={
        <button
          type="button"
          className="btn btn-primary"
          data-action="new"
          onClick={(event) => {
            saveMutation.reset();
            openDrawer('new', { trigger: event.currentTarget });
          }}
        >
          新建
        </button>
      }
    >
      <FilterBar
        entity={entity}
        query={query}
        relationOptions={relationOptions}
        onChange={setQuery}
      />

      {listState === 'error' ? (
        <ErrorState
          title="列表加载失败"
          text={`${errorDetail(list.error) ?? '未知错误'}。数据未丢失，可重试；若筛选条件非法，清除筛选后重试。`}
          onRetry={() => void list.refetch()}
          extra={
            activeFilters ? (
              <button type="button" className="btn" onClick={() => setQuery(clearFilters(query))}>
                清除筛选
              </button>
            ) : undefined
          }
        />
      ) : listState === 'empty' ? (
        <EmptyState
          title={`还没有${entity.title}`}
          text="该实体下暂无记录。可用右上角「新建」录入第一条，或通过管理接口写入后再刷新。"
          action={
            <button
              type="button"
              className="btn btn-primary"
              data-action="new"
              onClick={(event) => openDrawer('new', { trigger: event.currentTarget })}
            >
              新建
            </button>
          }
        />
      ) : listState === 'nomatch' ? (
        <EmptyState
          title="没有匹配的记录"
          text="当前关键词或筛选条件排除了全部结果。可逐个移除条件，或清除全部筛选后重新查询。"
          action={
            <button type="button" className="btn" onClick={() => setQuery(clearFilters(query))}>
              清除筛选
            </button>
          }
        />
      ) : (
        <>
          <DataTable
            title={entity.title}
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
            skeletonRows={query.pageSize}
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
    </AppShell>
  );
}
