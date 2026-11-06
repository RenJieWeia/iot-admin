import { useQuery } from '@tanstack/react-query';
import { fetchOptions, fetchOptionsSearch } from './client';
import type { OptionItem } from './types';

/**
 * 关联字段选项（GET /admin/v1/options/{entity}）：
 * 一次取某实体的 value/label 列表，供关系下拉、筛选项与列名称回填共用。
 */
export function useFieldOptions(entities: string[]) {
  const key = [...entities].sort().join(',');
  return useQuery({
    queryKey: ['options', key],
    queryFn: async ({ signal }) => {
      const entries = await Promise.all(
        entities.map(
          async (entity) => [entity, await fetchOptions(entity, signal)] as const,
        ),
      );
      const map: Record<string, OptionItem[]> = {};
      for (const [entity, items] of entries) map[entity] = items;
      return map;
    },
    enabled: entities.length > 0,
    staleTime: 60 * 1000,
  });
}

/**
 * 下拉搜索（F4）：`GET /admin/v1/options/{entity}?q=…&limit=…`。
 * q 作为 queryKey 的一部分，配合 fetch 的 AbortSignal，旧响应不会覆盖新结果。
 */
export function useOptionSearch(entity: string | undefined, q: string, enabled: boolean) {
  return useQuery({
    queryKey: ['option-search', entity ?? '', q],
    queryFn: ({ signal }) => fetchOptionsSearch(entity ?? '', { q, limit: 50 }, signal),
    enabled: Boolean(entity) && enabled,
    staleTime: 30 * 1000,
    placeholderData: (prev) => prev,
  });
}

/** 把选项列表转成 value → label 映射，供单元格渲染。 */
export function toLabelMap(items: OptionItem[] | undefined): Record<string, string> {
  const map: Record<string, string> = {};
  for (const item of items ?? []) {
    map[item.value] = item.label;
  }
  return map;
}
