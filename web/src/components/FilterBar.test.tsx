import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { server } from '../test/server';

/** jsdom 没有 matchMedia：窄屏用例用假实现模拟视口（与 AppShell.test 同一手法）。 */
function stubViewport(narrow: boolean) {
  const mediaQueryList = {
    matches: narrow,
    media: '(max-width: 1279.98px)',
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  };
  window.matchMedia = (() => mediaQueryList) as unknown as typeof window.matchMedia;
}

afterEach(() => {
  delete (window as unknown as { matchMedia?: unknown }).matchMedia;
});

/** 等筛选控件就绪：字段元数据渲染出常驻控件（列表请求随后返回）。 */
async function waitForFilters() {
  await screen.findByLabelText('名称', {}, { timeout: 5000 });
}

const nameInput = () => screen.getByLabelText('名称') as HTMLInputElement;

describe('筛选控件以 QueryState 为单一事实源', () => {
  it('字段级文本筛选：输入 300ms 后写 URL，回车立即写', async () => {
    const view = renderApp('/terminals');
    await waitForFilters();

    fireEvent.change(nameInput(), { target: { value: '泵' } });
    // 防抖窗口内不写 URL：旧实现的「只有回车/失焦才提交」会一直等到失焦。
    expect(view.location()).not.toContain('filter.name');
    await waitFor(() => expect(decodeURIComponent(view.location())).toContain('filter.name.contains=泵'), {
      timeout: 2000,
    });

    fireEvent.change(nameInput(), { target: { value: '水泵' } });
    fireEvent.keyDown(nameInput(), { key: 'Enter' });
    // 回车不等防抖：事件处理里同步写 URL。
    expect(decodeURIComponent(view.location())).toContain('filter.name.contains=水泵');
  });

  it('URL 带条件时控件回显该值（受控化的回归护栏）', async () => {
    const view = renderApp('/terminals?filter.name.contains=%E6%B3%B5&filter.enabled.eq=true');
    await waitForFilters();

    expect(nameInput().value).toBe('泵');
    // Radix Select 的触发符是 button[role=combobox]，没有 .value：断言它显示的当前项文案，
    // 语义等价（true 对应的选项文案是「是」，与旧原生 select 显示的完全一致）。
    expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('是');
    expect(decodeURIComponent(view.location())).toContain('filter.name.contains=泵');
  });

  it('删标签后控件同步清空，URL 不再带该条件', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals?filter.name.contains=泵');
    await waitForFilters();
    expect(nameInput().value).toBe('泵');

    await user.click(screen.getByRole('button', { name: /移除筛选 名称 包含 泵/ }));
    await waitFor(() => expect(nameInput().value).toBe(''));
    expect(view.location()).not.toContain('filter.name');
  });

  it('清除全部筛选后聚焦再失焦，不会把旧值写回 URL', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals?filter.name.contains=泵');
    await waitForFilters();

    await user.click(screen.getByRole('button', { name: '清除全部筛选' }));
    await waitFor(() => expect(nameInput().value).toBe(''));
    expect(view.location()).not.toContain('filter.name');

    nameInput().focus();
    await user.tab();
    expect(view.location()).not.toContain('filter.name');
  });

  it('关键词与字段级筛选共用防抖：输入 300ms 后 q 落 URL', async () => {
    const view = renderApp('/terminals');
    await waitForFilters();
    const keyword = screen.getByLabelText('关键词') as HTMLInputElement;

    fireEvent.change(keyword, { target: { value: '终端' } });
    expect(view.location()).not.toContain('q=');
    await waitFor(() => expect(decodeURIComponent(view.location())).toContain('q=终端'), {
      timeout: 2000,
    });
  });
});

describe('筛选控件局部健壮性与呈现', () => {
  it('数值范围 min > max：保留输入、给字段级提示、不发请求', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals');
    await waitForFilters();

    await user.click(screen.getByRole('button', { name: '更多筛选' }));
    const dialog = await screen.findByRole('dialog');
    const gte = within(dialog).getByLabelText('小数位 下限');
    const lte = within(dialog).getByLabelText('小数位 上限');

    fireEvent.change(gte, { target: { value: '10' } });
    fireEvent.change(lte, { target: { value: '1' } });
    fireEvent.blur(lte);

    expect(await within(dialog).findByText(/最小值不能大于最大值/)).toBeInTheDocument();
    // 输入保留，不静默清空（§16.4：有原因也要有恢复路径）。
    expect(gte).toHaveValue(10);
    expect(lte).toHaveValue(1);
    expect(view.location()).not.toContain('filter.precision');

    // 改回合法值后提示消失，条件正常提交。
    fireEvent.change(lte, { target: { value: '20' } });
    fireEvent.blur(lte);
    await waitFor(() => expect(view.location()).toContain('filter.precision.gte=10'));
    expect(within(dialog).queryByText(/最小值不能大于最大值/)).not.toBeInTheDocument();
  });

  it('日期范围用日期控件并声明时区，起止倒置时不提交', async () => {
    renderApp('/readonly/latest-values');
    const gte = await screen.findByLabelText('取值时间 下限', {}, { timeout: 5000 });

    expect(gte).toHaveAttribute('type', 'date');
    expect(screen.getByText('取值时间（范围，+08:00）')).toBeInTheDocument();

    fireEvent.change(gte, { target: { value: '2026-11-05' } });
    fireEvent.change(screen.getByLabelText('取值时间 上限'), { target: { value: '2026-11-01' } });
    fireEvent.blur(screen.getByLabelText('取值时间 上限'));
    expect(await screen.findByText(/开始日期不能晚于结束日期/)).toBeInTheDocument();
  });

  it('选项加载失败时在控件下局部提示并可重试，表格不受影响', async () => {
    const user = userEvent.setup();
    server.use(
      http.get('/admin/v1/options/:entity', () =>
        HttpResponse.json({ code: 500, message: 'options_failed' }, { status: 500 }),
      ),
    );
    renderApp('/terminals');
    await waitForFilters();

    // 表格主体不被选项失败阻塞。
    await waitFor(() => expect(document.querySelectorAll('tbody tr')).toHaveLength(10));

    await user.click(screen.getByRole('button', { name: '更多筛选' }));
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText(/选项加载失败/)).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '重试' })).toBeInTheDocument();
  });
});


/** 打开「更多筛选」并展开终端类型组合框（弹层内容通过 Portal 渲染到 body）。 */
async function openTypeCombobox(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: '更多筛选' }));
  const dialog = await screen.findByRole('dialog');
  const trigger = within(dialog).getByRole('combobox', { name: '终端类型' });
  await user.click(trigger);
  const input = await screen.findByRole('combobox', { name: '搜索终端类型选项' });
  return { dialog, trigger, input };
}

describe('多选筛选：可搜索组合框（§5.1/§14.1）', () => {
  it('搜到选项后用 Enter 切换选中/取消，已选计数与 URL 同步', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals');
    await waitForFilters();

    const { dialog, input } = await openTypeCombobox(user);
    // 打开即聚焦搜索框：可以直接键入，不必先 Tab
    expect(document.activeElement).toBe(input);

    await user.type(input, '电');
    expect(await screen.findByRole('option', { name: '电表' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: '泵站' })).not.toBeInTheDocument();

    // Enter 切中选择，弹层不关（多选要连续操作）
    await user.keyboard('{Enter}');
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
    expect(screen.getByRole('listbox', { name: '终端类型选项' })).toBeInTheDocument();
    await waitFor(() => expect(view.location()).toContain('filter.type_id.in=3'));

    // 再按一次 Enter 取消该项
    await user.keyboard('{Enter}');
    await waitFor(() => expect(screen.queryByText('已选 1 项')).not.toBeInTheDocument());
    expect(view.location()).not.toContain('filter.type_id');
    expect(dialog).toBeInTheDocument();
  });

  it('Esc 只关组合框、把焦点还给触发按钮，外层浮层不跟着关', async () => {
    const user = userEvent.setup();
    renderApp('/terminals');
    await waitForFilters();
    const { dialog, trigger } = await openTypeCombobox(user);

    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.queryByRole('listbox', { name: '终端类型选项' })).not.toBeInTheDocument(),
    );
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(screen.getByRole('dialog')).toBe(dialog);
  });

  it('已选项可单项移除，也可一键清空', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals?filter.type_id.in=1&filter.type_id.in=2');
    await waitForFilters();
    await openTypeCombobox(user);

    expect(screen.getByText('已选 2 项')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '移除终端类型 泵站' }));
    await waitFor(() => expect(screen.getByText('已选 1 项')).toBeInTheDocument());
    expect(decodeURIComponent(view.location())).toContain('filter.type_id.in=2');

    await user.click(screen.getByRole('button', { name: '清空' }));
    await waitFor(() => expect(screen.queryByText(/已选 \d+ 项/)).not.toBeInTheDocument());
    expect(view.location()).not.toContain('filter.type_id');
  });
});

describe('窄屏筛选抽屉（§14.1/§11.2）', () => {
  it('窄屏只留「筛选（N）」，抽屉内改条件后计数同步，已选标签仍在页面', async () => {
    stubViewport(true);
    const user = userEvent.setup();
    const view = renderApp('/terminals');

    const trigger = await screen.findByRole('button', { name: '筛选（0）' }, { timeout: 5000 });
    // 窄屏不渲染常驻筛选区
    expect(screen.queryByLabelText('名称')).not.toBeInTheDocument();

    await user.click(trigger);
    const panel = await screen.findByRole('dialog');
    const nameInput = within(panel).getByLabelText('名称');
    fireEvent.change(nameInput, { target: { value: '泵' } });
    fireEvent.keyDown(nameInput, { key: 'Enter' });
    await waitFor(() =>
      expect(decodeURIComponent(view.location())).toContain('filter.name.contains=泵'),
    );

    // 修改即生效：手动点「完成」才收起
    await user.click(within(panel).getByRole('button', { name: '完成' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    expect(screen.getByRole('button', { name: '筛选（1）' })).toBeInTheDocument();
    // 已生效条件标签常驻页面，不藏进抽屉
    const chips = screen.getByRole('list', { name: '已生效筛选条件' });
    expect(within(chips).getByText('泵')).toBeInTheDocument();
  });

  it('计数口径：关键词算 1 条，每个筛选条件算 1 条', async () => {
    stubViewport(true);
    renderApp('/terminals?q=%E7%BB%88%E7%AB%AF&filter.name.contains=%E6%B3%B5');
    expect(await screen.findByRole('button', { name: '筛选（2）' }, { timeout: 5000 })).toBeInTheDocument();
  });
});

describe('「更多筛选」浮层底部动作（§5.4）', () => {
  it('取消只关闭浮层，不撤销已生效条件', async () => {
    const user = userEvent.setup();
    const view = renderApp('/terminals');
    await waitForFilters();

    await user.click(screen.getByRole('button', { name: '更多筛选' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('修改即生效')).toBeInTheDocument();

    const gte = within(dialog).getByLabelText('小数位 下限');
    fireEvent.change(gte, { target: { value: '3' } });
    fireEvent.blur(gte);
    await waitFor(() => expect(view.location()).toContain('filter.precision.gte=3'));

    await user.click(within(dialog).getByRole('button', { name: '取消' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // 取消不撤销：条件在点「取消」前就已生效
    expect(view.location()).toContain('filter.precision.gte=3');
  });
});
