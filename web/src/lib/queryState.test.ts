import { describe, expect, it } from 'vitest';
import type { FilterOp } from '../api/types';
import {
  clearFilters,
  parseQueryState,
  patchQuery,
  removeFilterAt,
  setSort,
  toListQuery,
  toSearchParams,
  type ActiveFilter,
} from './queryState';

describe('QueryState ↔ URL 双向同步', () => {
  it('解析 q/sort/order/page/pageSize 与 filter.<col>.<op> 编码', () => {
    const params = new URLSearchParams(
      'q=%E6%B3%B5&sort=name&order=asc&page=2&pageSize=20&filter.enabled.eq=true&filter.type_id.in=1&filter.type_id.in=2',
    );
    expect(parseQueryState(params)).toEqual({
      q: '泵',
      sort: 'name',
      order: 'asc',
      page: 2,
      pageSize: 20,
      filters: [
        { field: 'enabled', op: 'eq', values: ['true'] },
        { field: 'type_id', op: 'in', values: ['1', '2'] },
      ],
    });
  });

  it('无操作符的 filter.<col> 解析为空操作符，交给服务端选默认操作符', () => {
    expect(parseQueryState(new URLSearchParams('filter.name=%E6%B3%B5')).filters).toEqual([
      { field: 'name', op: '', values: ['泵'] },
    ]);
  });

  it('序列化 → 解析往返一致（多值用重复参数，不合并成逗号）', () => {
    const state = {
      q: '泵',
      sort: 'name',
      order: 'asc' as const,
      page: 3,
      pageSize: 50 as const,
      filters: [
        { field: 'type_id', op: 'in' as FilterOp, values: ['1', '2'] },
        { field: 'gateway_id', op: 'contains' as FilterOp, values: ['gw'] },
      ],
    };
    const roundTripped = parseQueryState(toSearchParams(state));
    expect(roundTripped).toEqual(state);
  });

  it('非法 page/pageSize 归一：page<1 → 1，pageSize 非 10/20/50 → 10', () => {
    const parsed = parseQueryState(new URLSearchParams('page=0&pageSize=999'));
    expect(parsed.page).toBe(1);
    expect(parsed.pageSize).toBe(10);
  });

  it('toListQuery 保留显式操作符与多值', () => {
    const query = toListQuery({
      q: '',
      sort: 'name',
      order: 'desc',
      page: 1,
      pageSize: 10,
      filters: [{ field: 'type_id', op: 'in', values: ['1', '2'] }],
    });
    expect(query.filters).toEqual([{ field: 'type_id', op: 'in', values: ['1', '2'] }]);
    expect(query.q).toBeUndefined();
    expect(query.sort).toBe('name');
  });

  it('筛选/关键词/排序/pageSize 变化回第一页，单纯翻页不回第一页', () => {
    const base = parseQueryState(new URLSearchParams('page=3&pageSize=20'));
    expect(patchQuery(base, { q: '泵' }).page).toBe(1);
    expect(
      patchQuery(base, { filters: [{ field: 'name', op: 'contains', values: ['泵'] }] }).page,
    ).toBe(1);
    expect(patchQuery(base, { sort: 'name' }).page).toBe(1);
    expect(patchQuery(base, { pageSize: 50 }).page).toBe(1);
    expect(patchQuery(base, { page: 4 }).page).toBe(4);
  });

  it('同列排序切换方向，换列从 desc 开始，排序变化回第一页', () => {
    const state = parseQueryState(new URLSearchParams('sort=name&order=desc&page=2'));
    // 同列再点一次：降序 → 升序。
    expect(setSort(state, 'name')).toMatchObject({ sort: 'name', order: 'asc', page: 1 });
    expect(setSort(parseQueryState(new URLSearchParams('sort=name&order=asc')), 'name')).toMatchObject(
      { sort: 'name', order: 'desc' },
    );
    // 换列：从 desc 开始。
    expect(setSort(state, 'id')).toMatchObject({ sort: 'id', order: 'desc', page: 1 });
  });

  it('单删/全清筛选只影响目标条件', () => {
    const state = parseQueryState(
      new URLSearchParams('q=x&filter.enabled.eq=true&filter.name.contains=%E6%B3%B5&page=2'),
    );
    const afterRemove = removeFilterAt(state, 'enabled', 'eq');
    expect(afterRemove.filters.map((f: ActiveFilter) => f.field)).toEqual(['name']);
    expect(afterRemove.q).toBe('x');
    expect(afterRemove.page).toBe(1);

    const cleared = clearFilters(state);
    expect(cleared.filters).toEqual([]);
    expect(cleared.q).toBe('');
  });
});
