/**
 * 列表四态与「页码越界纠偏」的唯一判定处。
 *
 * 实体列表页与终端类型详情页共用同一套口径，避免两页各写一份、慢慢长歪：
 * - `empty`   ：确实没有数据（total === 0）→ 给主操作入口
 * - `nomatch` ：有用户可改的条件，且排除了全部结果 → 给「清除筛选」
 * - `stale`   ：没有可改条件，但 total > 0 且当前页无行 → 页内数据在变，给「刷新」
 * - `error`   ：无数据可展示的失败（已有数据时的刷新失败另由页面自己降级）
 * - `loading` ：首次加载
 * - `ready`   ：有行
 */
export type ListState = 'error' | 'loading' | 'ready' | 'empty' | 'nomatch' | 'stale';

export interface ListStateInput {
  /** 查询失败 */ isError: boolean;
  /** 已有数据（刷新失败时依然保留） */ hasData: boolean;
  /** 首次加载中 */ isPending: boolean;
  rowCount: number;
  total: number;
  /** 是否存在用户可改的查询条件（关键词/筛选条数） */ hasConditions: boolean;
}

export function computeListState(i: ListStateInput): ListState {
  if (i.isError && !i.hasData) return 'error';
  if (i.isPending) return 'loading';
  if (i.rowCount > 0) return 'ready';
  if (i.hasConditions) return 'nomatch';
  return i.total > 0 ? 'stale' : 'empty';
}

export interface ResetPageInput {
  page: number;
  rowCount: number;
  total: number;
  isPending: boolean;
  isError: boolean;
  /** 本次访问是否已经纠偏过（防止来回跳） */ alreadyCorrected: boolean;
}

/**
 * 页码越界（当前页无行、但总数 > 0、且不在第一页）时是否需要 replace 回第一页。
 * 调用方负责用 ref 记住 `alreadyCorrected`，保证只纠偏一次。
 */
export function shouldResetPage(i: ResetPageInput): boolean {
  if (i.isError || i.isPending) return false;
  if (i.page <= 1 || i.alreadyCorrected) return false;
  return i.rowCount === 0 && i.total > 0;
}

/** 纠偏提示的统一文案（中性提示，不是成功事件）。 */
export const RESET_PAGE_NOTICE = '当前页无数据，已回到第一页（筛选条件保留）。';
