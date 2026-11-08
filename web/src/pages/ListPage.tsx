import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Check, Plus, RotateCw } from 'lucide-react';
import * as ContextMenu from '@radix-ui/react-context-menu';
import { deleteRow, updateRow } from '../api/client';
import type { ListKind } from '../api/client';
import type { MetaPayload, PageMeta, ReadonlyMeta, RowData } from '../api/types';
import { cn } from '../lib/cn';
import { isBlank } from '../lib/format';
import { initQuery, setFilter } from '../lib/query';
import type { FilterCond, QueryState } from '../lib/query';
import { useMeta, useRelationLabels } from '../meta';
import { useList } from '../hooks/useList';
import { DataTable } from '../components/DataTable';
import { FilterBar } from '../components/FilterBar';
import { Pagination } from '../components/Pagination';
import { CursorPager } from '../components/CursorPager';
import { EmptyState } from '../components/States';
import { RecordDrawer } from '../components/RecordDrawer';
import { GatewayEnabledSwitch } from '../components/GatewayEnabledSwitch';
import { TerminalEnabledSwitch } from '../components/TerminalEnabledSwitch';
import { CardGrid } from '../components/CardGrid';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { RowMenu } from '../components/RowMenu';
import type { RowMenuItem } from '../components/RowMenu';
import { useToast } from '../components/Toast';
import { readDensity, readViewMode, writeDensity, writeViewMode } from '../lib/viewPrefs';
import type { CardDensity, ViewMode } from '../lib/viewPrefs';

/** 业务组（非快照实体）的列表都支持卡片视图；只读页/异常页仍只用表格。 */
function supportsCard(kind: ListKind, name: string): boolean {
  return kind === 'entity' && !name.startsWith('snapshot');
}

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
  const location = useLocation();
  const toast = useToast();
  const relationLabel = useRelationLabels();
  const [rowMenu, setRowMenu] = useState<{ x: number; y: number; row: RowData } | null>(null);
  const [pendingDelete, setPendingDelete] = useState<RowData | null>(null);
  const [deleting, setDeleting] = useState(false);
  const cardEnabled = supportsCard(kind, name);
  // 视图与密度都按实体记在浏览器里；切实体时重新读取自己的偏好。
  const [viewMode, setViewMode] = useState<ViewMode>(() => (cardEnabled ? readViewMode(name) : 'table'));
  const [density, setDensity] = useState<CardDensity>(() => readDensity(name));

  useEffect(() => {
    setViewMode(cardEnabled ? readViewMode(name) : 'table');
    setDensity(readDensity(name));
  }, [name, cardEnabled]);

  // 跳转带来的筛选只在"切换实体/首次挂载"时消费一次：用 ref 抓当前值，避免清空 state 触发重算。
  const incomingFilters = useRef<FilterCond[] | undefined>(undefined);
  if (location.state) {
    incomingFilters.current = (location.state as { filters?: FilterCond[] }).filters;
  }

  const changeView = (mode: ViewMode) => {
    setViewMode(mode);
    writeViewMode(name, mode);
  };
  const changeDensity = (next: CardDensity) => {
    setDensity(next);
    writeDensity(name, next);
  };

  // 切页 / 换实体：按页面默认排序重置查询，并把跳转带来的筛选合入（只消费一次）。
  useEffect(() => {
    let next = initQuery(page?.defaultSort ?? '', page?.defaultOrder ?? 'desc', defaultPageSize);
    const fromState = incomingFilters.current;
    if (kind === 'entity' && fromState?.length) {
      for (const filter of fromState) next = setFilter(next, filter);
      incomingFilters.current = undefined;
      navigate(location.pathname, { replace: true, state: null });
    }
    setQuery(next);
  }, [kind, name, defaultPageSize, page?.defaultSort, page?.defaultOrder, navigate, location.pathname]);

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
      // 物模型的"绑定关系"在详情页（字段 + 终端实例），行点击直接进详情。
      if (kind === 'entity' && name === 'thing-models' && row.id !== undefined) {
        navigate(`/e/thing-models/${String(row.id)}`);
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
      : kind === 'entity' && page.name === 'terminals'
        ? (row: RowData) => <TerminalEnabledSwitch row={row} onSaved={list.reload} />
        : undefined;
  const sortLabel = query.sort
    ? (page.fields.find((field) => field.key === query.sort)?.label ?? query.sort)
    : '默认';

  const displayValue = (field: typeof page.fields[number], value: unknown): string => {
    if (field.type === 'relation') return relationLabel(field.ref, value);
    const hit = field.options.find((option) => option.value === String(value));
    return hit?.label ?? String(value);
  };

  const appendFilter = (field: typeof page.fields[number], value: unknown) => {
    setQuery((current) => {
      const existing = current.filters.find((filter) => filter.field === field.key);
      const values = [...new Set([...(existing?.values ?? []), String(value)])];
      const others = current.filters.filter((filter) => filter.field !== field.key);
      return {
        ...current,
        filters: [...others, { field: field.key, op: field.operators[0], values }],
        page: 1,
      };
    });
  };

  const buildRowMenu = (row: RowData): RowMenuItem[] => {
    if (kind !== 'entity' || !page || name.startsWith('snapshot')) return [];
    const items: RowMenuItem[] = [];
    const rowID = String(row.id ?? row.gateway_id ?? '');

    // ① 启停
    if (['terminals', 'gateways', 'quality-rules'].includes(name) && rowID) {
      const enabled = row.enabled === true;
      items.push({
        key: 'toggle',
        section: '操作',
        label: enabled ? '停用' : '启用',
        danger: enabled,
        onSelect: () => {
          void (async () => {
            try {
              await updateRow(name, rowID, { enabled: !enabled });
              toast.success(enabled ? '已停用' : '已启用');
              list.reload();
            } catch (err) {
              toast.error(err instanceof Error ? err.message : '操作失败');
            }
          })();
        },
      });
    }

    // ② 快速筛选（追加到当前筛选）
    const filterKeys: Record<string, string[]> = {
      terminals: ['type_id', 'gateway_id', 'online', 'enabled'],
      gateways: ['gateway_id', 'enabled'],
      'thing-models': ['domain_id'],
      'data-domains': ['code', 'name'],
      'quality-rules': ['terminal_id', 'item_code', 'rule_type'],
    };
    for (const key of filterKeys[name] ?? []) {
      const field = page.fields.find((candidate) => candidate.key === key);
      if (!field || isBlank(row[key])) continue;
      items.push({
        key: `filter-${key}`,
        section: '按当前值筛选',
        label: `${field.label}：${displayValue(field, row[key])}`,
        onSelect: () => appendFilter(field, row[key]),
      });
    }

    // ③ 跳转
    const jump = (key: string, label: string, to: string) => items.push({ key, section: '跳转', label, onSelect: () => navigate(to) });
    const jumpWithFilter = (key: string, label: string, path: string, filter: FilterCond) =>
      items.push({
        key,
        section: '跳转',
        label,
        onSelect: () => navigate(path, { state: { filters: [filter] } }),
      });

    switch (name) {
      case 'terminals': {
        if (!isBlank(row.type_id)) jump('thing-model', '查看所属物模型', `/e/thing-models/${String(row.type_id)}`);
        if (!isBlank(row.gateway_id)) {
          jumpWithFilter('gateway', '查看网关列表（按此网关筛选）', '/e/gateways', {
            field: 'gateway_id',
            op: 'eq',
            values: [String(row.gateway_id)],
          });
        }
        break;
      }
      case 'gateways': {
        jumpWithFilter('terminals', '查看下挂终端', '/e/terminals', {
          field: 'gateway_id',
          op: 'eq',
          values: [String(row.gateway_id)],
        });
        break;
      }
      case 'thing-models': {
        jumpWithFilter('terminals', '查看该物模型下终端', '/e/terminals', {
          field: 'type_id',
          op: 'in',
          values: [String(row.id)],
        });
        break;
      }
      case 'data-domains': {
        jumpWithFilter('thing-models', '查看该数据域下物模型', '/e/thing-models', {
          field: 'domain_id',
          op: 'in',
          values: [String(row.id)],
        });
        break;
      }
      case 'quality-rules': {
        if (!isBlank(row.terminal_id)) jump('terminal', '查看终端详情', `/e/terminals/${String(row.terminal_id)}`);
        break;
      }
    }

    // ④ 删除（终端的删除是软删除：标记 + 隐藏，保留历史数据）
    const deletable = ['data-domains', 'thing-models', 'terminals', 'gateways', 'quality-rules'];
    if (deletable.includes(name) && rowID) {
      items.push({
        key: 'delete',
        section: '危险操作',
        label: '删除',
        danger: true,
        onSelect: () => setPendingDelete(row),
      });
    }
    return items;
  };

  const confirmDelete = async () => {
    if (!pendingDelete || !page) return;
    setDeleting(true);
    try {
      await deleteRow(name, String(pendingDelete.id ?? pendingDelete.gateway_id ?? ''));
      toast.success(`已删除${page.title}`);
      setPendingDelete(null);
      list.reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除失败');
    } finally {
      setDeleting(false);
    }
  };

  const deleteDescription = (() => {
    if (!pendingDelete) return '';
    const label = String(pendingDelete.name ?? pendingDelete.id ?? pendingDelete.gateway_id ?? '');
    switch (name) {
      case 'terminals':
        return `「${label}」将被标记为已删除并从列表隐藏；最新值/快照等历史数据保留。`;
      case 'thing-models':
        return `删除物模型「${label}」会同时删除它绑定的字段；不可恢复。`;
      case 'gateways':
        return `删除网关「${label}」会同时注销其 Broker 账号；下挂终端需先解绑。`;
      case 'data-domains':
        return `删除数据域「${label}」前需先清理它下面的物模型。`;
      default:
        return `删除「${label}」后不可恢复。`;
    }
  })();

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

      {/* 列表主体最小高度：筛选后行数变少/空态时，卡片与分页位置保持稳定不塌；
          视口较高时基本吃满顶栏以下的剩余空间。 */}
      <div className="mt-4 flex min-h-[max(420px,calc(100vh-15rem))] flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-panel">
        <FilterBar fields={page.fields} query={query} onChange={setQuery} />
        {/* 样式选项只在表格/卡片区域右击出现；筛选栏与分页条保持浏览器默认菜单。 */}
        <ContextMenu.Root>
          <ContextMenu.Trigger asChild>
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
            onRowContextMenu={(event, row) => setRowMenu({ x: event.clientX, y: event.clientY, row })}
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
            density={density}
            onSortChange={handleSort}
            onRowClick={openRow}
            renderActions={rowActions}
            onRowContextMenu={(event, row) => setRowMenu({ x: event.clientX, y: event.clientY, row })}
            onRetry={list.reload}
            onReset={resetQuery}
          />
        )}
            </div>
          </ContextMenu.Trigger>
          <ContextMenu.Portal>
            <ContextMenu.Content className="z-50 min-w-44 rounded-lg border border-line bg-surface p-1 shadow-panel">
              <ContextMenu.Label className="px-2 py-1.5 text-xs text-muted">表格样式</ContextMenu.Label>
              {cardEnabled ? (
                <>
                  <ContextMenu.Label className="px-2 pt-1 pb-0.5 text-xs text-muted">视图</ContextMenu.Label>
                  <ContextMenu.RadioGroup value={viewMode} onValueChange={(value) => changeView(value as ViewMode)}>
                    <ContextMenu.RadioItem
                      value="table"
                      className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2"
                    >
                      <ContextMenu.ItemIndicator><Check className="size-3.5" aria-hidden /></ContextMenu.ItemIndicator>
                      表格
                    </ContextMenu.RadioItem>
                    <ContextMenu.RadioItem
                      value="card"
                      className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2"
                    >
                      <ContextMenu.ItemIndicator><Check className="size-3.5" aria-hidden /></ContextMenu.ItemIndicator>
                      卡片
                    </ContextMenu.RadioItem>
                  </ContextMenu.RadioGroup>
                  <ContextMenu.Separator className="my-1 h-px bg-line" />
                </>
              ) : null}
              <ContextMenu.Label className="px-2 pt-1 pb-0.5 text-xs text-muted">密度</ContextMenu.Label>
              <ContextMenu.RadioGroup value={density} onValueChange={(value) => changeDensity(value as CardDensity)}>
                <ContextMenu.RadioItem
                  value="comfortable"
                  className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2"
                >
                  <ContextMenu.ItemIndicator><Check className="size-3.5" aria-hidden /></ContextMenu.ItemIndicator>
                  适中
                </ContextMenu.RadioItem>
                <ContextMenu.RadioItem
                  value="compact"
                  className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2"
                >
                  <ContextMenu.ItemIndicator><Check className="size-3.5" aria-hidden /></ContextMenu.ItemIndicator>
                  紧凑
                </ContextMenu.RadioItem>
              </ContextMenu.RadioGroup>
            </ContextMenu.Content>
          </ContextMenu.Portal>
        </ContextMenu.Root>
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

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title={`删除${page.title}`}
        description={deleteDescription}
        confirmLabel="删除"
        loading={deleting}
        onConfirm={() => void confirmDelete()}
      />

      {rowMenu ? (
        <RowMenu
          x={rowMenu.x}
          y={rowMenu.y}
          items={buildRowMenu(rowMenu.row)}
          onClose={() => setRowMenu(null)}
        />
      ) : null}

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
