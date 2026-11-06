import type { Envelope, FilterOp, ListMeta, OptionItem, RowData } from './types';

/** API 前缀固定，SPA 与 Go 进程同源部署（dev 期由 vite proxy 转发）。 */
const BASE = '/admin/v1';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail: string;

  constructor(status: number, code: string, detail: string) {
    // detail 直接来自后端 invalid_query: <原因>，保留原文便于定位与展示恢复路径。
    super(detail || code);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.detail = detail;
  }
}

export interface ListQuery {
  q?: string;
  filters?: Array<{ field: string; op: FilterOp | ''; values: string[] }>;
  sort?: string;
  order?: 'asc' | 'desc';
  page?: number;
  pageSize?: number;
}

/**
 * 把 QueryState 序列化成后端白名单认识的 search params：
 * 无操作符的字段用 filter.<col>（服务端取该字段声明的第一个操作符），
 * 有操作符的用 filter.<col>.<op>；in 用重复参数表达多值。
 */
export function toSearchParams(query: ListQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (query.q) params.set('q', query.q);
  if (query.sort) params.set('sort', query.sort);
  if (query.order) params.set('order', query.order);
  if (query.page !== undefined) params.set('page', String(query.page));
  if (query.pageSize !== undefined) params.set('pageSize', String(query.pageSize));
  for (const filter of query.filters ?? []) {
    // op 为空时只发字段名，由服务端按该字段声明的第一个操作符执行。
    const key = filter.op ? `filter.${filter.field}.${filter.op}` : `filter.${filter.field}`;
    for (const value of filter.values) {
      if (value === '') continue;
      params.append(key, value);
    }
  }
  return params;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<Envelope<T>> {
  const response = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...init.headers,
    },
  });
  let payload: Envelope<T> | null = null;
  try {
    payload = (await response.json()) as Envelope<T>;
  } catch {
    payload = null;
  }
  if (!response.ok || (payload && payload.code !== 0)) {
    const message = payload?.message ?? '';
    const code = message.replace(/:.*$/, '') || `http_${response.status}`;
    const detail = message || `HTTP ${response.status}`;
    throw new ApiError(response.status, code, detail);
  }
  return payload ?? { code: 0, message: 'ok' };
}

export interface ListResponse {
  rows: RowData[];
  meta: ListMeta;
}

export async function fetchList(
  entity: string,
  query: ListQuery,
  signal?: AbortSignal,
): Promise<ListResponse> {
  const suffix = toSearchParams(query).toString();
  const payload = await request<RowData[]>(`/${entity}${suffix ? `?${suffix}` : ''}`, { signal });
  return {
    rows: Array.isArray(payload.data) ? payload.data : [],
    meta: payload.meta ?? {
      total: 0,
      page: query.page ?? 1,
      pageSize: query.pageSize ?? 10,
      sort: query.sort ?? '',
      order: query.order ?? 'desc',
    },
  };
}

/**
 * 关联字段选项（R-019：GET /admin/v1/options/{entity}，上限 1000 行）。
 * 用于关系下拉、关系筛选项与关系列的名称回填。
 */
/** 下拉搜索：q 为空时按无参查询（后端等价于全量前 limit 行）。 */
export async function fetchOptionsSearch(
  entity: string,
  params: { q?: string; limit?: number },
  signal?: AbortSignal,
): Promise<OptionItem[]> {
  const search = new URLSearchParams();
  if (params.q) search.set('q', params.q);
  if (params.limit) search.set('limit', String(params.limit));
  const suffix = search.toString();
  const payload = await request<OptionItem[]>(
    `/options/${entity}${suffix ? `?${suffix}` : ''}`,
    { signal },
  );
  return Array.isArray(payload.data) ? payload.data : [];
}

export async function fetchOptions(entity: string, signal?: AbortSignal): Promise<OptionItem[]> {
  const payload = await request<OptionItem[]>(`/options/${entity}`, { signal });
  return Array.isArray(payload.data) ? payload.data : [];
}


export async function fetchOne(entity: string, id: string, signal?: AbortSignal): Promise<RowData> {
  const payload = await request<RowData>(`/${entity}/${encodeURIComponent(id)}`, { signal });
  return payload.data ?? {};
}

export async function createRow(entity: string, body: RowData): Promise<void> {
  await request<RowData>(`/${entity}`, { method: 'POST', body: JSON.stringify(body) });
}

export async function updateRow(entity: string, id: string, body: RowData): Promise<void> {
  await request<RowData>(`/${entity}/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  });
}

export async function deleteRow(entity: string, id: string): Promise<void> {
  await request<null>(`/${entity}/${encodeURIComponent(id)}`, { method: 'DELETE' });
}
