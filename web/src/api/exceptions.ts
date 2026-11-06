import { useQuery } from '@tanstack/react-query';
import { fetchCursorPage, fetchList, toSearchParams, type ListQuery, type ListResponse } from './client';
import type { CursorMeta, RowData } from './types';

/**
 * 只读数据源（task-009 §2）：`exceptions` 与 `readonly` 走同一套 QueryState / meta 结构，
 * 只是 URL 前缀不同。两者都不提供任何写操作。
 */
export type SlotSource = 'exceptions' | 'readonly';

export function fetchSlotList(
  source: SlotSource,
  slot: string,
  query: ListQuery,
  signal?: AbortSignal,
): Promise<ListResponse> {
  return fetchList(`${source}/${slot}`, query, signal);
}

export function useSlotList(
  source: SlotSource,
  slot: string,
  query: ListQuery,
  enabled = true,
) {
  return useQuery({
    queryKey: [source, slot, JSON.stringify(query)],
    queryFn: ({ signal }) => fetchSlotList(source, slot, query, signal),
    enabled,
    placeholderData: (prev) => prev,
  });
}

/** 异常两页的调用点保持不变。 */
export function useExceptionList(slot: string, query: ListQuery, enabled = true) {
  return useSlotList('exceptions', slot, query, enabled);
}

/** 游标分页的只读源（快照页）：cursor 留在 URL 里，刷新/后退可复原同一批。 */
export function useSlotCursorList(
  source: SlotSource,
  slot: string,
  query: { q: string; filters: ListQuery['filters']; cursor: string; limit?: number },
  enabled = true,
) {
  return useQuery({
    queryKey: [source, slot, 'cursor', JSON.stringify(query)],
    queryFn: async ({ signal }): Promise<{ rows: RowData[]; meta: CursorMeta }> => {
      const params = toSearchParams({ q: query.q || undefined, filters: query.filters });
      if (query.cursor) params.set('cursor', query.cursor);
      if (query.limit) params.set('limit', String(query.limit));
      return fetchCursorPage(`${source}/${slot}`, params, signal);
    },
    enabled,
    placeholderData: (prev) => prev,
  });
}
