import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ensureLocalStorage } from './test/localStorage';

// 首帧防闪烁的回归守卫：主题必须在 createRoot().render() **之前**落到 <html data-theme>。
// 这条测的是「赋值与渲染的先后」，而不是最终结果——用 useEffect 也能得到最终结果，
// 但那会在第一帧闪一次默认色，正是本用例要拦住的回归。
const order: string[] = [];

vi.mock('react-dom/client', () => ({
  createRoot: () => ({
    render: () => {
      // 渲染被调用的那一刻，主题应当已经落好。
      order.push(`render:${document.documentElement.dataset.theme ?? '(none)'}`);
    },
  }),
}));

vi.mock('./App', () => ({ App: () => null }));

describe('main.tsx 的首帧取色时机', () => {
  beforeEach(() => {
    // jsdom 在 opaque origin 下不提供 localStorage，先装内存兜底。
    ensureLocalStorage();
    order.length = 0;
    document.documentElement.removeAttribute('data-theme');
    document.body.innerHTML = '<div id="root"></div>';
    window.localStorage.clear();
  });

  it('render 之前就把 localStorage 里的主题写到 data-theme 上', async () => {
    window.localStorage.setItem('iot-theme', 'graphite');
    order.push(`before-import:${document.documentElement.dataset.theme ?? '(none)'}`);
    vi.resetModules();
    await import('./main');
    expect(order).toEqual(['before-import:(none)', 'render:graphite']);
  });

  it('存储里是非法值时，渲染时已经是默认主题 blue', async () => {
    window.localStorage.setItem('iot-theme', 'neon');
    vi.resetModules();
    await import('./main');
    expect(order.at(-1)).toBe('render:blue');
  });

  it('存储为空时，渲染时是默认主题 blue', async () => {
    vi.resetModules();
    await import('./main');
    expect(order.at(-1)).toBe('render:blue');
  });
});
