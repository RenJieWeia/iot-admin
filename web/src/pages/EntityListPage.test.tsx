import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { server, writeCalls } from '../test/server';

function bodyRows(): HTMLElement[] {
  return Array.from(document.querySelectorAll('tbody tr')) as HTMLElement[];
}

/** 等到「加载态结束且渲染的是服务端数据」：骨架行同样是 10 行，不能只看行数。 */
async function waitForReady() {
  await waitFor(
    () => {
      expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
      expect(bodyRows()).toHaveLength(10);
      expect(bodyRows()[0]).toHaveTextContent('t-001');
    },
    { timeout: 5000 },
  );
}

describe('实体列表页（服务端驱动）', () => {
  it('只渲染服务端返回的当前页 10 行，总数 128 不落成 DOM', async () => {
    renderApp('/terminals');
    await screen.findByRole('table');
    await waitForReady();
    expect(screen.getByText('第 1–10 条，共 128 条')).toBeInTheDocument();
    // 前端不做本地分页：第 11 条只可能来自服务端下一页。
    expect(bodyRows()[0]).toHaveTextContent('t-001');
  });

  it('四态文案互不相同（加载 / 无数据 / 无匹配 / 失败）', async () => {
    const seen = new Set<string>();

    server.use(
      http.get('/admin/v1/terminals', async () => {
        await new Promise((resolve) => setTimeout(resolve, 300));
        return HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { total: 0, page: 1, pageSize: 10, sort: 'id', order: 'desc' },
        });
      }),
    );
    const loading = renderApp('/terminals');
    seen.add((await screen.findByText('正在加载列表…')).textContent ?? '');
    loading.unmount();
    cleanup();
    server.resetHandlers();

    server.use(
      http.get('/admin/v1/terminals', () =>
        HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { total: 0, page: 1, pageSize: 10, sort: 'id', order: 'desc' },
        }),
      ),
    );
    const empty = renderApp('/terminals');
    seen.add((await screen.findByText('还没有终端')).textContent ?? '');
    empty.unmount();
    cleanup();
    server.resetHandlers();

    const nomatch = renderApp('/terminals?q=zzz');
    seen.add((await screen.findByText('没有匹配的记录')).textContent ?? '');
    // 无匹配态必须给「清除筛选」出口，不能只写一句空话。
    expect(screen.getByRole('button', { name: '清除筛选' })).toBeInTheDocument();
    nomatch.unmount();
    cleanup();

    server.use(
      http.get('/admin/v1/terminals', () =>
        HttpResponse.json({ code: 500, message: 'db_unavailable' }, { status: 500 }),
      ),
    );
    const error = renderApp('/terminals');
    seen.add((await screen.findByText('列表加载失败')).textContent ?? '');
    // 失败态必须含原因与恢复路径。
    expect(screen.getByText(/db_unavailable/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument();
    error.unmount();

    expect(seen.size).toBe(4);
  });

  it('已生效筛选以标签外显，可单删并回第一页', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals?filter.enabled.eq=true&page=2');
    await waitFor(() => expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument());

    const chipList = await screen.findByRole('list', { name: '已生效筛选条件' });
    expect(within(chipList).getByText('状态')).toBeInTheDocument();
    expect(within(chipList).getByText('等于')).toBeInTheDocument();
    expect(within(chipList).getByText('是')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /移除筛选 状态 等于 是/ }));
    await waitFor(() => {
      expect(view.location()).not.toContain('filter.enabled.eq');
    });
    expect(view.location()).not.toContain('page=2');
    await waitFor(() => expect(bodyRows()).toHaveLength(10));
  });

  it('页码越界自动回到第一页（replace，只跳一次，筛选保留）', async () => {
    const view = renderApp('/terminals?page=99&filter.enabled.eq=true&sort=name');
    // 回到第一页：URL 里 page 不再出现，但筛选与排序保留。
    await waitFor(() => expect(view.location()).not.toContain('page='));
    expect(view.location()).toContain('filter.enabled.eq=true');
    expect(view.location()).toContain('sort=name');
    expect(
      await screen.findByText('当前页无数据，已回到第一页（筛选条件保留）。'),
    ).toBeInTheDocument();
    await waitFor(() => expect(bodyRows()).toHaveLength(10));

    // 不震荡：等一拍后 URL 与行数都不再变化。
    const settled = view.location();
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(view.location()).toBe(settled);
    expect(bodyRows()).toHaveLength(10);
  });

  it('修正后第一页仍为空时按「无匹配」展示，不再跳转', async () => {
    // 模拟数据在翻页期间被删空：总数仍 > 0，但任何页都取不到行。
    server.use(
      http.get('/admin/v1/terminals', () =>
        HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { total: 5, page: 1, pageSize: 10, sort: 'id', order: 'desc' },
        }),
      ),
    );
    const view = renderApp('/terminals?page=99');
    await waitFor(() => expect(view.location()).not.toContain('page='));
    expect(await screen.findByText('没有匹配的记录')).toBeInTheDocument();
    const settled = view.location();
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(view.location()).toBe(settled);
  });

  it('翻页写 URL 并请求下一页，不重置筛选', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals');
    await waitForReady();

    await user.click(screen.getByRole('button', { name: '下一页' }));
    await waitFor(() => expect(view.location()).toContain('page=2'));
    await waitFor(() => expect(screen.getByText('第 11–20 条，共 128 条')).toBeInTheDocument());
  });

  it('关联/多选筛选不在常驻区，只在「更多筛选」里（R2）', async () => {
    const user = userEvent.setup();
    renderApp('/terminals');
    await waitForReady();

    // 常驻区：单行控件（terminal.id / gateway_id / name / enabled）。
    expect(screen.getByLabelText('终端 ID')).toBeInTheDocument();
    expect(screen.getByLabelText('网关 ID')).toBeInTheDocument();
    expect(screen.getByLabelText('状态')).toBeInTheDocument();
    // 关联选择器不进常驻区，避免把筛选条撑高、与单行控件高度参差。
    expect(screen.queryByLabelText('终端类型')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '更多筛选' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText('终端类型')).toBeInTheDocument();
  });

  it('抽屉：role=dialog、Esc 关闭、焦点回到触发按钮', async () => {
    const user = userEvent.setup();
    renderApp('/terminals');
    await waitForReady();

    const editButtons = screen.getAllByRole('button', { name: '编辑' });
    const firstEdit = editButtons[0];
    if (!firstEdit) throw new Error('缺少编辑按钮');
    await user.click(firstEdit);

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('编辑终端')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '取消' })).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '保存' })).toBeInTheDocument();

    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // 焦点回到触发按钮：用实时查询取当前节点（列表重渲染后旧引用可能已分离）。
    await waitFor(() => {
      const fresh = screen.getAllByRole('button', { name: '编辑' })[0];
      expect(fresh).toHaveFocus();
    });
  });

  it('新建保存后关闭抽屉、刷新列表并给轻提示', async () => {
    const user = userEvent.setup();
    renderApp('/terminals');
    await waitForReady();

    await user.click(screen.getByRole('button', { name: '新建' }));
    const dialog = await screen.findByRole('dialog');
    await user.type(within(dialog).getByLabelText(/终端 ID/), 't-new');
    await user.type(within(dialog).getByLabelText(/^名称/), '新终端');
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('POST');
    expect(writeCalls[0]?.body).toMatchObject({ id: 't-new', name: '新终端' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(await screen.findByText('已创建记录，列表已刷新。')).toBeInTheDocument();
  });

  it('删除走二次确认，确认按钮含对象标识，确认后才发 DELETE', async () => {
    const user = userEvent.setup();
    renderApp('/terminals');
    await waitForReady();

    const deleteButtons = screen.getAllByRole('button', { name: '删除' });
    const firstDelete = deleteButtons[0];
    if (!firstDelete) throw new Error('缺少删除按钮');
    await user.click(firstDelete);

    const confirm = await screen.findByRole('alertdialog');
    // 确认按钮本身带对象标识（描述文本里也会有同一句，故按 button 角色取）。
    expect(
      within(confirm).getByRole('button', { name: /删除 终端 1（t-001）/ }),
    ).toBeInTheDocument();
    expect(writeCalls).toHaveLength(0);

    await user.click(within(confirm).getByRole('button', { name: /删除 终端 1（t-001）/ }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('DELETE');
    expect(writeCalls[0]?.path).toBe('terminals/t-001');
  });

  it('required:true 的字段阻止空提交并显示（必填）', async () => {
    const user = userEvent.setup();
    renderApp('/terminals');
    await waitForReady();

    await user.click(screen.getByRole('button', { name: '新建' }));
    const dialog = await screen.findByRole('dialog');
    // 必填提示由 _meta.required 驱动（不是按字段类型猜）：terminals 只有 id 是 required。
    expect(within(dialog).getByLabelText('终端 ID（必填）')).toBeInTheDocument();
    expect(within(dialog).queryByLabelText('名称（必填）')).not.toBeInTheDocument();
    expect(within(dialog).queryByLabelText('网关 ID（必填）')).not.toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    expect(await within(dialog).findByText('终端 ID必填')).toBeInTheDocument();
    expect(
      within(dialog).queryByText('名称必填'),
      '非 required 字段不应被前端拦下',
    ).not.toBeInTheDocument();
    expect(writeCalls).toHaveLength(0);

    await user.type(within(dialog).getByLabelText('终端 ID（必填）'), 't-req');
    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.body).toMatchObject({ id: 't-req', name: null });
  });

  it('写失败就地提示原因，抽屉不关闭', async () => {
    const user = userEvent.setup();
    server.use(
      http.post('/admin/v1/terminals', () =>
        HttpResponse.json({ code: 400, message: 'column "name" too long' }, { status: 400 }),
      ),
    );
    renderApp('/terminals');
    await waitForReady();

    await user.click(screen.getByRole('button', { name: '新建' }));
    const dialog = await screen.findByRole('dialog');
    await user.type(within(dialog).getByLabelText(/终端 ID/), 't-x');
    await user.type(within(dialog).getByLabelText(/^名称/), '写失败终端');
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    expect(await screen.findByText(/保存失败：column "name" too long/)).toBeInTheDocument();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });
});
