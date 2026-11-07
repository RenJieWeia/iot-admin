import { afterEach, describe, expect, it } from 'vitest';
import { getScrollPane, setScrollPane } from './scrollPane';

/**
 * 外壳固定后滚动发生在内容区容器上，window 不再滚动。
 * 这里钉住「登记 → 取用」这条链路：没有登记时回落到文档滚动元素，
 * 免得测试环境或未挂载外壳的场景读不到 scrollTop。
 */
describe('内容区滚动容器登记（lib/scrollPane.ts）', () => {
  afterEach(() => setScrollPane(null));

  it('登记后 getScrollPane 返回同一个节点', () => {
    const el = document.createElement('main');
    setScrollPane(el);
    expect(getScrollPane()).toBe(el);
  });

  it('未登记时回落到文档滚动元素（不返回 null，调用方不用判空）', () => {
    setScrollPane(null);
    const pane = getScrollPane();
    expect(pane).toBeTruthy();
    expect(pane === document.scrollingElement || pane === document.documentElement).toBe(true);
  });

  it('容器上的 scrollTop 可读写（滚动记忆依赖它）', () => {
    const el = document.createElement('main');
    Object.defineProperty(el, 'scrollTop', { value: 0, writable: true, configurable: true });
    setScrollPane(el);
    getScrollPane().scrollTop = 420;
    expect(el.scrollTop).toBe(420);
  });
});
