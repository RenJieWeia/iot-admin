import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { HttpResponse, http } from 'msw';
import { renderApp } from '../test/utils';
import { propertyQueries, server, writeCalls } from '../test/server';

function bodyRows(): HTMLElement[] {
  return Array.from(document.querySelectorAll('tbody tr')) as HTMLElement[];
}

async function waitForRows(expected: number) {
  await waitFor(
    () => {
      expect(bodyRows()).toHaveLength(expected);
    },
    { timeout: 5000 },
  );
}

describe('终端类型详情页（task-016）', () => {
  it('只渲染当前类型的属性：type_id 固定注入请求，地址里改不掉', async () => {
    // 地址里塞一个 filter.type_id，页面不得采信（固定条件只在请求层注入）。
    const view = renderApp('/device-types/1?filter.type_id.eq=2');
    await waitForRows(4);

    // 请求层固定注入 type_id=1，且必须用 relation 字段唯一支持的 in（eq 会被后端 400）。
    expect(propertyQueries.at(-1)).toContain('filter.type_id.in=1');
    expect(propertyQueries.at(-1)).not.toContain('filter.type_id.eq=');
    expect(propertyQueries.at(-1)).not.toContain('filter.type_id.in=2');
    // 类型 2 独有的点位不应出现。
    expect(screen.queryByText('flow')).not.toBeInTheDocument();
    // 固定条件说明存在，但不是可删除的 chip。
    expect(screen.getByText(/固定条件/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /移除筛选/ })).not.toBeInTheDocument();
    // 标题能看出在哪个终端类型下。
    expect(screen.getByRole('heading', { name: '终端类型 · 泵站' })).toBeInTheDocument();
    expect(view.location()).toBe('/device-types/1?filter.type_id.eq=2');
  });

  it('新建属性时 type_id 锁定为当前类型，提交体里带上它', async () => {
    const user = userEvent.setup();
    renderApp('/device-types/1');
    await waitForRows(4);

    await user.click(screen.getByRole('button', { name: '新建属性' }));
    const dialog = await screen.findByRole('dialog');

    const typeInput = within(dialog).getByLabelText(/终端类型/);
    expect(typeInput).toHaveValue('1');
    expect(typeInput).toBeDisabled();
    expect(within(dialog).getByText('由终端类型固定，不可编辑')).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText(/点位编码/), 'voltage');
    await user.type(within(dialog).getByLabelText(/属性名称/), '电压');
    // Radix Select：点触发符 → 点选项（原生 select 的 selectOptions 不再适用）。
    await user.click(within(dialog).getByRole('combobox', { name: /值类型/ }));
    await user.click(await screen.findByRole('option', { name: '数字' }));
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('POST');
    expect(writeCalls[0]?.body).toMatchObject({
      type_id: '1',
      item_code: 'voltage',
      name: '电压',
      value_type: 'number',
    });
  });

  it('面包屑展开到「实体 / 终端类型 / 当前类型」，分组不可点、末项 aria-current，父级带回筛选', async () => {
    const user = userEvent.setup();
    const listURL = '/device-types?sort=name&order=asc&pageSize=20';
    // 列表页的滚动键是「window.location.pathname + 查询串」；MemoryRouter 不会写 window.location，
    // 所以这里按它实际算出的键（'/?sort=…'）预置位置。真实浏览器里该键就是 /device-types?…
    window.sessionStorage.setItem('iot-admin:scroll:/?sort=name&order=asc&pageSize=20', '420');
    // 外壳固定后 window 不再滚动：恢复位置写的是内容区容器（<main>）的 scrollTop，
    // 由 AppShell 在挂载时登记。这里直接断言那个 DOM 节点，不注入假容器
    // （注入了也会被 AppShell 的注册覆盖，反而测不到真实链路）。

    const view = renderApp(listURL);
    const openButtons = await screen.findAllByRole('button', { name: '查看属性' }, { timeout: 5000 });
    await user.click(openButtons[0]!);
    await waitFor(() => expect(view.location()).toBe('/device-types/1'));
    await waitForRows(4);

    // 页面内的「返回列表」按钮已删（1107009 追加：面包屑是唯一页面内返回路径）。
    expect(screen.queryByRole('button', { name: '返回列表' })).not.toBeInTheDocument();
    const crumb = screen.getByRole('navigation', { name: '面包屑' });
    // 分组只是文本，没有落地页，不做成禁用按钮。
    const groupCrumbText = within(crumb).getByText('实体');
    expect(groupCrumbText.tagName).toBe('SPAN');
    expect(within(crumb).queryByRole('link', { name: '实体' })).toBeNull();
    // 末项是当前页：aria-current=page 且不是链接。
    const current = within(crumb).getByText('泵站');
    expect(current).toHaveAttribute('aria-current', 'page');
    expect(current.closest('a')).toBeNull();
    // 中间项「终端类型」可点，且带回**来源 URL**（含筛选参数），用户上下文不丢。
    const parent = within(crumb).getByRole('link', { name: '终端类型' });
    const parentHref = parent.getAttribute('href') ?? '';
    expect(parentHref).toContain('/device-types');
    expect(parentHref).toContain('sort=name');
    expect(parentHref).toContain('pageSize=20');
    await user.click(parent);
    await waitFor(() => expect(view.location()).toBe(listURL));
  });

  it('面包屑父级键盘可达：Tab 聚焦 + Enter 触发返回（仍带筛选）', async () => {
    const user = userEvent.setup();
    const listURL = '/device-types?sort=name&order=asc&pageSize=20';
    const view = renderApp(listURL);
    const openButtons = await screen.findAllByRole('button', { name: '查看属性' }, { timeout: 5000 });
    await user.click(openButtons[0]!);
    await waitFor(() => expect(view.location()).toBe('/device-types/1'));
    await waitForRows(4);

    // 面包屑父级是真链接（不是 onClick 伪装），所以能被键盘聚焦、也能用 Enter 激活。
    const crumb = screen.getByRole('navigation', { name: '面包屑' });
    const parent = within(crumb).getByRole('link', { name: '终端类型' });
    expect(parent.tagName).toBe('A');
    parent.focus();
    expect(document.activeElement).toBe(parent);

    await user.keyboard('{Enter}');
    await waitFor(() => expect(view.location()).toBe(listURL));
  });

  it('深链直接打开详情（无 location.state）：父级回落到 /device-types', async () => {
    renderApp('/device-types/1');
    await waitForRows(4);
    const crumb = screen.getByRole('navigation', { name: '面包屑' });
    const parent = within(crumb).getByRole('link', { name: '终端类型' });
    expect(parent.getAttribute('href')).toBe('/device-types');
  });

  it('列表页挂载时按自己的键恢复内容区滚动位置（外壳固定后的回归）', async () => {
    // 外壳固定后 window 不再滚动：恢复写的是 <main>.scrollTop，由 AppShell 挂载时登记。
    window.sessionStorage.setItem('iot-admin:scroll:/?sort=name&order=asc&pageSize=20', '420');
    renderApp('/device-types?sort=name&order=asc&pageSize=20');
    await waitFor(() => expect(document.getElementById('main')?.scrollTop).toBe(420), {
      timeout: 5000,
    });
  });

  it('顶部摘要可编辑：编辑入口改的是终端类型本身，不是属性', async () => {
    const user = userEvent.setup();
    renderApp('/device-types/1');
    await waitForRows(4);

    // 摘要区把该类型的关键字段只读展示出来。
    const summary = screen.getByRole('region', { name: '终端类型摘要' });
    expect(within(summary).getByText('泵站')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '编辑基本信息' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('编辑终端类型')).toBeInTheDocument();
    const name = within(dialog).getByLabelText(/名称/);
    await user.clear(name);
    await user.type(name, '泵站-改');
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('PUT');
    expect(writeCalls[0]?.path).toBe('device-types/1');
    expect(writeCalls[0]?.body).toMatchObject({ name: '泵站-改' });
  });

  it('类型不存在：给明确错误态，不落到「还没有属性」也不暴露裸 invalid_query', async () => {
    renderApp('/device-types/999');
    expect(await screen.findByText('终端类型不存在或加载失败')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '返回列表' })).toBeInTheDocument();
    expect(screen.queryByText('该终端类型下还没有属性')).not.toBeInTheDocument();
    expect(screen.queryByText(/invalid_query/)).not.toBeInTheDocument();
  });

  it('导航只来自 _meta.entities：终端属性不进导航，但详情页仍用其字段元数据', async () => {
    renderApp('/device-types');
    // 等元数据就绪：导航里的实体链接出现后，再看是否混入了子资源。
    await screen.findByRole('link', { name: '终端类型' }, { timeout: 5000 });
    const nav = screen.getByRole('navigation', { name: '主导航' });
    expect(within(nav).queryByRole('link', { name: '终端属性' })).not.toBeInTheDocument();
  });

  it('分页越界（total>0 但当前页无行）：给「当前页暂时没有数据」，不是「还没有属性」', async () => {
    server.use(
      http.get('/admin/v1/thing-model-properties', () =>
        HttpResponse.json({
          code: 0,
          message: 'ok',
          data: [],
          meta: { total: 8, page: 3, pageSize: 10, sort: 'id', order: 'desc' },
        }),
      ),
    );
    const view = renderApp('/device-types/1?page=3');

    expect(await screen.findByText('当前页暂时没有数据')).toBeInTheDocument();
    // 关键回归：越界不等于「这个类型没有属性」。
    expect(screen.queryByText('该终端类型下还没有属性')).not.toBeInTheDocument();
    // 纠偏：replace 回第一页，只纠偏一次，提示是中性嵌入条。
    await waitFor(() => expect(view.location()).toBe('/device-types/1'));
    expect(screen.getByText(/已回到第一页（筛选条件保留）/)).toBeInTheDocument();
  });

  it('旧深链 /ui/thing-model-properties 重定向到终端类型列表，并给中性提示', async () => {
    const view = renderApp('/thing-model-properties');
    await waitFor(() => expect(view.location()).toBe('/device-types'));
    expect(await screen.findByText(/终端属性已并入终端类型详情页/)).toBeInTheDocument();
    // 重定向后仍在正常页面上，不是「未知实体」错误页。
    expect(screen.queryByText('未知实体')).not.toBeInTheDocument();
  });
});
