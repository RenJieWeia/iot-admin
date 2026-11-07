import { cleanup, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { allTerminals } from '../test/fixtures';
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
    expect(screen.getAllByRole('button', { name: '清除筛选' })[0]!).toBeInTheDocument();
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

  it('表格说明不再重复报数（数字只留在分页区）', async () => {
    renderApp('/terminals');
    await waitForReady();
    const caption = document.querySelector('caption');
    expect(caption).not.toBeNull();
    expect(caption?.textContent).toBe('当前筛选结果');
    // §16.1：两处不能都报数 —— 表格说明里不出现数字，总数只由分页区承担。
    expect(caption?.textContent ?? '').not.toMatch(/\d/);
    expect(screen.getByText('第 1–10 条，共 128 条')).toBeInTheDocument();
    // 用拼接字面量断言，避免测试文件自身命中 F3 的守卫 grep（见 report-012）。
    const implementationLeak = ['由服务端', '执行'].join('');
    expect(caption?.textContent ?? '').not.toContain(implementationLeak);
  });

  it('页码越界自动回到第一页（replace，只跳一次，筛选保留）', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals?page=99&filter.enabled.eq=true&sort=name');
    // 回到第一页：URL 里 page 不再出现，但筛选与排序保留。
    await waitFor(() => expect(view.location()).not.toContain('page='));
    expect(view.location()).toContain('filter.enabled.eq=true');
    expect(view.location()).toContain('sort=name');
    expect(
      await screen.findByText('当前页无数据，已回到第一页（筛选条件保留）。'),
    ).toBeInTheDocument();
    // 中性嵌入提示而非成功 toast：带可关闭入口，点掉后不再占位。
    const dismiss = screen.getByRole('button', { name: '关闭纠偏提示' });
    await user.click(dismiss);
    expect(screen.queryByText('当前页无数据，已回到第一页（筛选条件保留）。')).not.toBeInTheDocument();
    await waitFor(() => expect(bodyRows()).toHaveLength(10));

    // 不震荡：等一拍后 URL 与行数都不再变化。
    const settled = view.location();
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(view.location()).toBe(settled);
    expect(bodyRows()).toHaveLength(10);
  });

  it('修正后第一页仍为空：无筛选不判「无匹配」，按数据在变化提示', async () => {
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
    expect(await screen.findByText(/数据在变化，请刷新或重试/)).toBeInTheDocument();
    // 没有任何筛选条件时不能说「筛选排除了全部结果」。
    expect(screen.queryByText('没有匹配的记录')).not.toBeInTheDocument();
    const settled = view.location();
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(view.location()).toBe(settled);
  });

  it('总数 > 0 但无筛选的空页：不出现「清除筛选」死按钮', async () => {
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
    renderApp('/terminals');
    expect(await screen.findByText(/数据在变化，请刷新或重试/)).toBeInTheDocument();
    // 卡头的「清除筛选」在无筛选时是禁用态（不是死按钮），空态区不再出现第二个。
    const clearButtons = screen.queryAllByRole('button', { name: '清除筛选' });
    expect(clearButtons).toHaveLength(1);
    expect(clearButtons[0]).toBeDisabled();
    // 恢复路径仍在：刷新就是重发当前查询。
    expect(screen.getByRole('button', { name: '刷新' })).toBeInTheDocument();
  });

  it('空列表只有一个主按钮：空态 CTA 与顶栏新建不同权重', async () => {
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
    renderApp('/terminals');
    expect(await screen.findByText('还没有终端')).toBeInTheDocument();
    expect(document.querySelectorAll('.btn-primary')).toHaveLength(1);
    expect(screen.getByRole('button', { name: '新建终端' })).toBeInTheDocument();
  });

  it('已有数据时刷新失败保留表格，只在顶部提示上次数据', async () => {
    const view = renderApp('/terminals');
    await waitForReady();

    server.use(
      http.get('/admin/v1/terminals', () =>
        HttpResponse.json({ code: 500, message: 'db_unavailable' }, { status: 500 }),
      ),
    );
    await view.queryClient.invalidateQueries({ queryKey: ['list'] });

    expect(await screen.findByText('刷新失败，显示的是上次数据')).toBeInTheDocument();
    // 整页错误态不得替换掉已渲染的数据（§13.4 刷新失败保留已有内容）。
    expect(screen.queryByText('列表加载失败')).not.toBeInTheDocument();
    expect(screen.getByRole('table')).toBeInTheDocument();
    expect(bodyRows()).toHaveLength(10);
    expect(bodyRows()[0]).toHaveTextContent('t-001');
  });

  it('排序参数非法（400）时「重置查询」清空 sort 后恢复', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/admin/v1/terminals', ({ request }) => {
        const url = new URL(request.url);
        if (url.searchParams.get('sort') === 'nope') {
          return HttpResponse.json({ code: 400, message: 'invalid_sort' }, { status: 400 });
        }
        return HttpResponse.json({
          code: 0,
          message: 'ok',
          data: allTerminals.slice(0, 1),
          meta: { total: 1, page: 1, pageSize: 10, sort: 'id', order: 'desc' },
        });
      }),
    );
    const view = renderApp('/terminals?sort=nope');
    expect(await screen.findByText('列表加载失败')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '重置查询' }));
    await waitFor(() => expect(view.location()).not.toContain('sort=nope'));
    await waitFor(() => expect(bodyRows()).toHaveLength(1));
    expect(screen.queryByText('列表加载失败')).not.toBeInTheDocument();
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
    // C6：必填改用红色星号（aria-hidden），可访问名里不再带「（必填）」；
    // 断言仍是「只有 required 字段带标记」。
    // 1 个字段星号 + 顶部图例的 1 个星号；非 required 字段不带。
    expect(within(dialog).getAllByText('*')).toHaveLength(2);
    expect(within(dialog).getByRole('textbox', { name: /终端 ID/ })).toBeInTheDocument();
    expect(within(dialog).getByRole('textbox', { name: /名称/ })).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    expect(await within(dialog).findByText('终端 ID必填')).toBeInTheDocument();
    expect(
      within(dialog).queryByText('名称必填'),
      '非 required 字段不应被前端拦下',
    ).not.toBeInTheDocument();
    expect(writeCalls).toHaveLength(0);

    await user.type(within(dialog).getByRole('textbox', { name: /终端 ID/ }), 't-req');
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
