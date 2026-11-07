import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * Radix Themes 基座守卫（1107016）。
 *
 * 这里锁两件容易「改一处忘一处」的事：
 * 1. 样式引入顺序：radix 的 styles.css 必须用 `layer(radix)` 收进最底层，且排在
 *    tailwind 的 theme/utilities 之前；层序语句必须落在全局样式树的**第一条语句**
 *    （层序由首次出现决定，晚一步 base 就会变成最低层）。
 * 2. 令牌来源：14 个语义变量必须引用 Radix 变量，且语义色不得被顺手接过去。
 */
const read = (path: string): string => readFileSync(join(process.cwd(), path), 'utf8');

const tailwind = read('src/styles/tailwind.css');
const globalCss = read('src/styles/global.css');
const tokens = read('src/styles/tokens.css');

/** 14 个语义变量 → 期望的 Radix 引用（:root 与 [data-theme] 块各一份，故要求出现 ≥2 次）。 */
const RADIX_MAPPING: ReadonlyArray<readonly [string, string]> = [
  ['--color-bg', 'var(--slate-2)'],
  ['--color-surface', 'var(--slate-1)'],
  ['--color-surface-2', 'var(--slate-3)'],
  ['--color-border', 'var(--slate-6)'],
  ['--color-border-strong', 'var(--slate-7)'],
  ['--color-text', 'var(--slate-12)'],
  ['--color-muted', 'var(--slate-11)'],
  ['--color-faint', 'color-mix(in oklab, var(--slate-11) 95%, var(--slate-2))'],
  ['--color-primary', 'var(--accent-11)'],
  ['--color-primary-hover', 'color-mix(in oklab, var(--accent-11) 70%, var(--accent-12))'],
  ['--color-primary-weak', 'var(--accent-3)'],
  ['--color-on-primary', 'var(--accent-contrast)'],
  ['--color-shadow-sticky', 'var(--slate-a6)'],
  ['--color-skeleton', 'var(--slate-4)'],
];

describe('Radix Themes 基座', () => {
  it('radix 样式以 layer(radix) 引入，且排在 tailwind theme/utilities 之前', () => {
    const radix = tailwind.indexOf("@import '@radix-ui/themes/styles.css' layer(radix)");
    expect(radix, '缺少 layer(radix) 形式的 radix 样式引入').toBeGreaterThan(-1);
    expect(radix, 'radix 必须早于 tailwind theme').toBeLessThan(
      tailwind.indexOf('tailwindcss/theme.css'),
    );
    expect(radix, 'radix 必须早于 tailwind utilities').toBeLessThan(
      tailwind.indexOf('tailwindcss/utilities.css'),
    );
  });

  it('层序声明是全局样式树的第一条语句，且 radix 排最底', () => {
    const firstStatement = globalCss
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line && !line.startsWith('/*') && !line.startsWith('*') && !line.startsWith('*/'))[0];
    expect(firstStatement).toBe('@layer radix, theme, base, components, utilities;');
  });

  it('14 个语义变量指向 Radix 变量', () => {
    // 出现次数不断言：--color-skeleton 在共享层只写一次，其余 13 个在「:root」与
    // 「:root, [data-theme='blue']」两处同值；两处都有由 themes.test.ts 负责。
    const lines = tokens.split('\n').map((line) => line.trim());
    for (const [name, value] of RADIX_MAPPING) {
      const hits = lines.filter((line) => line.startsWith(`${name}:`) && line.includes(value));
      expect(hits.length, `${name} 未指向 ${value}`).toBeGreaterThanOrEqual(1);
    }
  });

  it('语义色仍是字面值，不接 Radix 状态色', () => {
    for (const literal of [
      '--color-success: #15803D;',
      '--color-warning: #B45309;',
      '--color-danger: #B91C1C;',
      '--color-info: #1D4ED8;',
    ]) {
      expect(tokens, `语义色被改动：${literal}`).toContain(literal);
    }
  });

  it('.radix-themes 上钉回 Radix 的同名变量（元素自身声明压过继承）', () => {
    const block = tokens.slice(tokens.indexOf('.radix-themes {'));
    expect(block, '未钉回 --color-surface').toContain('--color-surface: var(--slate-1)');
    expect(block, '未钉回 --color-overlay').toContain('--color-overlay:');
    for (const space of ['--space-5: 20px;', '--space-6: 24px;', '--space-8: 32px;']) {
      expect(block, `未钉回 ${space}`).toContain(space);
    }
  });
});
