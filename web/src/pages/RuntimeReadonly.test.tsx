import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { server, writeCalls } from '../test/server';

function bodyRows(): HTMLElement[] {
  return Array.from(document.querySelectorAll('tbody tr')) as HTMLElement[];
}

async function waitForRows(expected: number) {
  await waitFor(
    () => {
      expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
      expect(bodyRows()).toHaveLength(expected);
    },
    { timeout: 5000 },
  );
}

describe('小时暂存（offset 分页）', () => {
  it('沿用页码分页并显示总数，不出现「加载更多」', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/hourly-staging');
    await waitForRows(10);

    expect(screen.getByText('第 1–10 条，共 25 条')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();

    // 页码分页按钮带 touch-hit（触屏下热区 ≥44，视觉高度仍是 h-8）。
    expect(screen.getByRole('button', { name: '下一页' }).className).toContain('touch-hit');
    await user.click(screen.getByRole('button', { name: '下一页' }));
    await waitFor(() => expect(screen.getByText('第 11–20 条，共 25 条')).toBeInTheDocument());
    expect(view.location()).toContain('page=2');
  });
});

describe('小时快照（cursor 分页）', () => {
  it('首屏只加载一批，不显示总数，给出「加载更多」', async () => {
    renderApp('/readonly/hourly-snapshot');
    await waitForRows(20);

    expect(
      screen.getByText(/已加载 20 条 · 每批 20 条（游标分页，不显示总数；只能继续加载，不能回到指定页）/),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '加载更多' })).toBeInTheDocument();
    // 表格说明同样不报数（§16.1）：条数只在游标分页区出现。
    expect(document.querySelector('caption')?.textContent).toBe('当前筛选结果');
    // 分区表上不显示 total：整页不应出现「共 N 条」。
    expect(screen.queryByText(/共 \d+ 条/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '下一页' })).not.toBeInTheDocument();
  });

  it('加载更多把 nextCursor 写进 URL，两批数据不重叠；到底后隐藏按钮', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/hourly-snapshot');
    await waitForRows(20);
    const firstBatch = bodyRows().map((row) => row.textContent);

    await user.click(screen.getByRole('button', { name: '加载更多' }));
    await waitFor(() => expect(view.location()).toContain('cursor=off'));
    await waitFor(() => expect(screen.getByText(/已加载 20 条/)).toBeInTheDocument());
    await waitFor(() => {
      const secondBatch = bodyRows().map((row) => row.textContent);
      expect(secondBatch.some((text) => firstBatch.includes(text))).toBe(false);
    });

    // 第三批只剩 5 行，hasMore=false → 隐藏按钮并提示已到末尾。
    await user.click(screen.getByRole('button', { name: '加载更多' }));
    await waitFor(() => expect(screen.getByText(/已加载 5 条/)).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
    expect(screen.getByText('已到末尾')).toBeInTheDocument();
  });

  it('游标页不给排序入口：后端固定 settled_at desc，传 sort/order 会 400', async () => {
    renderApp('/readonly/hourly-snapshot');
    await waitForRows(20);
    // 没有可点击的排序按钮（点了会 400），但默认排序列仍有静态方向标识。
    expect(screen.queryByRole('button', { name: /^按.+排序$/ })).not.toBeInTheDocument();
    const headers = screen.getAllByRole('columnheader');
    const settled = headers.find((th) => th.textContent?.includes('结算时间'));
    expect(settled).toBeDefined();
    expect(settled).toHaveAttribute('aria-sort', 'descending');
    expect(settled?.textContent).toContain('↓');
  });

  it('revision 徽标：0 显示 —，>0 显示数字且 title 为「已修正 N 次」', async () => {
    renderApp('/readonly/hourly-snapshot');
    await waitForRows(20);
    const table = screen.getByRole('table');

    // 第一行 revision=1；第 2..4 行 revision=0。
    expect(within(table).getAllByTitle('已修正 1 次').length).toBeGreaterThan(0);
    expect(within(table).getAllByText('—').length).toBeGreaterThan(0);
  });

  it('「仅看被修正过」= revision.gte=1，条件标签同步', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/hourly-snapshot');
    await waitForRows(20);

    await user.click(screen.getByRole('checkbox', { name: /仅看被修正过/ }));
    await waitFor(() => expect(view.location()).toContain('filter.revision.gte=1'));
    // 45 行里每 4 行有 1 行被修正 → 12 行，一批装得下，hasMore=false。
    await waitFor(() => expect(screen.getByText(/已加载 12 条/)).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();

    const chips = screen.getByRole('list', { name: '已生效筛选条件' });
    expect(within(chips).getByText('修正次数')).toBeInTheDocument();
    expect(within(chips).getByText('≥')).toBeInTheDocument();
  });

  it('翻页不重置筛选：加载更多时保留 revision 条件', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/hourly-snapshot?filter.revision.gte=1');
    await waitForRows(12);

    // 只有 12 行 → 没有更多；用 quality 条件再验一次 URL 组合保留。
    await user.click(screen.getByRole('checkbox', { name: /仅看有问题/ }));
    await waitFor(() => expect(view.location()).toContain('filter.quality.gte=1'));
    expect(view.location()).toContain('filter.revision.gte=1');
  });
});

describe('两页的只读约束', () => {
  it('无写入口、不渲染抽屉、全程零写请求', async () => {
    const user = userEvent.setup();
    for (const slot of ['hourly-staging', 'hourly-snapshot']) {
      const view = renderApp(`/readonly/${slot}`);
      await waitFor(() => expect(bodyRows().length).toBeGreaterThan(0), { timeout: 5000 });

      expect(screen.queryByRole('button', { name: '新建' })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: '删除' })).not.toBeInTheDocument();
      expect(document.querySelector('[data-action]')).toBeNull();
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
      expect(screen.getByText(/只读页面/)).toBeInTheDocument();

      const nextButton = screen.queryByRole('button', { name: '加载更多' })
        ?? screen.queryByRole('button', { name: '下一页' });
      if (nextButton) await user.click(nextButton);

      expect(writeCalls).toHaveLength(0);
      view.unmount();
    }
  });

  it('导航「数据」分组按 _meta.readonly 顺序列出三项', async () => {
    renderApp('/readonly/hourly-snapshot');
    await waitForRows(20);
    const nav = screen.getByRole('navigation', { name: '主导航' });
    const dataLinks = within(nav)
      .getAllByRole('link')
      .map((link) => link.textContent?.trim())
      .filter((text) => text === '最新值' || text === '小时暂存' || text === '小时快照');
    expect(dataLinks).toEqual(['最新值', '小时暂存', '小时快照']);
  });
});

describe('快照页三态', () => {
  it('无数据 / 无匹配 / 失败 文案互不相同', async () => {
    const seen = new Set<string>();

    server.use(
      http.get('/admin/v1/readonly/hourly-snapshot', () =>
        HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { hasMore: false, nextCursor: null, limit: 20 },
        }),
      ),
    );
    const empty = renderApp('/readonly/hourly-snapshot');
    seen.add((await screen.findByText('暂无小时快照')).textContent ?? '');
    empty.unmount();
    cleanup();
    server.resetHandlers();

    const nomatch = renderApp('/readonly/hourly-snapshot?filter.revision.gte=99');
    seen.add((await screen.findByText('没有匹配的记录')).textContent ?? '');
    nomatch.unmount();
    cleanup();

    server.use(
      http.get('/admin/v1/readonly/hourly-snapshot', () =>
        HttpResponse.json({ code: 500, message: 'db_unavailable' }, { status: 500 }),
      ),
    );
    const error = renderApp('/readonly/hourly-snapshot');
    seen.add((await screen.findByText('列表加载失败')).textContent ?? '');
    expect(screen.getByText(/db_unavailable/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument();
    error.unmount();

    expect(seen.size).toBe(3);
  });
});
