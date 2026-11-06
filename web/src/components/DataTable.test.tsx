import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { metaFixture } from '../test/fixtures';
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
  it('骨架行数跟随调用方，不再被截到 10 行', () => {
    render(<DataTable {...baseProps} state="loading" skeletonRows={20} caption="正在加载…" />);
    expect(document.querySelectorAll('tbody tr')).toHaveLength(20);
    expect(document.querySelectorAll('.bg-skeleton').length).toBeGreaterThan(0);
  });

  it('首个表头单元格与首列一起固定，横滚时列名不漂移', () => {
    render(<DataTable {...baseProps} caption="共 0 条" />);
    const firstHeader = document.querySelector('thead th');
    expect(firstHeader?.className).toContain('sticky left-0');
    expect(document.querySelectorAll('thead th[class*="sticky"]')).toHaveLength(1);
  });
});
