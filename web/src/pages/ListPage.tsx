import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Grid2x2, Grid3x3, LayoutGrid, Plus, RotateCw, Table2 } from 'lucide-react';
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
import { CardGrid } from '../components/CardGrid';
import { readDensity, readViewMode, writeDensity, writeViewMode } from '../lib/viewPrefs';
import type { CardDensity, ViewMode } from '../lib/viewPrefs';

/** 卡片视图只对终端与网关开放；其它实体保持表格。 */
const CARD_ENTITIES = new Set(['terminals', 'gateways']);

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
  const navigate = useNavigate();
  const cardEnabled = kind === 'entity' && CARD_ENTITIES.has(name);
  // 视图与密度都按实体记在浏览器里；切实体时重新读取自己的偏好。
  const [viewMode, setViewMode] = useState<ViewMode>(() => (cardEnabled ? readViewMode(name) : 'table'));
  const [density, setDensity] = useState<CardDensity>(() => (cardEnabled ? readDensity(name) : 'comfortable'));

  useEffect(() => {
    if (!cardEnabled) {
      setViewMode('table');
      return;
    }
    setViewMode(readViewMode(name));
    setDensity(readDensity(name));
  }, [name, cardEnabled]);

  const changeView = (mode: ViewMode) => {
    setViewMode(mode);
    writeViewMode(name, mode);
  };
  const changeDensity = (next: CardDensity) => {
    setDensity(next);
    writeDensity(name, next);
  };

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

  const openRow = useCallback(
    (row: RowData) => {
      // 终端类型的"绑定关系"在详情页（字段 + 终端实例），行点击直接进详情。
      if (kind === 'entity' && name === 'device-types' && row.id !== undefined) {
        navigate(`/e/device-types/${String(row.id)}`);
        return;
      }
      if (kind === 'entity' && name === 'terminals' && row.id !== undefined) {
        navigate(`/e/terminals/${String(row.id)}`);
        return;
      }
      setDrawer({ open: true, row, mode: 'view' });
    },
    [kind, name, navigate],
  );

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
            <p className="mt-1 truncate text-xs text-muted" title={page.description}>
              {page.description}
            </p>
          ) : null}
          {/* 有实体说明时只显示说明，不再叠一行技术性摘要。 */}
          {!page.description ? (
            <p className="mt-1 truncate text-xs text-muted">
              {visibleFields.length} 列 ·{' '}
              {cursorMode
                ? '游标分页（顺序由服务端固定）'
                : `排序 ${sortLabel} · ${query.order === 'asc' ? '升序' : '降序'}`}{' '}
              · 分页与筛选均由服务端执行
            </p>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          {cardEnabled ? (
            <div className="inline-flex h-9 items-center rounded-md border border-line p-0.5" role="group" aria-label="视图切换">
              <button
                type="button"
                aria-pressed={viewMode === 'table'}
                title="表格视图"
                onClick={() => changeView('table')}
                className={cn(
                  'inline-flex h-8 items-center gap-1 rounded px-2 text-xs',
                  viewMode === 'table' ? 'bg-primary-soft text-primary' : 'text-muted hover:text-fg',
                )}
              >
                <Table2 className="size-4" aria-hidden />
                表格
              </button>
              <button
                type="button"
                aria-pressed={viewMode === 'card'}
                title="卡片视图"
                onClick={() => changeView('card')}
                className={cn(
                  'inline-flex h-8 items-center gap-1 rounded px-2 text-xs',
                  viewMode === 'card' ? 'bg-primary-soft text-primary' : 'text-muted hover:text-fg',
                )}
              >
                <LayoutGrid className="size-4" aria-hidden />
                卡片
              </button>
            </div>
          ) : null}
          {cardEnabled && viewMode === 'card' ? (
            <div className="inline-flex h-9 items-center rounded-md border border-line p-0.5" role="group" aria-label="卡片密度">
              <button
                type="button"
                aria-pressed={density === 'comfortable'}
                title="适中"
                onClick={() => changeDensity('comfortable')}
                className={cn(
                  'inline-flex h-8 items-center rounded px-2 text-xs',
                  density === 'comfortable' ? 'bg-primary-soft text-primary' : 'text-muted hover:text-fg',
                )}
              >
                <Grid2x2 className="size-4" aria-hidden />
                适中
              </button>
              <button
                type="button"
                aria-pressed={density === 'compact'}
                title="紧凑"
                onClick={() => changeDensity('compact')}
                className={cn(
                  'inline-flex h-8 items-center rounded px-2 text-xs',
                  density === 'compact' ? 'bg-primary-soft text-primary' : 'text-muted hover:text-fg',
                )}
              >
                <Grid3x3 className="size-4" aria-hidden />
                紧凑
              </button>
            </div>
          ) : null}
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

      {/* 列表主体最小高度：筛选后行数变少/空态时，卡片与分页位置保持稳定不塌。 */}
      <div className="mt-4 flex min-h-[max(360px,calc(100vh-22rem))] flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-panel">
        <FilterBar fields={page.fields} query={query} onChange={setQuery} />
        <div className="flex-1">
        {cardEnabled && viewMode === 'card' ? (
          <CardGrid
            fields={visibleFields}
            rows={list.rows}
            loading={list.loading}
            error={list.error}
            density={density}
            onRowClick={openRow}
            renderActions={rowActions}
            onRetry={list.reload}
            onReset={resetQuery}
          />
        ) : (
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
        )}
        </div>
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
