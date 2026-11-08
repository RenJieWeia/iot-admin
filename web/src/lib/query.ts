import type { FilterOp } from '../api/types';

export interface FilterCond {
  field: string;
  /** 省略时由服务端按字段声明的第一个操作符执行。 */
  op?: FilterOp;
  values: string[];
}

export interface QueryState {
  q: string;
  filters: FilterCond[];
  sort: string;
  order: 'asc' | 'desc';
  page: number;
  pageSize: number;
}

export function initQuery(
  defaultSort: string,
  defaultOrder: 'asc' | 'desc',
  pageSize: number,
): QueryState {
  return { q: '', filters: [], sort: defaultSort, order: defaultOrder, page: 1, pageSize };
}

/** 与服务端 QueryState 对齐：filter.<col>[.<op>]，in 用重复参数。 */
export function toParams(state: QueryState): URLSearchParams {
  const params = new URLSearchParams();
  if (state.q.trim()) params.set('q', state.q.trim());
  if (state.sort) params.set('sort', state.sort);
  if (state.order) params.set('order', state.order);
  params.set('page', String(state.page));
  params.set('pageSize', String(state.pageSize));
  for (const filter of state.filters) {
    if (!filter.values.length) continue;
    const key = filter.op ? `filter.${filter.field}.${filter.op}` : `filter.${filter.field}`;
    for (const value of filter.values) {
      if (value !== '') params.append(key, value);
    }
  }
  return params;
}

/** 快照只读页走游标分页：排序由服务端固定，只带 cursor/limit 与筛选白名单。 */
export function toCursorParams(state: QueryState, cursor: string | null): URLSearchParams {
  const params = toParams({ ...state, page: 1 });
  params.delete('page');
  params.delete('pageSize');
  params.delete('sort');
  params.delete('order');
  params.set('limit', String(state.pageSize));
  if (cursor) params.set('cursor', cursor);
  return params;
}

export function setFilter(state: QueryState, next: FilterCond): QueryState {
  const others = state.filters.filter((filter) => filter.field !== next.field);
  const filters = next.values.length ? [...others, next] : others;
  return { ...state, filters, page: 1 };
}

export function removeFilter(state: QueryState, field: string): QueryState {
  return { ...state, filters: state.filters.filter((filter) => filter.field !== field), page: 1 };
}
