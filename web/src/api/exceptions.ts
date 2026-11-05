import { useQuery } from '@tanstack/react-query';
import { fetchList, type ListQuery, type ListResponse } from './client';

/**
 * 异常两页的只读列表（R-019：`GET /admin/v1/exceptions/{events|intervals}`）。
 * 与实体列表共用 QueryState 与 meta 结构，但不提供任何写操作。
 */
export function fetchExceptionList(
  slot: string,
  query: ListQuery,
  signal?: AbortSignal,
): Promise<ListResponse> {
  return fetchList(`exceptions/${slot}`, query, signal);
}

export function useExceptionList(slot: string, query: ListQuery, enabled = true) {
  return useQuery({
    queryKey: ['exceptions', slot, JSON.stringify(query)],
    queryFn: ({ signal }) => fetchExceptionList(slot, query, signal),
    enabled,
    placeholderData: (prev) => prev,
  });
}
