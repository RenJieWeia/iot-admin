import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { snapshotPlanQueries, writeCalls } from '../test/server';

async function waitForRows(expected: number) {
  await waitFor(
    () => {
      expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
      expect(document.querySelectorAll('tbody tr')).toHaveLength(expected);
    },
    { timeout: 5000 },
  );
}

describe('快照计划页（B4）', () => {
  it('列表展示派生列「已启用档位」，筛选不写死终端类型', async () => {
    renderApp('/snapshot-plans');
    await waitForRows(2);

    // 派生列由服务端 SQL 算好（秒30, 分5, 时1 / 分1），前端只展示。
    expect(screen.getByText('秒30, 分5, 时1')).toBeInTheDocument();
    expect(screen.getByText('分1')).toBeInTheDocument();
    // 列表请求不带任何硬编码过滤（导航/过滤都由用户与后端驱动）。
    expect(snapshotPlanQueries.length).toBeGreaterThan(0);
    expect(snapshotPlanQueries.at(-1) ?? '').not.toContain('filter.');
  });

  it('顶部渲染「存储与保留」三行，改值走 PUT，空值表示永久保留', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-plans');
    await waitForRows(2);

    const section = screen.getByRole('region', { name: '存储与保留' });
    // 三行固定：粗表 / 分表 / 秒表（行集不可增删）。
    const seconds = within(section).getByLabelText('秒表（秒） 保留天数');
    const coarse = within(section).getByLabelText('粗表（时 / 日 / 月 / 年） 保留天数');
    expect(seconds).toHaveValue(7);
    expect(coarse).toHaveValue(null); // 空 = 永久保留

    await user.clear(seconds);
    await user.type(seconds, '30');
    await user.click(within(section).getAllByRole('button', { name: '保存' })[2]!);

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('PUT');
    expect(writeCalls[0]?.path).toBe('snapshot-settings/snapshot_second');
    expect(writeCalls[0]?.body).toMatchObject({ retention_days: 30 });

    // 清空 = 永久保留：提交 null。
    await user.clear(seconds);
    await user.click(within(section).getAllByRole('button', { name: '保存' })[2]!);
    await waitFor(() => expect(writeCalls).toHaveLength(2));
    expect(writeCalls[1]?.body).toMatchObject({ retention_days: null });
  });

  it('非法保留期本地拦下，不发请求', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-plans');
    await waitForRows(2);

    const section = screen.getByRole('region', { name: '存储与保留' });
    const minute = within(section).getByLabelText('分表（分） 保留天数');
    await user.clear(minute);
    await user.type(minute, '-1');
    await user.click(within(section).getAllByRole('button', { name: '保存' })[1]!);

    expect(await within(section).findByText(/保留天数必须是正整数/)).toBeInTheDocument();
    expect(writeCalls).toHaveLength(0);
  });

  it('计划抽屉提供六档「启用 + 步长」12 个字段（档位固定，无新增档位入口）', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-plans');
    await waitForRows(2);

    await user.click(screen.getAllByRole('button', { name: '编辑' })[0]!);
    const dialog = await screen.findByRole('dialog');
    for (const label of ['秒档', '秒步长', '分档', '分步长', '时档', '时步长', '日档', '日步长', '月档', '月步长', '年档', '年步长']) {
      expect(within(dialog).getByLabelText(new RegExp(`^${label}`))).toBeInTheDocument();
    }
    // 档位是列不是行：页面/抽屉都不存在「新增档位」入口。
    expect(within(dialog).queryByRole('button', { name: /新增档位/ })).not.toBeInTheDocument();
  });
});
