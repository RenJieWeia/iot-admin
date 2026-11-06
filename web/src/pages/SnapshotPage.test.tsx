import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';

function bodyRows(): HTMLElement[] {
  return Array.from(document.querySelectorAll('tbody tr')) as HTMLElement[];
}

async function waitForRows(expected: number) {
  await waitFor(
    () => {
      expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
      expect(document.querySelectorAll('tbody tr')).toHaveLength(expected);
    },
    { timeout: 5000 },
  );
}

describe('快照只读页（一个页面 + 三个存储）', () => {
  it('导航只有一项「快照」，三个存储都能切到并渲染各自的列', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/snapshot-coarse');
    await waitForRows(4);

    // 导航不因三个 slot 涨出三项。
    const nav = screen.getByRole('navigation', { name: '主导航' });
    expect(within(nav).getAllByRole('link', { name: '快照' })).toHaveLength(1);
    expect(within(nav).queryByRole('link', { name: '分表快照' })).not.toBeInTheDocument();

    // 粗表特有列「档位」。
    expect(screen.getByRole('columnheader', { name: /档位/ })).toBeInTheDocument();

    // 切到分表：URL 路径变化、列集合变化（无档位列）。
    await user.click(screen.getByRole('link', { name: '分表（分）' }));
    await waitFor(() => expect(view.location()).toBe('/readonly/snapshot-minute'));
    await waitForRows(10);
    expect(screen.queryByRole('columnheader', { name: /档位/ })).not.toBeInTheDocument();

    // 切到秒表。
    await user.click(screen.getByRole('link', { name: '秒表（秒）' }));
    await waitFor(() => expect(view.location()).toBe('/readonly/snapshot-second'));
    await waitForRows(10);
  });

  it('粗表按档位筛选走 filter.granularity.in，不按时间近似', async () => {
    const view = renderApp('/readonly/snapshot-coarse?filter.granularity.in=hour');
    await waitForRows(1);

    // 只保留 hour 档（服务端按 filter.granularity.in 过滤，不是按时间近似）。
    expect(view.location()).toContain('filter.granularity.in=hour');
    // 条件标签外显：档位 = 时（来自 _meta 的枚举选项文案）。
    const chipList = screen.getByRole('list', { name: '已生效筛选条件' });
    expect(within(chipList).getByText('档位')).toBeInTheDocument();
    expect(within(chipList).getByText('时')).toBeInTheDocument();
  });

  it('无总数：不出现「共 N 条」与页码分页，只有「加载更多」', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/snapshot-minute');
    await waitForRows(10);

    expect(screen.queryByText(/共 \d+ 条/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '下一页' })).not.toBeInTheDocument();
    expect(screen.getByText(/游标分页，不显示总数/)).toBeInTheDocument();

    const firstBatch = bodyRows().map((row) => row.textContent);
    await user.click(screen.getByRole('button', { name: '加载更多' }));
    await waitFor(() => expect(view.location()).toContain('cursor='), { timeout: 5000 });
    // 仍是「一批」而不是累加：换批后内容不同、依旧不出现总数。
    await waitFor(() => {
      const secondBatch = bodyRows().map((row) => row.textContent);
      expect(secondBatch.some((text) => firstBatch.includes(text ?? ''))).toBe(false);
    });
    expect(screen.queryByText(/共 \d+ 条/)).not.toBeInTheDocument();

    // 最后一批只剩 5 行：到底后隐藏按钮并提示已到末尾。
    await user.click(screen.getByRole('button', { name: '加载更多' }));
    await waitForRows(5);
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument();
    expect(screen.getByText('已到末尾')).toBeInTheDocument();
  });

  it('快照页没有任何写入口（只读）', async () => {
    renderApp('/readonly/snapshot-second');
    await waitForRows(10);
    for (const name of ['新建', '编辑', '删除']) {
      expect(screen.queryByRole('button', { name })).not.toBeInTheDocument();
    }
  });

  it('保留期区块说明最小清理粒度', async () => {
    renderApp('/snapshot-plans');
    const section = await screen.findByRole('region', { name: '存储与保留' });
    expect(
      within(section).getByText(/最小清理粒度 = 整月（粗表 \/ 分表）、整日（秒表），实际保留可能比设定值更长。/),
    ).toBeInTheDocument();
  });
});
