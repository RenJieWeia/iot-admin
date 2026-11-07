/**
 * Radix Select 的空值哨兵（1107020）。
 *
 * Radix 的 `Select.Item` 不接受空字符串 value（`value=""` 会被当成非法值，控制台报错、
 * 选中态失效）。而我们三处下拉都有「空选项」——筛选的「全部」、枚举字段的「未设置」、
 * 快照步长的「选择步长」，它们在业务上就是 `''`（= 不加条件 / 未选值）。
 *
 * 统一用一个哨兵值承载，进出各转一次：界面看不见它，URL / 请求体 / state 里永远是 `''`。
 */
export const EMPTY_SELECT_VALUE = '__empty__';

/** 业务值 → Select 的 value（空 → 哨兵）。 */
export function toSelectValue(value: string | null | undefined): string {
  return value ? value : EMPTY_SELECT_VALUE;
}

/** Select 的 value → 业务值（哨兵 → 空）。 */
export function fromSelectValue(value: string): string {
  return value === EMPTY_SELECT_VALUE ? '' : value;
}
