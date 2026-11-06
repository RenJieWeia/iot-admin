import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const css = readFileSync(join(process.cwd(), 'src/styles/tailwind.css'), 'utf8');

/** 取某个 @utility 的完整块（到下一个 @utility 为止）。 */
function utilityBlock(name: string): string {
  const start = css.indexOf(`@utility ${name}`);
  if (start < 0) throw new Error(`缺少 @utility ${name}`);
  const next = css.indexOf('@utility ', start + 1);
  return css.slice(start, next < 0 ? undefined : next);
}

describe('触摸热区（§14.1）', () => {
  it('touch-hit 只在粗指针环境生效，用伪元素扩热区而不改视觉尺寸', () => {
    const block = utilityBlock('touch-hit');
    // 只在触屏（粗指针）生效，桌面鼠标环境保持原密度。
    expect(block).toContain('@media (pointer: coarse)');
    // 尺寸来自令牌（--touch-target = 44px），不是散落的像素值。
    expect(block).toContain('var(--touch-target)');
    // 绝对定位的伪元素脱离文档流：不改按钮盒模型，因此表格行高/容器高度不变。
    expect(block).toContain('position: absolute');
    expect(block).toContain('&::after');
    // 类里不得出现会撑大视觉尺寸的 min-height/min-width（那是断点版 touch-target 的职责）。
    expect(block).not.toMatch(/min-height|min-width/);
  });

  it('断点版 touch-target 仍保留，两者语义不同且规则不重复', () => {
    const legacy = utilityBlock('touch-target');
    expect(legacy).toContain('@media (max-width: 1023.98px)');
    expect(legacy).toContain('var(--touch-target)');
    // touch-hit 不再重复声明断点条件。
    expect(utilityBlock('touch-hit')).not.toContain('max-width: 1023.98px');
  });
});
