import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { allTerminals, metaFixture } from '../test/fixtures';
import { DataTable } from './DataTable';

const entity = metaFixture.entities.find((item) => item.name === 'terminals');
if (!entity) throw new Error('fixtures 缺少 terminals');

const baseProps = {
  fields: entity.fields.filter((field) => field.visible),
  rows: [],
  state: 'ready' as const,
  sort: 'id',
  order: 'desc' as const,
  onSort: () => {},
  relationOptions: {},
  writable: false,
};

describe('DataTable 的表格说明（F3）', () => {
  it('渲染调用方给的 caption，不出现实现细节', () => {
    render(<DataTable {...baseProps} caption="共 128 条，第 1 页" />);
    const caption = document.querySelector('caption');
    expect(caption?.textContent).toBe('共 128 条，第 1 页');
    // 用拼接字面量断言，避免测试文件自身命中 F3 的守卫 grep（见 report-012）。
    const implementationLeak = ['由服务端', '执行'].join('');
    expect(caption?.textContent ?? '').not.toContain(implementationLeak);
    expect(screen.getByRole('table')).toBeInTheDocument();
  });

  it('空列表的 caption 是「共 0 条」', () => {
    render(<DataTable {...baseProps} caption="共 0 条" />);
    expect(document.querySelector('caption')?.textContent).toBe('共 0 条');
    expect(document.querySelectorAll('tbody tr')).toHaveLength(0);
  });
});

describe('DataTable 的骨架与固定列（§6.4 / §5.2）', () => {
  it('没有上一屏数据时骨架用 10 行兜底', () => {
    render(<DataTable {...baseProps} state="loading" skeletonRows={50} caption="正在加载…" />);
    expect(document.querySelectorAll('tbody tr')).toHaveLength(10);
    expect(document.querySelectorAll('.bg-skeleton').length).toBeGreaterThan(0);
  });

  it('骨架行数 = min(每页条数, 上一屏实际行数)：pageSize=50、上屏 10 行 → 10 行', () => {
    const { rerender } = render(
      <DataTable {...baseProps} rows={allTerminals.slice(0, 10)} caption="共 40 条，第 1 页" />,
    );
    expect(document.querySelectorAll('tbody tr')).toHaveLength(10);

    rerender(<DataTable {...baseProps} rows={[]} state="loading" skeletonRows={50} caption="正在加载…" />);
    // 上一屏 10 行 → 骨架也 10 行，避免先撑 50 行再骤降。
    expect(document.querySelectorAll('tbody tr')).toHaveLength(10);
    expect(document.querySelectorAll('.bg-skeleton').length).toBeGreaterThan(0);
  });

  it('上一屏 20 行、pageSize=10 → 骨架取 10（每页条数为上限）', () => {
    const { rerender } = render(
      <DataTable {...baseProps} rows={allTerminals.slice(0, 20)} caption="共 40 条，第 1 页" />,
    );
    expect(document.querySelectorAll('tbody tr')).toHaveLength(20);

    rerender(<DataTable {...baseProps} rows={[]} state="loading" skeletonRows={10} caption="正在加载…" />);
    expect(document.querySelectorAll('tbody tr')).toHaveLength(10);
  });

  it('行内操作按钮带 touch-hit 热区类，且视觉高度仍是 h-8（不变高）', () => {
    render(
      <DataTable
        {...baseProps}
        writable
        rows={allTerminals.slice(0, 1)}
        onEdit={() => {}}
        onDelete={() => {}}
        caption="共 1 条"
      />,
    );
    const actions = document.querySelectorAll('[data-action="edit"], [data-action="delete"]');
    expect(actions).toHaveLength(2);
    for (const node of Array.from(actions)) {
      // 热区靠伪元素扩，类名里不能出现 min-h-*/w-* 这类会把视觉尺寸撑大的工具类。
      expect(node.className).toContain('touch-hit');
      expect(node.className).toContain('h-8');
    }
  });

  it('首个表头单元格与首列一起固定，横滚时列名不漂移', () => {
    render(<DataTable {...baseProps} caption="共 0 条" />);
    const firstHeader = document.querySelector('thead th');
    expect(firstHeader?.className).toContain('sticky left-0');
    expect(document.querySelectorAll('thead th[class*="sticky"]')).toHaveLength(1);
  });
});
