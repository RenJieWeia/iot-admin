import type { FieldMeta, Option, Tone } from '../api/types';

/** 实体与异常页共享的字段容器（两者都只有 fields）。 */
export interface FieldHolder {
  fields: FieldMeta[];
}

/** 可见列：hidden 字段不进表格，但可以进抽屉（§4 信息分层）。 */
export function visibleFields(holder: FieldHolder): FieldMeta[] {
  return holder.fields.filter((f) => f.visible);
}

/**
 * 抽屉可编辑字段：排除自增主键。判定依据是字段元数据本身——
 * 用户自填主键（如 terminals.id）声明为 filterable 的 id 字段，自增主键不是（见 fields.go）。
 * 该推断属于契约缺口，已在 Open-Q 里要求后端补显式 `editable` 标记。
 */
export function formFields(holder: FieldHolder): FieldMeta[] {
  return holder.fields.filter((field) => field.type !== 'id' || field.filterable);
}

/** 常驻高频筛选 ≤5，其余进「更多筛选」（table-query §5）。 */
export const QUICK_FILTER_LIMIT = 5;

export function quickFilters(holder: FieldHolder): FieldMeta[] {
  return holder.fields
    .filter((f) => f.filterable && !f.advancedOnly)
    .slice(0, QUICK_FILTER_LIMIT);
}

export function advancedFilters(holder: FieldHolder): FieldMeta[] {
  const quick = new Set(quickFilters(holder).map((f) => f.key));
  return holder.fields.filter((f) => f.filterable && (f.advancedOnly || !quick.has(f.key)));
}

export function fieldByKey(holder: FieldHolder, key: string): FieldMeta | undefined {
  return holder.fields.find((f) => f.key === key);
}

/** 服务端默认排序：有 id 用 id，否则用第一个可排序时间列（与 internal/admin/query.go 同口径）。 */
export function defaultSort(holder: FieldHolder): string {
  const id = holder.fields.find((f) => f.key === 'id' && f.sortable);
  if (id) return id.key;
  const datetime = holder.fields.find((f) => f.filterable && f.sortable && f.type === 'datetime');
  return datetime?.key ?? holder.fields.find((f) => f.sortable)?.key ?? '';
}

export function optionOf(field: FieldMeta, value: string): Option | undefined {
  return field.options?.find((o) => o.value === value);
}

export function toneOf(field: FieldMeta, value: string): Tone {
  return optionOf(field, value)?.tone ?? 'neutral';
}

export function labelOf(field: FieldMeta, value: string): string {
  return optionOf(field, value)?.label ?? value;
}

/** 关联字段的目标实体（field.ref.entity），用于取 options 与回填名称。 */
export function relationTarget(field: FieldMeta): string | undefined {
  return field.ref?.entity;
}
