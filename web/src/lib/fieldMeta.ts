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
 * 抽屉可编辑字段：只看后端下发的 `editable`（admin.Entity.Cols 的可写列白名单）。
 * 前端不做任何推断；缺省 false 即不可编辑（fail-closed）。
 */
export function formFields(holder: FieldHolder): FieldMeta[] {
  return holder.fields.filter((field) => field.editable);
}

/** 常驻高频筛选 ≤5，其余进「更多筛选」（table-query §5）。 */
export const QUICK_FILTER_LIMIT = 5;

/**
 * 常驻筛选只收单行控件：多选与关联选择器（原生 multiple / 长选项列表）会把筛选条撑高、
 * 与同排输入框高度参差（REVIEW-005 R2）。这些 filterType 一律进「更多筛选」抽屉，
 * 已生效条件仍以标签外显（单删/全清不变）。advancedOnly 仍由后端字段决定，这里只加类型约束。
 */
const SINGLE_LINE_FILTER_TYPES: ReadonlySet<string> = new Set([
  'text',
  'boolean',
  'numberRange',
  'dateRange',
]);

export function quickFilters(holder: FieldHolder): FieldMeta[] {
  return holder.fields
    .filter((f) => f.filterable && !f.advancedOnly && SINGLE_LINE_FILTER_TYPES.has(f.filterType))
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

/**
 * 页面是否提供写入口：只读实体不写；此外**没有任何 editable 字段**的实体同样不可写
 * （异常两页全部 editable=false）。判定只看 `_meta`，不看「这是哪条路由」。
 */
export function isWritable(holder: FieldHolder & { readOnly?: boolean }): boolean {
  if (holder.readOnly) return false;
  return formFields(holder).length > 0;
}
