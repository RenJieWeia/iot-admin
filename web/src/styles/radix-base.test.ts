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

  it('语义色 / 图表 / 媒体 / 顺序色阶也一律引用 Radix 色阶', () => {
    const lines = tokens.split('\n').map((line) => line.trim());
    const expected: ReadonlyArray<readonly [string, string]> = [
      ['--color-success', 'var(--green-9)'],
      ['--color-success-weak', 'var(--green-3)'],
      ['--color-warning', 'var(--amber-9)'],
      ['--color-warning-weak', 'var(--amber-3)'],
      ['--color-danger', 'var(--red-9)'],
      ['--color-danger-weak', 'var(--red-3)'],
      ['--color-info', 'var(--blue-9)'],
      ['--color-info-weak', 'var(--blue-3)'],
      ['--chart-1', 'var(--teal-9)'],
      ['--chart-2', 'var(--blue-9)'],
      ['--chart-3', 'var(--amber-9)'],
      ['--chart-4', 'var(--purple-9)'],
      ['--chart-5', 'var(--cyan-9)'],
      ['--chart-6', 'var(--lime-9)'],
      ['--chart-7', 'var(--crimson-9)'],
      ['--chart-8', 'var(--gray-9)'],
      ['--media-electric', 'var(--amber-9)'],
      ['--media-water', 'var(--cyan-9)'],
      ['--media-gas', 'var(--purple-9)'],
      ['--media-heat', 'var(--red-9)'],
      ...Array.from({ length: 10 }, (_, i) => [`--scale-${i + 1}`, `var(--teal-${i + 1})`] as const),
    ];
    for (const [name, value] of expected) {
      const hit = lines.find((line) => line.startsWith(`${name}:`) && line.includes(value));
      expect(hit, `${name} 未指向 ${value}`).toBeDefined();
    }
  });

  it('颜色类令牌里不再有字面色（--color-* / --chart-* / --media-* / --scale-*）', () => {
    // 只约束「颜色令牌」：--shadow-* 里的 rgba 是阴影的半透明度，不是颜色角色，
    // 也不匹配验收用的 `:  #hex|rgba(` 形态；要一并默认化得改 --black-aN（会从蓝黑变纯黑）。
    const COLOR_TOKENS = /^--(?:color|chart|media|scale)-[\w-]+\s*:/;
    const offenders = tokens
      .split('\n')
      .map((line, index) => ({ line: line.trim(), index: index + 1 }))
      .filter(({ line }) => COLOR_TOKENS.test(line))
      .filter(({ line }) => /#[0-9A-Fa-f]{3,8}\b|\brgba?\(|\bhsla?\(/.test(line));
    expect(offenders, `残留字面色：${offenders.map((o) => o.index).join(',')}`).toEqual([]);
  });

  it('.radix-themes 上钉回 Radix 的同名变量（元素自身声明压过继承）', () => {
    const block = tokens.slice(tokens.indexOf('.radix-themes {'));
    expect(block, '未钉回 --color-surface').toContain('--color-surface: var(--slate-1)');
    expect(block, '未钉回 --color-overlay').toContain('--color-overlay:');
    for (const space of ['--space-5: 20px;', '--space-6: 24px;', '--space-8: 32px;']) {
      expect(block, `未钉回 ${space}`).toContain(space);
    }
  });

  it('圆角令牌引用 Radix 刻度（control=radius-3 / card=radius-4 / pill 保留字面值）', () => {
    // --radius-N 定义在 [data-radius] 上，:root 取不到 → :root 带库默认值兜底，子树内不带。
    expect(tokens, ':root 未引用 --radius-3').toContain('--radius-control: var(--radius-3, 6px);');
    expect(tokens, ':root 未引用 --radius-4').toContain('--radius-card: var(--radius-4, 8px);');
    const block = tokens.slice(tokens.indexOf('.radix-themes {'));
    expect(block, '主题子树内未走 --radius-3').toContain('--radius-control: var(--radius-3);');
    expect(block, '主题子树内未走 --radius-4').toContain('--radius-card: var(--radius-4);');
    // Radix 没有通用的 --radius-full 变量，药丸圆角只能用字面值（注释里已说明）。
    expect(tokens, '--radius-pill 应保留字面值').toContain('--radius-pill: 999px;');
  });

  it('字号与行高引用 Radix 排版刻度（meta→1 / body→2 / section→3 / page→5）', () => {
    const pairs: ReadonlyArray<readonly [string, string]> = [
      ['--text-meta', 'var(--font-size-1, 12px)'],
      ['--lh-meta', 'var(--line-height-1, 16px)'],
      ['--text-body', 'var(--font-size-2, 14px)'],
      ['--lh-body', 'var(--line-height-2, 20px)'],
      ['--text-section', 'var(--font-size-3, 16px)'],
      ['--lh-section', 'var(--line-height-3, 24px)'],
      ['--text-page', 'var(--font-size-5, 20px)'],
      ['--lh-page', 'var(--line-height-5, 28px)'],
    ];
    const lines = tokens.split('\n').map((line) => line.trim());
    for (const [name, value] of pairs) {
      expect(lines.includes(`${name}: ${value};`), `${name} 未指向 ${value}`).toBe(true);
    }
    // 主题子树内不带兜底（跟随 <Theme scaling>）；--text-nav（11px）没有对应 Radix 档位，保持字面值。
    const block = tokens.slice(tokens.indexOf('.radix-themes {'));
    for (const decl of ['--text-body: var(--font-size-2);', '--lh-body: var(--line-height-2);']) {
      expect(block, `主题子树内未走 ${decl}`).toContain(decl);
    }
    expect(tokens, '--text-nav 应保持字面值').toContain('--text-nav: 11px;');
  });
});
