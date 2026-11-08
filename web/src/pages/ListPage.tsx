import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Check, Plus, RotateCw } from 'lucide-react';
import * as ContextMenu from '@radix-ui/react-context-menu';
import { batchAction, deleteRow, fetchGatewayCredential, resetGatewayCredential, updateRow } from '../api/client';
import type { BatchResult, BatchScope, GatewayCredential } from '../api/client';
import type { ListKind } from '../api/client';
import type { MetaPayload, PageMeta, ReadonlyMeta, RowData } from '../api/types';
import { cn } from '../lib/cn';
import { isBlank } from '../lib/format';
import { filterSearch, queryFromSearch, searchFromQuery } from '../lib/queryUrl';
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
import { BatchResultDialog } from '../components/BatchResultDialog';
import { CredentialDialog } from '../components/CredentialDialog';
import { RowMenu } from '../components/RowMenu';
import type { RowMenuEntry, RowMenuItem } from '../components/RowMenu';
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
    queryFromSearch(window.location.search, page, defaultPageSize, page?.pagination === 'cursor'),
  );
  const [drawer, setDrawer] = useState<DrawerState>({ open: false, row: null, mode: 'view' });
  const navigate = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const relationLabel = useRelationLabels();
  const [rowMenu, setRowMenu] = useState<{ x: number; y: number; row: RowData; batch: boolean } | null>(null);
  const [pendingDelete, setPendingDelete] = useState<RowData | null>(null);
  const [deleting, setDeleting] = useState(false);
  // 批量选择：ids=显式勾选（可跨页），filter=当前筛选结果（服务端按条件执行）。
  const [selection, setSelection] = useState<
    { mode: 'ids'; ids: string[] } | { mode: 'filter'; q: string; filters: FilterCond[]; total: number } | null
  >(null);
  const [batchResult, setBatchResult] = useState<BatchResult | null>(null);
  const [batchBusy, setBatchBusy] = useState(false);
  const [pendingBatchDelete, setPendingBatchDelete] = useState(false);
  const clickTimer = useRef<number | null>(null);
  const [credentialOpen, setCredentialOpen] = useState(false);
  const [credentialLoading, setCredentialLoading] = useState(false);
  const [credentialError, setCredentialError] = useState<string | null>(null);
  const [credential, setCredential] = useState<GatewayCredential | null>(null);
  const [pendingReset, setPendingReset] = useState<RowData | null>(null);
  const cardEnabled = supportsCard(kind, name);
  // 视图与密度都按实体记在浏览器里；切实体时重新读取自己的偏好。
  const [viewMode, setViewMode] = useState<ViewMode>(() => (cardEnabled ? readViewMode(name) : 'table'));
  const [density, setDensity] = useState<CardDensity>(() => readDensity(name));

  useEffect(() => {
    setViewMode(cardEnabled ? readViewMode(name) : 'table');
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

  // 地址栏是筛选/排序/分页的事实源：URL 变化 → 解析进查询；查询变化 → 写回 URL（replace）。
  const cursorMode = page?.pagination === 'cursor';
  useEffect(() => {
    const parsed = queryFromSearch(location.search, page, defaultPageSize, cursorMode);
    setQuery((current) => (searchFromQuery(current, cursorMode) === searchFromQuery(parsed, cursorMode) ? current : parsed));
  }, [location.search, page, defaultPageSize, cursorMode]);

  // 记录最近一次由本页写入的 URL：外部导航（快速跳转/前进后退）带来的 search 不覆盖成旧查询。
  const lastSyncedSearch = useRef(location.search);
  useEffect(() => {
    if (location.search !== lastSyncedSearch.current) {
      lastSyncedSearch.current = location.search;
      return;
    }
    const next = searchFromQuery(query, cursorMode);
    const current = location.search.replace(/^\?/, '');
    if (next !== current) {
      lastSyncedSearch.current = next ? `?${next}` : '';
      navigate(`${location.pathname}${next ? `?${next}` : ''}`, { replace: true, state: null });
    }
  }, [query, cursorMode, location.pathname, location.search, navigate]);

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

  const rowKey = (row: RowData) => String(row.id ?? row.gateway_id ?? '');
  const canToggle = ['terminals', 'gateways', 'quality-rules'].includes(name);
  const selectionCount = selection?.mode === 'filter' ? selection.total : (selection?.ids.length ?? 0);
  const selectedKeys = useMemo(() => {
    if (!selection) return undefined;
    if (selection.mode === 'ids') return new Set(selection.ids);
    return new Set(list.rows.map(rowKey));
  }, [selection, list.rows]);

  const clearSelection = () => setSelection(null);

  // 选中状态下按 Esc 取消选择；右键菜单/对话框打开时先交给它们自己处理。
  useEffect(() => {
    if (!selection) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      if (rowMenu || pendingDelete || pendingBatchDelete || batchResult) return;
      clearSelection();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selection, rowMenu, pendingDelete, pendingBatchDelete, batchResult]);
  const toggleSelect = (row: RowData) => {
    const key = rowKey(row);
    setSelection((current) => {
      if (!current || current.mode === 'filter') return { mode: 'ids', ids: [key] };
      const ids = current.ids.includes(key)
        ? current.ids.filter((id) => id !== key)
        : [...current.ids, key];
      return ids.length ? { mode: 'ids', ids } : null;
    });
  };
  const allPageSelected = list.rows.length > 0 && list.rows.every((row) => selection?.mode === 'ids' && selection.ids.includes(rowKey(row)));
  const selectAllPage = () => {
    if (allPageSelected) {
      clearSelection();
      return;
    }
    setSelection({ mode: 'ids', ids: list.rows.map(rowKey) });
  };
  const selectAllFiltered = () => {
    setSelection({ mode: 'filter', q: query.q, filters: query.filters, total: list.meta?.total ?? list.rows.length });
  };

  const runBatch = async (action: BatchResult['action']) => {
    if (!selection) return;
    const scope: BatchScope =
      selection.mode === 'ids'
        ? { mode: 'ids', ids: selection.ids }
        : {
            mode: 'filter',
            q: selection.q,
            filters: selection.filters.map((filter) => ({
              field: filter.field,
              op: filter.op ?? '',
              values: filter.values,
            })),
          };
    setBatchBusy(true);
    try {
      const result = await batchAction(name, action, scope);
      setBatchResult(result);
      setSelection(null);
      setPendingBatchDelete(false);
      list.reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '批量操作失败');
    } finally {
      setBatchBusy(false);
    }
  };

  // 查询条件变化（关键词/筛选/排序/每页条数）即清空选择；翻页保留显式选择。
  const querySignature = `${query.q}|${JSON.stringify(query.filters)}|${query.sort}|${query.order}|${query.pageSize}`;
  const previousSignature = useRef(querySignature);
  useEffect(() => {
    if (previousSignature.current !== querySignature) {
      previousSignature.current = querySignature;
      setSelection(null);
    }
  }, [querySignature]);
  useEffect(() => {
    setSelection(null);
  }, [name]);

  // 已处于选择状态时，单击对象即切换选中；未选择时才延迟进详情（给双击留判定窗口）。
  const handleRowClick = useCallback(
    (row: RowData) => {
      if (clickTimer.current) window.clearTimeout(clickTimer.current);
      clickTimer.current = window.setTimeout(() => {
        clickTimer.current = null;
        if (selection) toggleSelect(row);
        else openRow(row);
      }, 200);
    },
    [openRow, selection],
  );
  const handleRowDoubleClick = (row: RowData) => {
    if (clickTimer.current) {
      window.clearTimeout(clickTimer.current);
      clickTimer.current = null;
    }
    if (selection) {
      // 双击在选择状态下表示“把这个也选上”，不取消已选项。
      const key = rowKey(row);
      setSelection((current) => {
        if (current?.mode !== 'ids') return { mode: 'ids', ids: [key] };
        if (current.ids.includes(key)) return current;
        return { mode: 'ids', ids: [...current.ids, key] };
      });
      return;
    }
    toggleSelect(row);
  };

  const buildRowMenu = (row: RowData, batch: boolean): RowMenuEntry[] => {
    if (kind !== 'entity' || !page || name.startsWith('snapshot')) return [];
    const items: RowMenuEntry[] = [];
    const rowID = rowKey(row);

    // 选中多行时右键给批量菜单；只选中一行时仍给单行菜单。
    if (batch && selection?.mode === 'ids' && selection.ids.length > 1 && selection.ids.includes(rowID)) {
      if (canToggle) {
        items.push({ key: 'batch-enable', section: '批量操作', label: `启用所选 ${selection.ids.length} 项`, onSelect: () => void runBatch('enable') });
        items.push({ key: 'batch-disable', section: '批量操作', label: `停用所选 ${selection.ids.length} 项`, onSelect: () => void runBatch('disable') });
      }
      items.push({ key: 'batch-delete', section: '批量操作', label: `删除所选 ${selection.ids.length} 项`, danger: true, onSelect: () => setPendingBatchDelete(true) });
      items.push({ key: 'batch-clear', section: '批量操作', label: '清除选择', onSelect: clearSelection });
      return items;
    }

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
    const filterChildren: RowMenuItem[] = [];
    for (const key of filterKeys[name] ?? []) {
      const field = page.fields.find((candidate) => candidate.key === key);
      if (!field || isBlank(row[key])) continue;
      filterChildren.push({
        key: `filter-${key}`,
        label: `${field.label}：${displayValue(field, row[key])}`,
        onSelect: () => appendFilter(field, row[key]),
      });
    }
    if (filterChildren.length) {
      items.push({ key: 'filter', section: '筛选', label: '按当前值筛选', children: filterChildren });
    }

    // ③ 跳转
    const jumpChildren: RowMenuItem[] = [];
    const jump = (key: string, label: string, to: string) =>
      jumpChildren.push({ key, label, onSelect: () => navigate(to) });
    const jumpWithFilter = (key: string, label: string, path: string, filter: FilterCond) =>
      jumpChildren.push({
        key,
        label,
        onSelect: () => navigate(`${path}?${filterSearch([filter])}`),
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
        items.push({
          key: 'credential',
          section: '凭据',
          label: '查看 MQTT 凭据',
          onSelect: () => void openCredential(String(row.gateway_id ?? '')),
        });
        items.push({
          key: 'credential-reset',
          section: '凭据',
          label: '重置 MQTT 密码',
          danger: true,
          onSelect: () => setPendingReset(row),
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

    if (jumpChildren.length) {
      items.push({ key: 'jump', section: '跳转', label: '快速跳转', children: jumpChildren });
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

  const openCredential = async (gatewayID: string) => {
    setCredentialOpen(true);
    setCredentialLoading(true);
    setCredentialError(null);
    setCredential(null);
    try {
      setCredential(await fetchGatewayCredential(gatewayID));
    } catch (err) {
      setCredentialError(err instanceof Error ? err.message : '读取凭据失败');
    } finally {
      setCredentialLoading(false);
    }
  };

  const confirmReset = async () => {
    if (!pendingReset) return;
    const id = String(pendingReset.gateway_id ?? '');
    setCredentialLoading(true);
    try {
      const next = await resetGatewayCredential(id);
      setCredential(next);
      setCredentialError(null);
      setCredentialOpen(true);
      setPendingReset(null);
      toast.success('MQTT 密码已重置，旧密码立即失效');
      list.reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '重置失败');
    } finally {
      setCredentialLoading(false);
    }
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

  // 点空白处取消选中；行/卡片、操作控件上的点击不算空白。
  const handleBlankClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!selection) return;
    const target = event.target as HTMLElement;
    if (target.closest('tr') || target.closest('[role="button"][aria-label="查看详情"]')) return;
    if (target.closest('button, input, select, a, [role="menuitem"]')) return;
    clearSelection();
  };

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
          {selection ? (
            <>
              {canToggle ? (
                <>
                  <button
                    type="button"
                    disabled={batchBusy}
                    onClick={() => void runBatch('enable')}
                    className="inline-flex h-9 items-center rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
                  >
                    批量启用
                  </button>
                  <button
                    type="button"
                    disabled={batchBusy}
                    onClick={() => void runBatch('disable')}
                    className="inline-flex h-9 items-center rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
                  >
                    批量停用
                  </button>
                </>
              ) : null}
              <button
                type="button"
                disabled={batchBusy}
                onClick={() => setPendingBatchDelete(true)}
                className="inline-flex h-9 items-center rounded-md bg-negative px-3 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
              >
                批量删除
              </button>
            </>
          ) : (
            <>
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
            </>
          )}
        </div>
      </div>

      {/* 列表主体最小高度：筛选后行数变少/空态时，卡片与分页位置保持稳定不塌；
          视口较高时基本吃满顶栏以下的剩余空间。 */}
      <div className="mt-4 flex min-h-[max(420px,calc(100vh-15rem))] flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-panel">
        <FilterBar
          fields={page.fields}
          query={query}
          onChange={setQuery}
          selectionBar={
            selection ? (
              <>
                <span className="shrink-0 text-xs font-medium text-primary">已选 {selectionCount} 项</span>
                <span className="shrink-0 text-xs text-muted">
                  {selection.mode === 'filter' ? '范围：当前筛选结果' : '范围：已勾选行'}
                </span>
                <button
                  type="button"
                  onClick={selectAllPage}
                  className="shrink-0 rounded px-1 text-xs text-primary hover:underline"
                >
                  {allPageSelected ? '取消本页' : '全选本页'}
                </button>
                {selection.mode === 'ids' && list.meta && list.meta.total > selection.ids.length ? (
                  <button
                    type="button"
                    onClick={selectAllFiltered}
                    className="shrink-0 rounded px-1 text-xs text-primary hover:underline"
                  >
                    选择全部 {list.meta.total} 项
                  </button>
                ) : null}
                <button
                  type="button"
                  onClick={clearSelection}
                  className="ml-auto shrink-0 rounded px-1 text-xs text-muted hover:text-fg"
                >
                  清除
                </button>
              </>
            ) : undefined
          }
        />
        {/* 样式选项只在表格/卡片区域右击出现；筛选栏与分页条保持浏览器默认菜单。 */}
        <ContextMenu.Root>
          <ContextMenu.Trigger asChild>
            <div className="flex-1" onClick={handleBlankClick}>
        {cardEnabled && viewMode === 'card' ? (
          <CardGrid
            fields={visibleFields}
            rows={list.rows}
            loading={list.loading}
            error={list.error}
            density={density}
            onRowClick={handleRowClick}
            onRowDoubleClick={handleRowDoubleClick}
            selectedKeys={selectedKeys}
            renderActions={rowActions}
            onRowContextMenu={(event, row) => {
              const key = rowKey(row);
              const inSelection = selection?.mode === 'ids' && selection.ids.includes(key);
              if (!inSelection) setSelection({ mode: 'ids', ids: [key] });
              setRowMenu({
                x: event.clientX,
                y: event.clientY,
                row,
                batch: inSelection && selection.mode === 'ids' && selection.ids.length > 1,
              });
            }}
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
            onRowClick={handleRowClick}
            onRowDoubleClick={handleRowDoubleClick}
            selectedKeys={selectedKeys}
            renderActions={rowActions}
            onRowContextMenu={(event, row) => {
              const key = rowKey(row);
              const inSelection = selection?.mode === 'ids' && selection.ids.includes(key);
              if (!inSelection) setSelection({ mode: 'ids', ids: [key] });
              setRowMenu({
                x: event.clientX,
                y: event.clientY,
                row,
                batch: inSelection && selection.mode === 'ids' && selection.ids.length > 1,
              });
            }}
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
        open={pendingBatchDelete}
        onOpenChange={(open) => {
          if (!open) setPendingBatchDelete(false);
        }}
        title="批量删除"
        description={
          selection
            ? selection.mode === 'filter'
              ? `将删除当前筛选结果中的全部 ${selection.total} 项，操作不可恢复；有引用的项会被跳过并在结果中列出。`
              : `将删除所选 ${selection.ids.length} 项，操作不可恢复；有引用的项会被跳过并在结果中列出。`
            : ''
        }
        confirmLabel="删除"
        loading={batchBusy}
        onConfirm={() => void runBatch('delete')}
      />

      <BatchResultDialog result={batchResult} onOpenChange={(open) => { if (!open) setBatchResult(null); }} />

      <CredentialDialog
        open={credentialOpen}
        onOpenChange={(open) => {
          setCredentialOpen(open);
          if (!open) {
            setCredential(null);
            setCredentialError(null);
          }
        }}
        loading={credentialLoading}
        error={credentialError}
        credential={credential}
        onReset={() => {
          const id = credential?.gateway_id ?? '';
          if (id) setPendingReset({ gateway_id: id });
        }}
      />

      <ConfirmDialog
        open={pendingReset !== null}
        onOpenChange={(open) => {
          if (!open) setPendingReset(null);
        }}
        title="重置 MQTT 密码"
        description={
          pendingReset
            ? `将为网关「${String(pendingReset.gateway_id ?? '')}」重新生成密码，旧密码立即失效，网关需更新配置后才能继续上报。`
            : ''
        }
        confirmLabel="重置"
        onConfirm={() => void confirmReset()}
      />

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
          items={buildRowMenu(rowMenu.row, rowMenu.batch)}
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
