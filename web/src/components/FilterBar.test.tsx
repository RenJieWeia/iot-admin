import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';

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
