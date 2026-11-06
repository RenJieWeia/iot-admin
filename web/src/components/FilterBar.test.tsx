import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { server } from '../test/server';

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
    expect((screen.getByLabelText('状态') as HTMLSelectElement).value).toBe('true');
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
