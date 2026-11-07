/**
 * 当前内容区滚动容器的登记处。
 *
 * 外壳固定后（`shell` 是 height:100vh + overflow:hidden），window 不再滚动，
 * 滚动发生在 `<main>` 里。列表页的滚动位置记忆必须读这个容器的 scrollTop，
 * 读 window.scrollY 会永远是 0，表现为"记忆静默失效"。
 *
 * 用模块级引用而不是 context：AppShell 与页面之间隔着 Router 的 Outlet，
 * 走 context 需要在 Router 里多搭一层 Provider，而这里只需要一个可空引用。
 * 单元测试里没有 AppShell 时它会回落到 document.scrollingElement，行为与旧版一致。
 */
let pane: HTMLElement | null = null;

export function setScrollPane(el: HTMLElement | null): void {
  pane = el;
}

/** 返回当前滚动容器；未登记时回落到文档滚动元素（测试与无外壳场景）。 */
export function getScrollPane(): HTMLElement {
  return pane ?? (document.scrollingElement as HTMLElement) ?? document.documentElement;
}
