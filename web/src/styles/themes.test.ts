import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * 令牌完整性守护：三套主题只允许覆盖那 14 个变量；
 * 语义色（成功/警告/危险/信息）、图表色、媒体色、顺序色阶必须留在共享层，
 * 否则「红=危险」「琥珀=电」会随皮肤漂移。
 */
const THEME_VARS = [
  '--color-bg',
  '--color-surface',
  '--color-surface-2',
  '--color-border-subtle',
  '--color-border',
  '--color-border-strong',
  '--color-text',
  '--color-muted',
  '--color-faint',
  '--color-primary',
  '--color-primary-hover',
  '--color-primary-weak',
  '--color-on-primary',
  '--color-shadow-sticky',
] as const;

const SHARED_ONLY = [
  '--color-success',
  '--color-success-weak',
  '--color-warning',
  '--color-warning-weak',
  '--color-danger',
  '--color-danger-weak',
  '--color-info',
  '--color-info-weak',
  '--chart-1',
  '--chart-8',
  '--media-electric',
  '--media-water',
  '--media-gas',
  '--media-heat',
  '--scale-1',
  '--scale-10',
] as const;

const css = readFileSync(join(process.cwd(), 'src/styles/tokens.css'), 'utf8');

/** 取某个选择器块的内容（不支持嵌套块；本文件的结构是平的）。 */
function blockAfter(selector: string): string {
  const start = css.indexOf(selector);
  if (start === -1) return '';
  const open = css.indexOf('{', start);
  const close = css.indexOf('}', open);
  return css.slice(open + 1, close);
}

const themeBlocks: Record<string, string> = {

  blue: blockAfter(`[data-theme='blue']`),

};

describe('配色主题令牌', () => {
  it('单一主题块定义完整的 14 个变量', () => {
    for (const [name, body] of Object.entries(themeBlocks)) {
      for (const variable of THEME_VARS) {
        expect(body, `${name} 缺少 ${variable}`).toContain(`${variable}:`);
      }
    }
  });

  it(':root 承载共享层与电蓝默认值', () => {
    const root = blockAfter(':root {');
    for (const variable of THEME_VARS) expect(root, `:root 缺少 ${variable}`).toContain(`${variable}:`);
    for (const variable of SHARED_ONLY) expect(root, `:root 缺少共享 ${variable}`).toContain(`${variable}:`);
  });

  it('语义色 / 图表色 / 媒体色 / 顺序色阶不在任何主题块里', () => {
    for (const [name, body] of Object.entries(themeBlocks)) {
      for (const variable of SHARED_ONLY) {
        expect(body, `${name} 不应覆盖 ${variable}`).not.toContain(`${variable}:`);
      }
    }
  });

});
