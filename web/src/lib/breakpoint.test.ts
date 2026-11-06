import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { NARROW_QUERY } from './breakpoint';

/**
 * 窄屏断点单一来源的守护。
 *
 * `NARROW_QUERY` 被 `AppShell`（侧栏抽屉化）与 `FilterBar`（筛选抽屉化）共用；
 * 它必须与 `styles/tailwind.css` 里 `@media (max-width: 1023.98px)` 保持同一数值，
 * 否则会出现「CSS 已切单列、JS 仍认为桌面」的错位（历史缺陷 60bc623）。
 */

describe('窄屏断点常量（lib/breakpoint.ts）', () => {
  it('数值锁定为 1023.98px（Tailwind max-lg 口径，不含 1024）', () => {
    expect(NARROW_QUERY).toBe('(max-width: 1023.98px)');
  });

  it('与 tailwind.css 里的媒体查询数值一致', () => {
    const css = readFileSync(join(process.cwd(), 'src/styles/tailwind.css'), 'utf8');
    expect(css).toContain('@media (max-width: 1023.98px)');
    const px = NARROW_QUERY.match(/max-width:\s*([0-9.]+px)/)?.[1];
    expect(px).toBe('1023.98px');
  });
});
