import type {
  CursorMeta,
  Envelope,
  ListMeta,
  MetaPayload,
  OptionItem,
  RowData,
} from './types';

/** SPA 与 Go 进程同源：dev 期由 vite proxy 转发 /admin。 */
const BASE = '/admin/v1';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail: string;

  constructor(status: number, message: string) {
    // message 形如 "invalid_query: 未知排序字段 ..."，前半段当 code，整段当 detail。
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = message.replace(/:.*$/, '') || `http_${status}`;
    this.detail = message;
  }
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
    throw new ApiError(response.status, payload?.message ?? `HTTP ${response.status}`);
  }
  return payload ?? { code: 0, message: 'ok' };
}

export async function fetchMeta(): Promise<MetaPayload> {
  const payload = await request<MetaPayload>('/_meta');
  return payload.data as MetaPayload;
}

export type ListKind = 'entity' | 'readonly' | 'exception';

export function listPath(kind: ListKind, name: string): string {
  switch (kind) {
    case 'readonly':
      return `/readonly/${name}`;
    case 'exception':
      return `/exceptions/${name}`;
    default:
      return `/${name}`;
  }
}

export interface ListResult {
  rows: RowData[];
  meta: ListMeta;
  cursor?: CursorMeta;
}

/** 列表查询：分页/筛选/排序全部交给服务端（白名单在 Go 侧）。 */
export async function fetchList(
  kind: ListKind,
  name: string,
  params: URLSearchParams,
  signal?: AbortSignal,
): Promise<ListResult> {
  const suffix = params.toString();
  const payload = await request<RowData[]>(`${listPath(kind, name)}${suffix ? `?${suffix}` : ''}`, {
    signal,
  });
  const meta = payload.meta;
  return {
    rows: Array.isArray(payload.data) ? payload.data : [],
    meta: {
      total: meta?.total ?? 0,
      page: meta?.page ?? 1,
      pageSize: meta?.pageSize ?? 10,
      sort: meta?.sort ?? '',
      order: (meta?.order as 'asc' | 'desc') ?? 'desc',
    },
    cursor: meta && typeof meta.hasMore === 'boolean'
      ? {
          hasMore: meta.hasMore,
          nextCursor: meta.nextCursor ?? null,
          limit: meta.limit ?? 0,
        }
      : undefined,
  };
}

export interface SnapshotQueryColumn {
  key: string;
  label: string;
  unit?: string;
}

export interface SnapshotQueryCell {
  num?: number;
  text?: string;
  unit?: string;
  time?: string;
  quality?: number;
}

export interface SnapshotQueryRow {
  terminalId: string;
  terminalName: string;
  typeId: number;
  values: Record<string, SnapshotQueryCell>;
}

export interface SnapshotQueryPayload {
  template: { id: number; name: string; description: string };
  granularity: string;
  from: string;
  to: string;
  columns: SnapshotQueryColumn[];
  rows: SnapshotQueryRow[];
}

/** 快照查询：模板 + 档位 + 时间范围 → 终端 × 字段的透视结果。 */
export async function runSnapshotQuery(params: {
  template: number;
  granularity: 'hour' | 'day' | 'month' | 'year';
  from: string;
  to: string;
}): Promise<SnapshotQueryPayload> {
  const search = new URLSearchParams({
    template: String(params.template),
    granularity: params.granularity,
    from: params.from,
    to: params.to,
  });
  const payload = await request<SnapshotQueryPayload>(`/snapshot-query?${search.toString()}`);
  return payload.data as SnapshotQueryPayload;
}

export async function fetchOne(
  entity: string,
  id: string,
  signal?: AbortSignal,
): Promise<RowData> {
  const payload = await request<RowData>(`/${entity}/${encodeURIComponent(id)}`, { signal });
  return (payload.data ?? {}) as RowData;
}

export async function createRow(entity: string, body: RowData): Promise<RowData> {
  const payload = await request<RowData>(`/${entity}`, {
    method: 'POST',
    body: JSON.stringify(body),
  });
  return (payload.data ?? {}) as RowData;
}

export async function updateRow(entity: string, id: string, body: RowData): Promise<RowData> {
  const payload = await request<RowData>(`/${entity}/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  });
  return (payload.data ?? {}) as RowData;
}

export type BatchScope =
  | { mode: 'ids'; ids: string[] }
  | { mode: 'filter'; q: string; filters: { field: string; op: string; values: string[] }[] };

export interface BatchResult {
  action: 'enable' | 'disable' | 'delete';
  requested: number;
  succeeded: number;
  failed: { id: string; reason: string }[];
  credentials?: { id: string; password: string }[];
}

/** 业务组批量操作：按显式 ID 或当前筛选条件执行，返回逐项结果。 */
export async function batchAction(
  entity: string,
  action: BatchResult['action'],
  scope: BatchScope,
): Promise<BatchResult> {
  const payload = await request<BatchResult>(`/${entity}/batch`, {
    method: 'POST',
    body: JSON.stringify({ action, scope }),
  });
  return (payload.data ?? { action, requested: 0, succeeded: 0, failed: [] }) as BatchResult;
}

export interface RawItemRow {
  id: number;
  message_id: number;
  received_at: string;
  item_code: string;
  value_num: number | null;
  value_text: string | null;
  unit: string | null;
  value_time: string | null;
  quality: number;
  quality_reason: string | null;
  status: 'written' | 'dropped' | 'ignored';
  reason: string | null;
}

/** 异常现场回放：某终端在时间窗内的接入流水。 */
export async function fetchRawItems(terminalID: string, from: string, to: string): Promise<RawItemRow[]> {
  const search = new URLSearchParams({ terminal_id: terminalID, from, to });
  const payload = await request<RawItemRow[]>(`/raw-items?${search.toString()}`);
  return (payload.data ?? []) as RawItemRow[];
}

export interface GatewayCredential {
  gateway_id: string;
  mqtt_topic: string;
  mqtt_username: string;
  mqtt_password?: string;
  password_saved: boolean;
}

/** 读取网关 MQTT 凭据（密码需服务端密钥解密）。 */
export async function fetchGatewayCredential(id: string): Promise<GatewayCredential> {
  const payload = await request<GatewayCredential>(`/gateways/${encodeURIComponent(id)}/credential`);
  return payload.data as GatewayCredential;
}

/** 重置网关 MQTT 密码：Broker 侧重建账号，旧密码立即失效。 */
export async function resetGatewayCredential(id: string): Promise<GatewayCredential> {
  const payload = await request<GatewayCredential>(`/gateways/${encodeURIComponent(id)}/credential/reset`, {
    method: 'POST',
  });
  return payload.data as GatewayCredential;
}

export async function deleteRow(entity: string, id: string): Promise<void> {
  await request<null>(`/${entity}/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

/** 关联选项：ids 精确回填，q 模糊搜索，二者互斥（服务端约束）。 */
export async function fetchOptions(
  entity: string,
  params: { q?: string; ids?: string[]; limit?: number } = {},
  signal?: AbortSignal,
): Promise<OptionItem[]> {
  const search = new URLSearchParams();
  if (params.ids?.length) search.set('ids', params.ids.join(','));
  else {
    if (params.q) search.set('q', params.q);
    if (params.limit) search.set('limit', String(params.limit));
  }
  const suffix = search.toString();
  const payload = await request<OptionItem[]>(
    `/options/${entity}${suffix ? `?${suffix}` : ''}`,
    { signal },
  );
  return Array.isArray(payload.data) ? payload.data : [];
}
