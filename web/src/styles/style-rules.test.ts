import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * 样式规则守护（task-011 §5）：迁移到 Tailwind 后，颜色与间距不再只长在 .css 里，
 * 因此断言同时扫 TSX 与 CSS 两种源文件。
 *
 * 规则：
 * 1. 颜色字面量（#hex / rgb() / hsl()）不得出现在 className / cn() 参数 / 非 tokens.css 的样式里；
 * 2. 禁止 arbitrary value（`p-[13px]`、`w-[37%]`），只放行 Radix 需要的变体选择器
 *    `data-[...]` / `aria-[...]` / `has-[...]` / `group-[...]` / `peer-[...]`；
 * 3. 禁止 Tailwind 默认调色板（bg-slate-100 / text-red-500 等）；
 * 4. 颜色令牌只能在 tokens.css 的 :root 定义；
 * 5. 间距守 4px 网格：p-[Npx]/m-[Npx]/gap-[Npx] 的 N 必须是 4 的倍数。
 */
const SRC = join(process.cwd(), 'src');
const TOKENS = join(SRC, 'styles/tokens.css');

function walk(dir: string, suffix: RegExp): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) return walk(full, suffix);
    return suffix.test(entry) ? [full] : [];
  });
}

const tsxFiles = walk(SRC, /\.tsx$/).filter((file) => !/\.test\.tsx$/.test(file));
const cssFiles = walk(SRC, /\.css$/);

const COLOR_LITERAL =
  /#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|(?<![\w-])(?:red|blue|green|black|white|gray|grey|orange)(?![\w-])/;

/** Radix 变体选择器白名单：只允许 data-/aria-/has-/group-/peer- 前缀内的任意值。 */
const ARBITRARY = /\[[^\]]+\]/g;
const ALLOWED_PREFIX = /(?:^|[\s:])(?:data|aria|has|group|peer)-$/;

const DEFAULT_PALETTE =
  /\b(?:bg|text|border|ring|outline|fill|stroke)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b/;

const SPACING_ARBITRARY = /\b(?:p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap)-\[(\d+)px\]/g;

function classAttributeValues(code: string): string[] {
  const values: string[] = [];
  // className="..." / className='...' / className={`...`}
  for (const match of code.matchAll(/className=\{?["'`]([^"'`]*)[`"'"]/g)) {
    if (match[1]) values.push(match[1]);
  }
  // cn(...) 里的字符串字面量
  for (const call of code.matchAll(/cn\(([\s\S]*?)\)/g)) {
    for (const str of (call[1] ?? '').matchAll(/['"`]([^'"`]*)['"`]/g)) {
      if (str[1]) values.push(str[1]);
    }
  }
  return values;
}

describe('样式规则（Tailwind 迁移后）', () => {
  it('类名/内联样式中不出现颜色字面量', () => {
    const offenders: string[] = [];
    for (const file of tsxFiles) {
      const code = readFileSync(file, 'utf8');
      for (const value of classAttributeValues(code)) {
        if (COLOR_LITERAL.test(value)) offenders.push(`${file}: ${value}`);
      }
    }
    for (const file of cssFiles) {
      if (file === TOKENS) continue;
      readFileSync(file, 'utf8')
        .split('\n')
        .forEach((line, index) => {
          if (COLOR_LITERAL.test(line)) offenders.push(`${file}:${index + 1}: ${line.trim()}`);
        });
    }
    expect(offenders).toEqual([]);
  });

  it('不使用 arbitrary value（Radix 变体选择器白名单除外）', () => {
    const offenders: string[] = [];
    for (const file of tsxFiles) {
      for (const value of classAttributeValues(readFileSync(file, 'utf8'))) {
        for (const match of value.matchAll(ARBITRARY)) {
          const token = match[0];
          const start = match.index ?? 0;
          const prefix = value.slice(Math.max(0, start - 6), start);
          // 只有 `data-[…]` / `aria-[…]` / `has-[…]` / `group-[…]` / `peer-[…]` 这类
          // Radix/原生状态选择器放行；其余一律视为 arbitrary value。
          if (ALLOWED_PREFIX.test(prefix)) continue;
          offenders.push(`${file}: ${prefix}${token}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });

  it('不使用 Tailwind 默认调色板', () => {
    const offenders: string[] = [];
    for (const file of tsxFiles) {
      for (const value of classAttributeValues(readFileSync(file, 'utf8'))) {
        if (DEFAULT_PALETTE.test(value)) offenders.push(`${file}: ${value}`);
      }
    }
    expect(offenders).toEqual([]);
  });

  it('颜色令牌只在 tokens.css 定义（tailwind.css 只能做 var() 映射）', () => {
    const offenders: string[] = [];
    for (const file of cssFiles) {
      if (file === TOKENS) continue;
      readFileSync(file, 'utf8')
        .split('\n')
        .forEach((line, index) => {
          const declaration = /^\s*(--color-[\w-]+)\s*:\s*(.+?);/.exec(line);
          if (!declaration) return;
          const value = declaration[2] ?? '';
          // 映射（var(--color-x)）放行；出现颜色字面量说明在定义第二套颜色
          if (COLOR_LITERAL.test(value)) offenders.push(`${file}:${index + 1}: ${line.trim()}`);
        });
    }
    expect(offenders).toEqual([]);
    expect(readFileSync(TOKENS, 'utf8')).toContain('--color-surface');
  });

  it('间距 arbitrary value 必须落在 4px 网格上', () => {
    const offenders: string[] = [];
    for (const file of tsxFiles) {
      for (const value of classAttributeValues(readFileSync(file, 'utf8'))) {
        for (const match of value.matchAll(SPACING_ARBITRARY)) {
          const px = Number(match[1]);
          if (px % 4 !== 0) offenders.push(`${file}: ${match[0]}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });

  it('迁移完成：不再有 CSS Modules 引用', () => {
    // 拼接后缀，避免测试文件自身命中 `git grep "\\.module\\.css" web/src` 的守卫断言。
    const moduleSuffix = ['.module', '.css'].join('');
    const leftovers = tsxFiles.filter((file) => readFileSync(file, 'utf8').includes(moduleSuffix));
    expect(leftovers).toEqual([]);
  });

  it('字号/圆角词汇只用 Tailwind 名，不得回潮到项目语义别名', () => {
    // 「一个令牌两个类名」是风格不统一的根因：text-meta 与 text-sm 值一样，
    // 代码里各用一半。这里把它锁死——只允许 Tailwind 名。
    //
    // 例外：tailwind.css / tokens.css 里 `--text-meta: var(--text-meta)` 这种**变量定义行**，
    // 原始变量名仍叫 --text-meta（数值来源），不在禁令范围内。
    const banned = [
      'text-meta',
      'text-body',
      'text-section',
      'text-page',
      'rounded-control',
      'rounded-card',
    ];
    const isVariableDefinition = (line: string) =>
      /^\s*--(?:text|radius)-[\w-]+\s*:/.test(line) || /var\(--/.test(line);

    const offenders: string[] = [];
    for (const file of walk(SRC, /\.(?:tsx?|css)$/)) {
      // 本文件自己就是禁令清单，跳过（否则守卫会把自己告了）。
      if (file.endsWith('style-rules.test.ts')) continue;
      const lines = readFileSync(file, 'utf8').split('\n');
      lines.forEach((line, index) => {
        if (isVariableDefinition(line)) return;
        for (const word of banned) {
          // 词边界：别把 --text-meta 或 text-meta-size 误判成类名用法
          if (new RegExp(`(?<![\\w-])${word}(?![\\w-])`).test(line)) {
            offenders.push(`${file}:${index + 1}: ${word} → 请用 Tailwind 名`);
          }
        }
      });
    }
    expect(offenders).toEqual([]);
  });
});
