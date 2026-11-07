import { describe, expect, it } from 'vitest';
import { scrollEdges } from './scrollEdges';

// 纯函数单测：jsdom 里 scrollWidth/clientWidth 恒为 0，渲染层拿不到真值，
// 所以「根据尺寸算两个布尔」的逻辑在这里钉死，DOM 层只做一次冒烟（见 DataTable.test.tsx）。
describe('scrollEdges（横向滚动两侧状态）', () => {
  it('内容不溢出时两侧都不显示（宽屏下不该有"脏边"）', () => {
    expect(scrollEdges({ scrollLeft: 0, scrollWidth: 1000, clientWidth: 1000 })).toEqual({
      left: false,
      right: false,
    });
    // 只差 1px 视为不溢出（亚像素/缩放取整）。
    expect(scrollEdges({ scrollLeft: 0, scrollWidth: 1001, clientWidth: 1000 })).toEqual({
      left: false,
      right: false,
    });
  });

  it('溢出且尚未滚动：只有右侧（还有内容在右边）', () => {
    expect(scrollEdges({ scrollLeft: 0, scrollWidth: 1600, clientWidth: 1000 })).toEqual({
      left: false,
      right: true,
    });
  });

  it('滚到中间：两侧同时为真', () => {
    expect(scrollEdges({ scrollLeft: 300, scrollWidth: 1600, clientWidth: 1000 })).toEqual({
      left: true,
      right: true,
    });
  });

  it('滚到最右：只剩左侧，右侧消失（含 1px 容差）', () => {
    expect(scrollEdges({ scrollLeft: 600, scrollWidth: 1600, clientWidth: 1000 })).toEqual({
      left: true,
      right: false,
    });
    expect(scrollEdges({ scrollLeft: 599, scrollWidth: 1600, clientWidth: 1000 })).toEqual({
      left: true,
      right: false,
    });
  });

  it('滚回原位：左投影消失', () => {
    expect(scrollEdges({ scrollLeft: 0, scrollWidth: 1600, clientWidth: 1000 }).left).toBe(false);
  });
});
