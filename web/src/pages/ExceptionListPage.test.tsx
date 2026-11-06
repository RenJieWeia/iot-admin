import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { writeCalls } from '../test/server';

describe('异常只读两页', () => {
  it('异常事件：渲染行、枚举中文来自 _meta 字典，无任何写入口', async () => {
    renderApp('/exceptions/events');
    await screen.findByRole('table');

    await waitFor(() =>
      expect(screen.getByText('第 1–2 条，共 2 条')).toBeInTheDocument(),
    );
    // enums 字典由前端归一后渲染为中文标签（筛选下拉里也有同名选项，只看表格）。
    const table = screen.getByRole('table');
    expect(within(table).getByText('上报超时')).toBeInTheDocument();
    expect(within(table).getByText('协议错误')).toBeInTheDocument();

    expect(screen.queryByRole('button', { name: '新建' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '删除' })).not.toBeInTheDocument();
    expect(screen.getByText(/只读页面/)).toBeInTheDocument();
  });

  it('异常区间：只读页面且不出现操作列', async () => {
    renderApp('/exceptions/intervals');
    await screen.findByRole('table');
    const headers = screen.getAllByRole('columnheader').map((th) => th.textContent ?? '');
    expect(headers).not.toContain('操作');
    expect(screen.getByText(/只读页面/)).toBeInTheDocument();
  });

  it('整页不发出任何写请求（含打开筛选、翻页）', async () => {
    const user = userEvent.setup();
    renderApp('/exceptions/events');
    await screen.findByRole('table');
    expect(writeCalls).toHaveLength(0);

    const chip = screen.queryByRole('list', { name: '已生效筛选条件' });
    expect(chip).toBeNull();
    await user.click(screen.getByRole('button', { name: '下一页' }));
    await waitFor(() => expect(writeCalls).toHaveLength(0));
    const table = await screen.findByRole('table');
    expect(within(table).getAllByRole('row').length).toBeGreaterThan(1);
  });

  it('未知 slot 给出原因与返回路径', async () => {
    // 异常两页与通用只读页共用一个实现，文案统一为「未知只读页」。
    renderApp('/exceptions/unknown');
    expect(await screen.findByText('未知只读页')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '返回上一页' })).toBeInTheDocument();
  });
});
