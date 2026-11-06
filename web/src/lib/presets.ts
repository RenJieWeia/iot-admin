import type { FieldMeta } from '../api/types';
import { patchQuery, type QueryState } from './queryState';

/**
 * 只读页的快捷筛选开关（preset）。
 *
 * 两项都是本批固定的产品语义（task-009 §2、task-010 §4.3）：字段键与阈值集中在这里，
 * 页面只在「字段存在且支持 gte」时渲染对应开关，其他只读页天然不受影响。
 * 不新增 SQL 操作符，两端都用 `field.gte=1` 表达。
 */
export interface FilterPreset {
  field: string;
  value: string;
  label: string;
  hint: string;
}

const PRESET_RULES: readonly FilterPreset[] = [
  { field: 'quality', value: '1', label: '仅看有问题', hint: '质量 ≥ 1' },
  { field: 'revision', value: '1', label: '仅看被修正过', hint: '修正次数 ≥ 1' },
];

export function presetsFor(fields: FieldMeta[]): FilterPreset[] {
  return PRESET_RULES.filter((preset) =>
    fields.some(
      (field) =>
        field.key === preset.field && field.filterable && field.operators.includes('gte'),
    ),
  );
}

export function isPresetActive(query: QueryState, preset: FilterPreset): boolean {
  return (
    query.filters
      .find((filter) => filter.field === preset.field && filter.op === 'gte')
      ?.values.includes(preset.value) ?? false
  );
}

export function togglePreset(
  query: QueryState,
  preset: FilterPreset,
  active: boolean,
): QueryState {
  const rest = query.filters.filter((filter) => filter.field !== preset.field);
  const filters = active
    ? [...rest, { field: preset.field, op: 'gte' as const, values: [preset.value] }]
    : rest;
  // 条件变化由 patchQuery 统一把游标与页码复位。
  return patchQuery(query, { filters });
}
