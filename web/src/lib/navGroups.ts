/**
 * 导航分组：侧栏与面包屑的**唯一来源**。
 *
 * 之前「实体 / 数据 / 异常」三个词写死在 SidebarNav 里；面包屑如果自己再写一遍，
 * 就成了第二份定义——改一处忘一处，走查时必然对不上。
 */

export type NavGroupKey = 'entities' | 'data' | 'exceptions';

export interface NavGroup {
  key: NavGroupKey;
  /** 中文名：侧栏分组标题与面包屑第一级共用这一份。 */
  label: string;
}

export const NAV_GROUPS: NavGroup[] = [
  { key: 'entities', label: '实体' },
  { key: 'data', label: '数据' },
  { key: 'exceptions', label: '异常' },
];

/**
 * key → 中文名。**从 NAV_GROUPS 派生**，不再手写第二份：
 * 同一事实只留一个定义，否则改一处忘一处。
 */
export const NAV_GROUP_LABEL: Record<NavGroupKey, string> = Object.fromEntries(
  NAV_GROUPS.map((group) => [group.key, group.label]),
) as Record<NavGroupKey, string>;

/** 面包屑的一节：有 href 可点，无 href 是纯文本（分组没有落地页）。 */
export interface BreadcrumbItem {
  label: string;
  href?: string;
}

/** 一级页的通用面包屑：只显示分组名，页面名交给 h1，不与标题重复。 */
export function groupCrumb(group: NavGroupKey): BreadcrumbItem[] {
  return [{ label: NAV_GROUP_LABEL[group] }];
}
