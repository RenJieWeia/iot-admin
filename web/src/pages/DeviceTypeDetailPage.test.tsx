import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { renderApp } from '../test/utils';
import { propertyQueries, writeCalls } from '../test/server';

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

    // 请求层固定注入 type_id=1。
    expect(propertyQueries.at(-1)).toContain('filter.type_id.eq=1');
    expect(propertyQueries.at(-1)).not.toContain('filter.type_id.eq=2');
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
    await user.selectOptions(within(dialog).getByLabelText(/值类型/), 'number');
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

  it('返回列表：history.back 恢复原 URL（筛选/排序/分页），并按列表页的键恢复滚动位置', async () => {
    const user = userEvent.setup();
    const scrollTo = vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
    const listURL = '/device-types?sort=name&order=asc&pageSize=20';
    // 列表页的滚动键是「window.location.pathname + 查询串」；MemoryRouter 不会写 window.location，
    // 所以这里按它实际算出的键（'/?sort=…'）预置位置。真实浏览器里该键就是 /device-types?…
    window.sessionStorage.setItem('iot-admin:scroll:/?sort=name&order=asc&pageSize=20', '420');
    Object.defineProperty(window, 'scrollY', { value: 420, configurable: true, writable: true });

    const view = renderApp(listURL);
    const openButtons = await screen.findAllByRole('button', { name: '查看属性' }, { timeout: 5000 });
    await user.click(openButtons[0]!);
    await waitFor(() => expect(view.location()).toBe('/device-types/1'));
    await waitForRows(4);

    await user.click(screen.getByRole('button', { name: '返回列表' }));
    await waitFor(() => expect(view.location()).toBe(listURL));
    // 列表页按自己的键恢复滚动位置（详情页用 /device-types/:id 自己的键，互不覆盖）。
    await waitFor(() => expect(scrollTo).toHaveBeenCalledWith(0, 420), { timeout: 5000 });

    scrollTo.mockRestore();
    Object.defineProperty(window, 'scrollY', { value: 0, configurable: true, writable: true });
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

  it('导航只来自 _meta.entities：终端属性不进导航，但详情页仍用其字段元数据', async () => {
    renderApp('/device-types');
    // 等元数据就绪：导航里的实体链接出现后，再看是否混入了子资源。
    await screen.findByRole('link', { name: '终端类型' }, { timeout: 5000 });
    const nav = screen.getByRole('navigation', { name: '主导航' });
    expect(within(nav).queryByRole('link', { name: '终端属性' })).not.toBeInTheDocument();
  });
});
