import { useCallback, useEffect, useMemo, useState } from 'react';
import { Plus, RotateCw } from 'lucide-react';
import type { ListKind } from '../api/client';
import type { MetaPayload, PageMeta, ReadonlyMeta, RowData } from '../api/types';
import { cn } from '../lib/cn';
import { initQuery } from '../lib/query';
import type { QueryState } from '../lib/query';
import { useMeta } from '../meta';
import { useList } from '../hooks/useList';
import { DataTable } from '../components/DataTable';
import { FilterBar } from '../components/FilterBar';
import { Pagination } from '../components/Pagination';
import { CursorPager } from '../components/CursorPager';
import { EmptyState } from '../components/States';
import { RecordDrawer } from '../components/RecordDrawer';
import { GatewayEnabledSwitch } from '../components/GatewayEnabledSwitch';

const KIND_LABEL: Record<ListKind, string> = {
  entity: '实体',
  readonly: '只读视图',
  exception: '异常视图',
};

/** 只读页才带 pagination 声明，实体页按偏移分页兜底。 */
type ResolvedPage = PageMeta & Partial<Pick<ReadonlyMeta, 'slot' | 'pagination'>>;

/** 页面定义唯一来源：entity → entities；readonly → readonly；exception → exceptions。 */
function resolvePage(meta: MetaPayload, kind: ListKind, name: string): ResolvedPage | undefined {
  if (kind === 'readonly') {
    return (meta.readonly ?? []).find((item) => item.slot === name || item.name === name);
  }
  if (kind === 'exception') {
    return meta.exceptions.find((item) => item.slot === name || item.name === name);
  }
  return meta.entities.find((item) => item.name === name);
}

interface DrawerState {
  open: boolean;
  row: RowData | null;
  mode: 'view' | 'create';
}

/** 列表页：同一套表格骨架服务实体 / 只读 / 异常三类页面。 */
export function ListPage({ kind, name }: { kind: ListKind; name: string }) {
  const metaPayload = useMeta();
  const page = resolvePage(metaPayload, kind, name);
  const defaultPageSize = metaPayload.pagination?.default ?? 10;
  const pageOptions = metaPayload.pagination?.options?.length
    ? metaPayload.pagination.options
    : [defaultPageSize];

  const [query, setQuery] = useState<QueryState>(() =>
    initQuery(page?.defaultSort ?? '', page?.defaultOrder ?? 'desc', defaultPageSize),
  );
  const [drawer, setDrawer] = useState<DrawerState>({ open: false, row: null, mode: 'view' });

  // 切页 / 换实体：按页面默认排序重置查询，避免把上一页的排序字段带过去
  useEffect(() => {
    setQuery(
      initQuery(page?.defaultSort ?? '', page?.defaultOrder ?? 'desc', defaultPageSize),
    );
  }, [kind, name, defaultPageSize, page?.defaultSort, page?.defaultOrder]);

  const cursorMode = page?.pagination === 'cursor';
  const list = useList(kind, name, query, cursorMode);

  const visibleFields = useMemo(
    () => (page?.fields ?? []).filter((field) => field.visible),
    [page],
  );

  const handleSort = useCallback((key: string) => {
    setQuery((current) =>
      current.sort === key
        ? { ...current, order: current.order === 'asc' ? 'desc' : 'asc', page: 1 }
        : { ...current, sort: key, order: 'asc', page: 1 },
    );
  }, []);

  const handlePageChange = useCallback((patch: { page?: number; pageSize?: number }) => {
    setQuery((current) => ({ ...current, page: 1, ...patch }));
  }, []);

  const resetQuery = useCallback(() => {
    setQuery((current) => ({ ...current, q: '', filters: [], page: 1 }));
  }, []);

  const openRow = useCallback((row: RowData) => {
    setDrawer({ open: true, row, mode: 'view' });
  }, []);

  if (!page) {
    return (
      <div className="mx-auto max-w-[1400px] px-6 py-5">
        <EmptyState
          title="未找到页面"
          hint={`_meta 里没有名为 ${name} 的${KIND_LABEL[kind]}定义。`}
        />
      </div>
    );
  }

  const canCreate = kind === 'entity' && !page.readOnly;
  // 网关的启用状态是行内开关（操作栏），因为切换会同步 Broker 注册/注销。
  const rowActions =
    kind === 'entity' && page.name === 'gateways'
      ? (row: RowData) => <GatewayEnabledSwitch row={row} onSaved={list.reload} />
      : undefined;
  const sortLabel = query.sort
    ? (page.fields.find((field) => field.key === query.sort)?.label ?? query.sort)
    : '默认';

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted">管理后台 / {KIND_LABEL[kind]}</p>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">{page.title}</h1>
          {page.description ? (
            <p className="mt-1 max-w-3xl text-xs text-muted">{page.description}</p>
          ) : null}
          <p className="mt-1 text-xs text-muted">
            {visibleFields.length} 列 ·{' '}
            {cursorMode
              ? '游标分页（顺序由服务端固定）'
              : `排序 ${sortLabel} · ${query.order === 'asc' ? '升序' : '降序'}`}{' '}
            · 分页与筛选均由服务端执行
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={list.reload}
            disabled={list.loading}
            className="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RotateCw className={cn('size-4', list.loading && 'animate-spin')} aria-hidden />
            刷新
          </button>
          {canCreate ? (
            <button
              type="button"
              onClick={() => setDrawer({ open: true, row: null, mode: 'create' })}
              className="inline-flex h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover"
            >
              <Plus className="size-4" aria-hidden />
              新建
            </button>
          ) : null}
        </div>
      </div>

      <div className="mt-4 overflow-hidden rounded-xl border border-line bg-surface shadow-panel">
        <FilterBar fields={page.fields} query={query} onChange={setQuery} />
        <DataTable
          fields={visibleFields}
          rows={list.rows}
          loading={list.loading}
          error={list.error}
          sort={query.sort}
          order={query.order}
          disableSort={cursorMode}
          onSortChange={handleSort}
          onRowClick={openRow}
          renderActions={rowActions}
          onRetry={list.reload}
          onReset={resetQuery}
        />
        {cursorMode ? (
          <CursorPager
            hasMore={list.cursor?.hasMore ?? false}
            loading={list.loading}
            loaded={list.rows.length}
            onLoadMore={list.loadMore}
          />
        ) : list.meta ? (
          <Pagination
            page={list.meta.page}
            pageSize={list.meta.pageSize}
            total={list.meta.total}
            options={pageOptions}
            disabled={list.loading}
            onChange={handlePageChange}
          />
        ) : null}
      </div>

      <RecordDrawer
        entity={{
          name: page.name,
          title: page.title,
          fields: page.fields,
          readOnly: page.readOnly,
        }}
        row={drawer.row}
        mode={drawer.mode}
        open={drawer.open}
        onOpenChange={(open) => setDrawer((current) => ({ ...current, open }))}
        onSaved={list.reload}
      />
    </div>
  );
}

export default ListPage;
