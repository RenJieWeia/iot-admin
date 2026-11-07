import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
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
