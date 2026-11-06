import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { server, writeCalls } from '../test/server';

function bodyRows(): HTMLElement[] {
  return Array.from(document.querySelectorAll('tbody tr')) as HTMLElement[];
}

async function waitForReady(expectedRows = 10) {
  await waitFor(
    () => {
      expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
      expect(bodyRows()).toHaveLength(expectedRows);
    },
    { timeout: 5000 },
  );
}

describe('最新值只读页（task-009 Part B）', () => {
  it('渲染当前页 10 行、总数 128，quality 标签按 _meta.options 渲染', async () => {
    renderApp('/readonly/latest-values');
    await waitForReady();
    expect(screen.getByText('第 1–10 条，共 128 条')).toBeInTheDocument();

    const table = screen.getByRole('table');
    // options 的 label 直接来自 _meta，前端零硬编码；首 10 行 quality 依次是 0..7,0,1。
    for (const label of ['正常', '越界', '规则命中', '越界+规则', '单位闭集外', '越界+单位', '规则+单位', '越界+规则+单位']) {
      expect(within(table).getAllByText(label).length).toBeGreaterThan(0);
    }
    // 质量原因单行截断 + title 全文。
    const reason = within(table).getAllByTitle(/原因-/)[0];
    expect(reason).toBeInTheDocument();
    // 时间显示为绝对时间 +08:00。
    expect(within(table).getAllByText(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \+08:00/).length).toBeGreaterThan(0);
  });

  it('只读：无新建/编辑/删除入口、不渲染抽屉、零写请求', async () => {
    const user = userEvent.setup();
    renderApp('/readonly/latest-values');
    await waitForReady();

    expect(screen.queryByRole('button', { name: '新建' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '删除' })).not.toBeInTheDocument();
    expect(document.querySelector('[data-action="new"]')).toBeNull();
    expect(document.querySelector('[data-action="edit"]')).toBeNull();
    expect(screen.getByText(/只读页面/)).toBeInTheDocument();

    // 翻页、筛选、排序都不应发出写请求。
    await user.click(screen.getByRole('button', { name: '下一页' }));
    await waitFor(() => expect(screen.getByText('第 11–20 条，共 128 条')).toBeInTheDocument());
    expect(writeCalls).toHaveLength(0);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('「仅看有问题」= filter.quality.gte=1，标签外显且可单删', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/latest-values');
    await waitForReady();

    const toggle = screen.getByRole('checkbox', { name: /仅看有问题/ });
    expect(toggle).not.toBeChecked();

    await user.click(toggle);
    await waitFor(() => expect(view.location()).toContain('filter.quality.gte=1'));
    await waitFor(() => expect(screen.getByText('第 1–10 条，共 112 条')).toBeInTheDocument());

    const chips = screen.getByRole('list', { name: '已生效筛选条件' });
    expect(within(chips).getByText('质量')).toBeInTheDocument();
    expect(within(chips).getByText('≥')).toBeInTheDocument();
    expect(within(chips).getByText('1')).toBeInTheDocument();

    // 单删标签：条件与复选框状态一起回退。
    await user.click(screen.getByRole('button', { name: /移除筛选 质量 ≥ 1/ }));
    await waitFor(() => expect(view.location()).not.toContain('quality.gte'));
    await waitFor(() => expect(screen.getByText('第 1–10 条，共 128 条')).toBeInTheDocument());
    expect(screen.getByRole('checkbox', { name: /仅看有问题/ })).not.toBeChecked();
  });

  it('已生效条件可一键全清', async () => {
    const user = userEvent.setup();
    const view = renderApp('/readonly/latest-values?q=t-01&filter.quality.gte=1');
    await screen.findByRole('list', { name: '已生效筛选条件' });

    await user.click(screen.getByRole('button', { name: '清除全部筛选' }));
    await waitFor(() => expect(view.location()).not.toContain('filter.'));
    expect(view.location()).not.toContain('q=');
    await waitForReady();
  });

  it('三态文案互不相同（无数据 / 无匹配 / 失败）', async () => {
    const seen = new Set<string>();

    server.use(
      http.get('/admin/v1/readonly/:slot', () =>
        HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { total: 0, page: 1, pageSize: 10, sort: 'value_time', order: 'desc' },
        }),
      ),
    );
    const empty = renderApp('/readonly/latest-values');
    seen.add((await screen.findByText('暂无最新值')).textContent ?? '');
    empty.unmount();
    cleanup();
    server.resetHandlers();

    const nomatch = renderApp('/readonly/latest-values?q=zzz');
    seen.add((await screen.findByText('没有匹配的记录')).textContent ?? '');
    nomatch.unmount();
    cleanup();

    server.use(
      http.get('/admin/v1/readonly/:slot', () =>
        HttpResponse.json({ code: 500, message: 'db_unavailable' }, { status: 500 }),
      ),
    );
    const error = renderApp('/readonly/latest-values');
    seen.add((await screen.findByText('列表加载失败')).textContent ?? '');
    expect(screen.getByText(/db_unavailable/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument();
    error.unmount();

    expect(seen.size).toBe(3);
  });

  it('导航「数据」分组下出现「最新值」，未知 slot 给出原因与返回路径', async () => {
    renderApp('/readonly/latest-values');
    await waitForReady();
    expect(screen.getByRole('link', { name: '最新值' })).toBeInTheDocument();

    cleanup();
    renderApp('/readonly/unknown-slot');
    expect(await screen.findByText('未知只读页')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '返回上一页' })).toBeInTheDocument();
  });
});
