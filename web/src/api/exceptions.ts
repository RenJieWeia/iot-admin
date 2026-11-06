import { useQuery } from '@tanstack/react-query';
import { fetchList, type ListQuery, type ListResponse } from './client';

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
