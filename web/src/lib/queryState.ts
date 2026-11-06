import type { FilterOp } from '../api/types';
import type { ListQuery } from '../api/client';

/**
 * QueryState 的唯一事实源：URL search params。
 *   q / sort / order / page / pageSize / filter.<col>[.<op>]（in 用重复参数）
 * 刷新、浏览器返回、抽屉关闭后都由 URL 还原；前端不做本地过滤/排序/分页
 * （admin-ui-spec table-query §2、§10）。
 */
export const PAGE_SIZE_OPTIONS = [10, 20, 50] as const;
export type PageSize = (typeof PAGE_SIZE_OPTIONS)[number];

export interface ActiveFilter {
  field: string;
  /** 空串表示 URL 里没写操作符，交给服务端按字段声明的第一个操作符执行。 */
  op: FilterOp | '';
  values: string[];
}

export interface QueryState {
  q: string;
  filters: ActiveFilter[];
  sort: string;
  order: 'asc' | 'desc';
  page: number;
  pageSize: PageSize;
  /** 游标分页位置；offset 分页恒为空串。刷新/返回靠它复原同一批。 */
  cursor: string;
}

export const DEFAULT_QUERY: QueryState = {
  q: '',
  filters: [],
  sort: '',
  order: 'desc',
  page: 1,
  pageSize: 10,
  cursor: '',
};

const OPS: ReadonlySet<string> = new Set(['contains', 'eq', 'prefix', 'in', 'gte', 'lte']);

function toInt(raw: string | null, fallback: number): number {
  if (raw === null || raw.trim() === '') return fallback;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

export function parseQueryState(params: URLSearchParams): QueryState {
  const pageSizeRaw = toInt(params.get('pageSize'), DEFAULT_QUERY.pageSize);
  const pageSize = (PAGE_SIZE_OPTIONS as readonly number[]).includes(pageSizeRaw)
    ? (pageSizeRaw as PageSize)
    : DEFAULT_QUERY.pageSize;

  // 逐 key 收集，避免重复参数被同一个 field 覆盖。
  const filters: ActiveFilter[] = [];
  const seen = new Set<string>();
  for (const key of params.keys()) {
    if (!key.startsWith('filter.') || seen.has(key)) continue;
    seen.add(key);
    const rest = key.slice('filter.'.length);
    const dot = rest.lastIndexOf('.');
    const maybeOp = dot > 0 ? rest.slice(dot + 1) : '';
    const isOp = OPS.has(maybeOp);
    const field = isOp ? rest.slice(0, dot) : rest;
    const values = params.getAll(key).filter((v) => v !== '');
    if (!field || values.length === 0) continue;
    filters.push({ field, op: isOp ? (maybeOp as FilterOp) : '', values });
  }

  return {
    q: (params.get('q') ?? '').trim(),
    filters,
    sort: params.get('sort') ?? '',
    order: params.get('order') === 'asc' ? 'asc' : 'desc',
    page: toInt(params.get('page'), 1),
    pageSize,
    cursor: params.get('cursor') ?? '',
  };
}

/** 序列化回 URL；空值不落 URL，保持地址可读、可分享。 */
export function toSearchParams(state: QueryState): URLSearchParams {
  const params = new URLSearchParams();
  if (state.q) params.set('q', state.q);
  if (state.sort) params.set('sort', state.sort);
  if (state.sort && state.order !== 'desc') params.set('order', state.order);
  if (state.page > 1) params.set('page', String(state.page));
  if (state.pageSize !== DEFAULT_QUERY.pageSize) params.set('pageSize', String(state.pageSize));
  if (state.cursor) params.set('cursor', state.cursor);
  for (const filter of state.filters) {
    const key = filter.op ? `filter.${filter.field}.${filter.op}` : `filter.${filter.field}`;
    for (const value of filter.values) {
      if (value !== '') params.append(key, value);
    }
  }
  return params;
}

/** 转成 API 查询参数（服务端白名单校验，见 internal/admin/query.go）。 */
export function toListQuery(state: QueryState): ListQuery {
  return {
    q: state.q || undefined,
    filters: state.filters.map((f) => ({
      field: f.field,
      op: f.op,
      values: f.values,
    })),
    sort: state.sort || undefined,
    order: state.sort ? state.order : undefined,
    page: state.page,
    pageSize: state.pageSize,
  };
}

/** 条件类变化（关键词/筛选/排序/每页条数）一律回第一页（table-query §2、§6）。 */
export function patchQuery(state: QueryState, patch: Partial<QueryState>): QueryState {
  const next: QueryState = { ...state, ...patch };
  const resetsPage =
    patch.q !== undefined ||
    patch.filters !== undefined ||
    patch.sort !== undefined ||
    patch.order !== undefined ||
    patch.pageSize !== undefined;
  if (resetsPage) {
    // 条件一变回第一页，游标位置同时失效（table-query §2）。
    if (patch.page === undefined) next.page = 1;
    if (patch.cursor === undefined) next.cursor = '';
  }
  return next;
}

export function upsertFilter(state: QueryState, filter: ActiveFilter): QueryState {
  const others = state.filters.filter((f) => f.field !== filter.field);
  return patchQuery(state, { filters: [...others, filter] });
}

export function removeFilterAt(state: QueryState, field: string, op: FilterOp | ''): QueryState {
  return patchQuery(state, {
    filters: state.filters.filter((f) => !(f.field === field && f.op === op)),
  });
}

export function clearFilters(state: QueryState): QueryState {
  return patchQuery(state, { filters: [], q: '' });
}

/**
 * 错误恢复：连 sort/order/page 一起清空，回默认视图。
 * clearFilters 保留排序，遇到 `?sort=非法` 这类 400 时重试必然再失败（table-query §10、§11）。
 */
export function resetQuery(state: QueryState): QueryState {
  return { ...DEFAULT_QUERY, pageSize: state.pageSize };
}

export function setSort(state: QueryState, field: string): QueryState {
  // 同列再点一次切换方向；换列从 desc 开始（与后端默认 id desc 同向）。
  const order = state.sort === field && state.order === 'desc' ? 'asc' : 'desc';
  return patchQuery(state, { sort: field, order });
}

export function hasActiveConditions(state: QueryState): boolean {
  return state.q !== '' || state.filters.length > 0;
}

/** 抽屉地址参数：drawer=new 或 drawer=<id>，与 SSR 版保持一致，刷新后可还原。 */
export function drawerParam(params: URLSearchParams): string | null {
  const raw = params.get('drawer');
  return raw && raw.trim() !== '' ? raw.trim() : null;
}

export function withDrawer(params: URLSearchParams, value: string | null): URLSearchParams {
  const next = new URLSearchParams(params);
  if (value === null) next.delete('drawer');
  else next.set('drawer', value);
  return next;
}
