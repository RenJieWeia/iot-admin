import type { FieldMeta, FilterOp, PageMeta } from '../api/types';
import { initQuery } from './query';
import type { FilterCond, QueryState } from './query';

/** 列表查询与地址栏的互转：q / filter.<字段>[.<op>] / sort / order / page / pageSize。 */

function parseFilterKey(key: string): { field: string; op?: string } | null {
  if (!key.startsWith('filter.')) return null;
  const rest = key.slice('filter.'.length);
  if (!rest) return null;
  const dot = rest.indexOf('.');
  if (dot < 0) return { field: rest };
  return { field: rest.slice(0, dot), op: rest.slice(dot + 1) || undefined };
}

function allowed(field: FieldMeta, op: string | undefined): boolean {
  if (!field.filterable || field.filterType === 'none') return false;
  if (!op) return true;
  return field.operators.includes(op as never);
}

export function queryFromSearch(
  search: string,
  page: PageMeta | undefined,
  defaultPageSize: number,
  cursor: boolean,
): QueryState {
  const base = initQuery(page?.defaultSort ?? '', page?.defaultOrder ?? 'desc', defaultPageSize);
  const params = new URLSearchParams(search);
  const fields = new Map((page?.fields ?? []).map((field) => [field.key, field]));

  const grouped = new Map<string, { field: string; op?: string; values: string[] }>();
  for (const [key, value] of params.entries()) {
    const parsed = parseFilterKey(key);
    if (!parsed) continue;
    const field = fields.get(parsed.field);
    if (!field || !allowed(field, parsed.op) || value === '') continue;
    const id = `${parsed.field}|${parsed.op ?? ''}`;
    const hit = grouped.get(id) ?? { field: parsed.field, op: parsed.op, values: [] };
    hit.values.push(value);
    grouped.set(id, hit);
  }

  const filters: FilterCond[] = [...grouped.values()].map((group) => ({
    field: group.field,
    op: group.op as FilterOp | undefined,
    values: group.values,
  }));

  const q = params.get('q') ?? '';
  if (cursor) {
    return { ...base, q, filters };
  }

  const sortField = params.get('sort');
  const sort = sortField && fields.get(sortField)?.sortable ? sortField : base.sort;
  const orderParam = params.get('order');
  const order = orderParam === 'asc' || orderParam === 'desc' ? orderParam : base.order;

  const pageParam = Number.parseInt(params.get('page') ?? '', 10);
  const pageNo = Number.isFinite(pageParam) && pageParam > 0 ? pageParam : 1;

  const sizeParam = Number.parseInt(params.get('pageSize') ?? '', 10);
  const pageSize = Number.isFinite(sizeParam) && sizeParam > 0 ? sizeParam : defaultPageSize;

  return { ...base, q, filters, sort, order, page: pageNo, pageSize };
}

export function searchFromQuery(query: QueryState, cursor: boolean): string {
  const params = new URLSearchParams();
  if (query.q) params.set('q', query.q);
  for (const cond of query.filters) {
    if (!cond.values.length) continue;
    const key = `filter.${cond.field}${cond.op ? `.${cond.op}` : ''}`;
    for (const value of cond.values) params.append(key, value);
  }
  if (!cursor) {
    if (query.sort) params.set('sort', query.sort);
    if (query.order) params.set('order', query.order);
    if (query.page > 1) params.set('page', String(query.page));
    if (query.pageSize) params.set('pageSize', String(query.pageSize));
  }
  return params.toString();
}

/** 快速跳转用：把一组筛选条件编码成查询串（与列表 URL 同一格式）。 */
export function filterSearch(conds: { field: string; op?: string; values: string[] }[]): string {
  const params = new URLSearchParams();
  for (const cond of conds) {
    const key = `filter.${cond.field}${cond.op ? `.${cond.op}` : ''}`;
    for (const value of cond.values) params.append(key, value);
  }
  return params.toString();
}
