import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Drawer } from './Drawer';

/**
 * Drawer 动效契约（1107022）。
 *
 * jsdom 不跑媒体查询、也不算 animation 的最终样式，所以这里分两层断言：
 * - 运行时：`data-state` 与「关闭后是否真的卸载」（Radix Presence 的行为）；
 * - 样式文本：动画存在性、只动 opacity/transform、reduced-motion 的归零声明。
 * 浏览器里的 computed animationName/Duration 由交付说明里的实测数据补足。
 */
const css = readFileSync(join(process.cwd(), 'src/styles/global.css'), 'utf8');

function drawer(open: boolean) {
  return (
    <Drawer open={open} title="筛选" onClose={() => {}} description="修改即生效">
      内容
    </Drawer>
  );
}

describe('Drawer 的动效契约（1107022）', () => {
  it('打开时面板与遮罩都是 data-state="open"', () => {
    render(drawer(true));
    const panel = screen.getByRole('dialog');
    expect(panel).toHaveAttribute('data-state', 'open');
    expect(panel.className).toContain('drawer-panel');
    const overlay = document.querySelector('.drawer-overlay');
    expect(overlay).not.toBeNull();
    expect(overlay).toHaveAttribute('data-state', 'open');
  });

  it('关闭后元素从 DOM 卸下（Radix Presence 等动画播完再卸载，不是 CSS 隐藏）', async () => {
    const { rerender } = render(drawer(true));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    rerender(drawer(false));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(document.querySelector('.drawer-overlay')).toBeNull();
  });

  it('关闭后先进入 data-state="closed" 窗口，animationend 之后才卸载（不是立刻消失）', async () => {
    // jsdom 不解析 @keyframes、getComputedStyle().animationName 恒为 none，
    // 而 Radix Presence 正是靠它决定「等动画播完还是立刻卸载」。
    // 这里只对这一条用例把 animationName/Duration 打桩成真实浏览器里的值（Chrome 实测 200ms），
    // 断言的目标不变：关闭后节点仍在、带 data-state=closed，直到 animationend 才卸载。
    const real = window.getComputedStyle.bind(window);
    const spy = vi.spyOn(window, 'getComputedStyle').mockImplementation((el: Element) => {
      const style = real(el);
      if (el instanceof Element && el.classList.contains('drawer-panel')) {
        return new Proxy(style, {
          get(target, prop) {
            // 真实浏览器里 getComputedStyle 返回的是**活对象**：data-state 一变，animationName 跟着变。
            // jsdom 的快照不会更新，所以这里按当前 data-state 回放同名值，模拟「活对象」。
            if (prop === 'animationName') {
              return (el as HTMLElement).getAttribute('data-state') === 'closed'
                ? 'drawer-slide-out'
                : 'drawer-slide-in';
            }
            if (prop === 'animationDuration') return '0.2s';
            if (prop === 'animationDelay') return '0s';
            const value = Reflect.get(target, prop);
            return typeof value === 'function' ? value.bind(target) : value;
          },
        }) as CSSStyleDeclaration;
      }
      return style;
    });
    try {
      const { rerender } = render(drawer(true));
      expect(screen.getByRole('dialog')).toHaveAttribute('data-state', 'open');

      rerender(drawer(false));
      const closing = document.querySelector('.drawer-panel');
      expect(closing, '关闭瞬间就卸载了 → 退场动画没有机会播').not.toBeNull();
      expect(closing).toHaveAttribute('data-state', 'closed');

      // Radix 会核对 event.animationName 与当前动画名一致才认这次结束（jsdom 没有 AnimationEvent，
      // 用普通 Event 补上 animationName）。
      const end = new Event('animationend', { bubbles: true });
      Object.defineProperty(end, 'animationName', { value: 'drawer-slide-out' });
      closing?.dispatchEvent(end);
      await waitFor(() => expect(document.querySelector('.drawer-panel')).toBeNull());
    } finally {
      spy.mockRestore();
    }
  });

  it('点关闭按钮只触发一次 onClose（开关语义不变）', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(
      <Drawer open title="筛选" onClose={onClose}>
        内容
      </Drawer>,
    );
    await user.click(screen.getByRole('button', { name: '关闭' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('CSS 契约：open / closed 两态都有动画，时长走 --dur-base 与 --ease-out', () => {
    expect(css).toMatch(
      /\.drawer-panel\[data-state='open'\]\s*\{[^}]*animation:\s*drawer-slide-in var\(--dur-base\) var\(--ease-out\) both;/,
    );
    expect(css).toMatch(
      /\.drawer-panel\[data-state='closed'\]\s*\{[^}]*animation:\s*drawer-slide-out var\(--dur-base\) var\(--ease-out\) both;/,
    );
    expect(css).toMatch(/\.drawer-overlay\[data-state='open'\]\s*\{[^}]*drawer-overlay-in/);
    expect(css).toMatch(/\.drawer-overlay\[data-state='closed'\]\s*\{[^}]*drawer-overlay-out/);
  });

  it('动画只动 transform / opacity（Keyframes 里不出现宽高与定位）', () => {
    for (const name of ['drawer-slide-in', 'drawer-slide-out', 'drawer-overlay-in', 'drawer-overlay-out']) {
      const start = css.indexOf(`@keyframes ${name}`);
      expect(start, `缺少 @keyframes ${name}`).toBeGreaterThan(-1);
      const block = css.slice(start, start + 400);
      expect(block, `${name} 动了布局属性`).not.toMatch(/\b(width|height|top|left|right|margin)\s*:/);
      expect(block).toMatch(/opacity:\s*\d/);
    }
  });

  it('hover 过渡：只列颜色类属性，时长取 --dur-fast（120ms）', () => {
    expect(css).toMatch(/transition-property:\s*background-color, color, border-color, box-shadow;/);
    expect(css).toMatch(/transition-duration:\s*var\(--dur-fast\);/);
    // 不得把几何属性写进过渡列表。
    expect(css).not.toMatch(/transition-property:[^;]*\b(width|height|top|left|margin)\b/);
  });

  it('reduced-motion 下动画与过渡都置为 none，且覆盖抽屉与 hover 三类元素', () => {
    const at = css.indexOf('@media (prefers-reduced-motion: reduce)');
    expect(at, '缺少 prefers-reduced-motion 媒体查询').toBeGreaterThan(-1);
    const block = css.slice(at, css.indexOf('\n}', at) + 2);
    for (const selector of ['.drawer-panel', '.drawer-overlay', '.rt-Button', 'tbody tr']) {
      expect(block, `未覆盖 ${selector}`).toContain(selector);
    }
    expect(block).toMatch(/animation:\s*none;/);
    expect(block).toMatch(/transition:\s*none;/);
  });
});
