import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { metaFixture } from '../test/fixtures';
import { renderApp } from '../test/utils';
import { server } from '../test/server';

/**
 * 列表页摘要行（1107009 §4）：只消费现成数据（meta.total + 是否有筛选条件），不新增接口。
 * 三种状态（0 条 / 有数据 / 有筛选）都要出现同一行、同一高度——否则数字位数或条件
 * 有无会让内容跳一下（§6.4 布局稳定性）。
 */
describe('列表页摘要行', () => {
  it('有数据时显示「共 N 条」，无筛选时不带「已筛选」', async () => {
    renderApp('/terminals');
    const summary = await screen.findByTestId('list-summary');
    // 夹具里 terminals 的总数是 128（meta.total），与表格行数无关。
    await waitFor(() => expect(summary.textContent).toContain('共 128 条'));
    expect(summary.textContent).not.toContain('已筛选');
    expect(summary.className).toContain('h-6');
  });

  it('0 条时仍然渲染这一行（显示 0，不隐藏）', async () => {
    server.use(
      http.get('/admin/v1/terminals', () =>
        HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { total: 0, page: 1, pageSize: 10, totalPages: 0, sort: 'id', order: 'desc' },
        }),
      ),
    );
    renderApp('/terminals');
    const summary = await screen.findByTestId('list-summary');
    await waitFor(() => expect(summary.textContent).toContain('共 0 条'));
  });

  it('有筛选条件时附带「已筛选」，且仍是同一行同一高度', async () => {
    renderApp('/terminals?filter.enabled.eq=true');
    const summary = await screen.findByTestId('list-summary');
    await waitFor(() => expect(summary.textContent).toContain('已筛选'));
    expect(summary.className).toContain('h-6');
    expect(metaFixture.entities.some((e) => e.name === 'terminals')).toBe(true);
  });
});
