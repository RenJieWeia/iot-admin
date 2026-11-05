import { useQuery } from '@tanstack/react-query';
import { fetchOptions } from './client';
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

/** 把选项列表转成 value → label 映射，供单元格渲染。 */
export function toLabelMap(items: OptionItem[] | undefined): Record<string, string> {
  const map: Record<string, string> = {};
  for (const item of items ?? []) {
    map[item.value] = item.label;
  }
  return map;
}
