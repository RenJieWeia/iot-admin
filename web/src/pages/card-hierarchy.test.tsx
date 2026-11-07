import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';

/**
 * 卡片层级硬约束（1107010）：
 * - 卡片之间**平级不嵌套**：`[data-card] [data-card]` 必须为 0；
 * - 背景层（面包屑 / h1 / 摘要行）**不进卡**：closest('[data-card]') 为 null；
 * - 每页卡片数量按形态断言，不写「≥2」这种含糊条件。
 */
async function expectNoNesting() {
  await waitFor(() => expect(document.querySelectorAll('[data-card]').length).toBeGreaterThan(0));
  expect(document.querySelectorAll('[data-card] [data-card]').length).toBe(0);
  // 面包屑与 h1 在背景层。
  expect(screen.getByRole('navigation', { name: '面包屑' }).closest('[data-card]')).toBeNull();
  expect(screen.getByRole('heading', { level: 1 }).closest('[data-card]')).toBeNull();
}

describe('卡片层级', () => {
  it('列表页：两张平级卡（筛选 / 数据），摘要行在背景层', async () => {
    renderApp('/terminals');
    await screen.findByTestId('list-summary');
    await expectNoNesting();
    expect(document.querySelectorAll('[data-card]')).toHaveLength(2);
    // 摘要行不进卡。
    expect(screen.getByTestId('list-summary').closest('[data-card]')).toBeNull();
  });

  it('只读页：两张（筛选 / 数据）', async () => {
    renderApp('/readonly/latest-values');
    await screen.findAllByRole('table');
    await expectNoNesting();
    expect(document.querySelectorAll('[data-card]')).toHaveLength(2);
  });

  it('异常页：两张（筛选 / 数据）', async () => {
    renderApp('/exceptions/events');
    await screen.findAllByRole('table');
    await expectNoNesting();
    expect(document.querySelectorAll('[data-card]')).toHaveLength(2);
  });

  it('快照配置页：三张（六档 / 保留期 / 绑定）', async () => {
    renderApp('/snapshot-slots');
    await screen.findByRole('group', { name: '六档配置' });
    await expectNoNesting();
    expect(document.querySelectorAll('[data-card]')).toHaveLength(3);
  });

  it('终端类型详情页：一张（属性表）；摘要信息在背景层', async () => {
    renderApp('/device-types/1');
    await screen.findAllByRole('table');
    await expectNoNesting();
    // 单卡页面：需求方已逐案确认（摘要不包卡）。
    expect(document.querySelectorAll('[data-card]')).toHaveLength(1);
    expect(screen.getByLabelText('终端类型摘要').closest('[data-card]')).toBeNull();
  });
});
