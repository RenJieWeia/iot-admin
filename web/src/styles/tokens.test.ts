import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const CSS_ROOT = join(process.cwd(), 'src');

function cssFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) return cssFiles(full);
    return entry.endsWith('.css') ? [full] : [];
  });
}

const files = cssFiles(CSS_ROOT);
const tokensFile = join(CSS_ROOT, 'styles/tokens.css');

/** 颜色字面量：hex / rgb() / hsl() / 具名颜色（用前后边界避免误伤 white-space 这类属性名）。 */
const COLOR_LITERAL =
  /#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|(?<![\w-])(?:red|blue|green|black|white|gray|grey|orange)(?![\w-])/;

describe('CSS 令牌约束', () => {
  it('颜色字面量只出现在 tokens.css 的 :root', () => {
    expect(files).toContain(tokensFile);
    for (const file of files) {
      if (file === tokensFile) continue;
      readFileSync(file, 'utf8')
        .split('\n')
        .forEach((line, index) => {
          expect(COLOR_LITERAL.test(line), `${file}:${index + 1} 出现颜色字面量: ${line}`).toBe(
            false,
          );
        });
    }
  });

  it('tokens.css 的 :root 声明颜色、间距、行高与圆角令牌', () => {
    const css = readFileSync(tokensFile, 'utf8');
    for (const token of [
      '--color-text',
      '--color-primary',
      '--color-border',
      '--space-1',
      '--space-8',
      '--font-md',
      '--line-md',
      '--radius-md',
      '--row-height',
    ]) {
      expect(css).toContain(token);
    }
  });

  it('间距满足 4px 网格：padding/margin/gap 的 px 值都是 4 的倍数', () => {
    const spacing = /(?:padding|margin|gap)[a-z-]*\s*:\s*([^;]+);/g;
    for (const file of files) {
      const css = readFileSync(file, 'utf8');
      for (const match of css.matchAll(spacing)) {
        const value = match[1] ?? '';
        for (const px of value.matchAll(/(-?\d+)px/g)) {
          const n = Number(px[1]);
          // 允许 0 以及 1px/2px 的边框与视觉隐藏用的 -1px；其余必须是 4 的倍数。
          if (Math.abs(n) <= 2) continue;
          expect(n % 4, `${file} 间距 ${value.trim()} 不在 4px 网格上`).toBe(0);
        }
      }
    }
  });

  it('不使用 !important', () => {
    for (const file of files) {
      expect(readFileSync(file, 'utf8'), `${file} 使用了 !important`).not.toContain('!important');
    }
  });
});
