/**
 * 窄屏断点的唯一来源。
 *
 * 口径 = Tailwind `max-lg`（`< 1024px`，**不含 1024**）：1024 起算桌面（侧栏常驻），
 * 1023 及以下走窄屏（侧栏收进抽屉、筛选收进抽屉）。
 *
 * 与 `styles/tailwind.css` 里 `@media (max-width: 1023.98px)` 必须保持同一数值——
 * 差 1px 就会出现「CSS 已切单列、JS 仍认为桌面」的错位（历史缺陷，见 60bc623）。
 */
export const NARROW_QUERY = '(max-width: 1023.98px)';
