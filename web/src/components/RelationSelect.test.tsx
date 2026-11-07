import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { optionQueries, writeCalls } from '../test/server';

function bodyRows(): HTMLElement[] {
  return Array.from(document.querySelectorAll('tbody tr')) as HTMLElement[];
}

async function openDrawer() {
  await waitFor(
    () => {
      expect(screen.queryByText('正在加载列表…')).not.toBeInTheDocument();
      expect(bodyRows()).toHaveLength(10);
    },
    { timeout: 5000 },
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: '新建' }));
  return { user, dialog: await screen.findByRole('dialog') };
}

/** 打开关系字段的 combobox 弹层。 */
async function openCombobox(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement) {
  const trigger = within(dialog).getByRole('combobox', { name: '终端类型' });
  await user.click(trigger);
  const listbox = await screen.findByRole('listbox', { name: '终端类型选项' });
  return { trigger, listbox };
}

describe('关系字段远程搜索下拉（F4）', () => {
  it('打开后聚焦输入框，列出选项；输入 300ms 防抖后只剩匹配项', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    const { listbox } = await openCombobox(user, dialog);

    // 触发按钮（role=combobox）在抽屉内，弹层输入框通过 Portal 渲染到 body。
    const input = screen.getByPlaceholderText('输入关键词搜索');
    expect(document.activeElement).toBe(input);
    expect(within(listbox).getAllByRole('option')).toHaveLength(4);

    const queriesBefore = optionQueries.length;
    await user.type(input, '电');

    // 先等"请求已发出"这个确定性信号，再断言渲染——不要一开始就等 DOM 文本：
    // 300ms 防抖走的是真实计时器（本仓不全局用 fake timers，会牵连其它用例），
    // 24 个测试文件并行时机器一忙，等 DOM 的 waitFor 就会撞线（历史抖动三次）。
    await waitFor(() => expect(optionQueries.length).toBe(queriesBefore + 1), { timeout: 5000 });
    expect(optionQueries.at(-1)).toContain('q=%E7%94%B5');

    // 请求已确认，这里只是等一次渲染提交；同样给足超时，避免并行时的假阴性。
    await waitFor(() => expect(within(listbox).getAllByRole('option')).toHaveLength(1), {
      timeout: 5000,
    });
    expect(within(listbox).getByRole('option', { name: '电表' })).toBeInTheDocument();
  });

  it('连续输入只发最后一次查询（防抖 + 只应用最新响应）', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    const { listbox } = await openCombobox(user, dialog);
    const input = screen.getByPlaceholderText('输入关键词搜索');

    const before = optionQueries.length;
    await user.type(input, '水表');
    await waitFor(() => expect(within(listbox).getAllByRole('option')).toHaveLength(1), {
      timeout: 2000,
    });
    // 两次输入（水、水表）在 300ms 内 → 只发出最后一次 q
    expect(optionQueries.length - before).toBeLessThanOrEqual(1);
    expect(within(listbox).getByRole('option', { name: '水表' })).toBeInTheDocument();
  });

  it('↑↓ 移动 + Enter 选中，表单值随之提交', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    const { listbox, trigger } = await openCombobox(user, dialog);

    // 初始高亮第 1 项（泵站），↓ 到第 2 项（阀门）。
    await user.keyboard('{ArrowDown}');
    expect(within(listbox).getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'false');
    await user.keyboard('{Enter}');

    await waitFor(() =>
      expect(screen.queryByRole('listbox', { name: '终端类型选项' })).not.toBeInTheDocument(),
    );
    expect(trigger.textContent).toContain('阀门');

    // 关闭后焦点回到触发按钮
    await waitFor(() => expect(document.activeElement).toBe(trigger));

    // 提交：表单里 type_id 为 '2'
    await user.type(within(dialog).getByLabelText(/终端 ID/), 't-f4');
    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.body).toMatchObject({ id: 't-f4', type_id: '2' });
  });

  it('Esc 关闭弹层并把焦点还给触发按钮', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    const { trigger } = await openCombobox(user, dialog);

    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByRole('listbox', { name: '终端类型选项' })).not.toBeInTheDocument(),
    );
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it('无匹配时给出空态文案，可换关键词', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    const { listbox } = await openCombobox(user, dialog);
    const input = screen.getByPlaceholderText('输入关键词搜索');

    await user.type(input, 'zzz');
    expect(
      await within(listbox).findByText('没有匹配的选项，换个关键词'),
    ).toBeInTheDocument();
  });

  it('已选值可清除，回到「未设置」', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    const { trigger } = await openCombobox(user, dialog);
    await user.keyboard('{Enter}'); // 选第一项（泵站）
    await waitFor(() => expect(trigger.textContent).toContain('泵站'));

    await user.click(within(dialog).getByRole('button', { name: '清除终端类型' }));
    await waitFor(() => expect(trigger.textContent).toContain('未设置'));
  });
});

describe('关系字段组合框可访问性（§17）', () => {
  it('触发按钮关闭态 aria-expanded=false（护栏）', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();

    const trigger = within(dialog).getByRole('combobox', { name: '终端类型' });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');

    await user.click(trigger);
    await screen.findByRole('listbox', { name: '终端类型选项' });
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
  });

  it('弹层搜索框有可区分名称，并反映真实展开态', async () => {
    renderApp('/terminals');
    const { user, dialog } = await openDrawer();
    await openCombobox(user, dialog);

    // 同屏两个 combobox：触发按钮叫「终端类型」，搜索框必须自带字段名。
    const search = screen.getByRole('combobox', { name: '搜索终端类型选项' });
    expect(search).toHaveAttribute('aria-expanded', 'true');
    expect(search).toHaveAttribute('aria-controls');
    expect(document.activeElement).toBe(search);
  });
});
