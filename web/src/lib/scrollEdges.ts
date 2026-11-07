/**
 * 横向滚动的两侧状态：是否已经向右滚过、是否还没滚到最右。
 *
 * 抽成纯函数是为了可测：jsdom 里 scrollWidth/clientWidth 恒为 0，直接测渲染拿不到真值。
 * 1px 容差吸收亚像素与缩放带来的取整误差（否则"刚好滚到底"会永远是 false）。
 */
export const SCROLL_EDGE_TOLERANCE = 1;

export function scrollEdges(element: {
  scrollLeft: number;
  scrollWidth: number;
  clientWidth: number;
}): { left: boolean; right: boolean } {
  const { scrollLeft, scrollWidth, clientWidth } = element;
  // 内容没溢出：两侧都不显示投影（否则宽屏下像一条脏边）。
  if (scrollWidth - clientWidth <= SCROLL_EDGE_TOLERANCE) {
    return { left: false, right: false };
  }
  return {
    left: scrollLeft > SCROLL_EDGE_TOLERANCE,
    right: scrollLeft < scrollWidth - clientWidth - SCROLL_EDGE_TOLERANCE,
  };
}
