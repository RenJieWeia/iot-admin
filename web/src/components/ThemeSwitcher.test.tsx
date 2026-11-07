import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { ensureLocalStorage } from '../test/localStorage';
import { THEME_STORAGE_KEY } from '../lib/theme';

beforeEach(() => {
  ensureLocalStorage();
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
});

afterEach(() => {
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
});

describe('顶栏配色主题切换', () => {
  it('三个按钮同属一个 group，默认深青 aria-pressed=true', async () => {
    renderApp('/terminals');
    const group = await screen.findByRole('group', { name: '配色主题' });
    const buttons = within(group).getAllByRole('button');
    expect(buttons.map((button) => button.textContent)).toEqual(['深青', '电蓝', '石墨']);
    expect(within(group).getByRole('button', { name: '深青' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(group).getByRole('button', { name: '电蓝' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('点「电蓝」立即改 data-theme 并写 localStorage，aria-pressed 跟随', async () => {
    renderApp('/terminals');
    const group = await screen.findByRole('group', { name: '配色主题' });

    // 这里用 fireEvent：按钮在顶栏（测试容器是游离节点），userEvent 的指针模拟
    // 在该结构下不会冒泡到 React 根；fireEvent 直接派发 click，更贴近要断言的语义。
    fireEvent.click(within(group).getByRole('button', { name: '电蓝' }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('blue'));
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('blue');
    expect(within(group).getByRole('button', { name: '电蓝' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(group).getByRole('button', { name: '深青' })).toHaveAttribute('aria-pressed', 'false');

    // 再切石墨：不刷新页面（同一次挂载仍在），只换 dataset。
    fireEvent.click(within(screen.getByRole('group', { name: '配色主题' })).getByRole('button', { name: '石墨' }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('graphite'));
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('graphite');
    expect(screen.getByRole('group', { name: '配色主题' })).toBeInTheDocument();
  });

  it('刷新后按 localStorage 复原（重新挂载读取 dataset/存储）', async () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'blue');
    document.documentElement.dataset.theme = 'blue';
    renderApp('/terminals');
    const group = await screen.findByRole('group', { name: '配色主题' });
    expect(within(group).getByRole('button', { name: '电蓝' })).toHaveAttribute('aria-pressed', 'true');
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('blue');
  });

  it('localStorage 无偏好（被清空）时回落到深青', async () => {
    renderApp('/terminals');
    const group = await screen.findByRole('group', { name: '配色主题' });
    expect(within(group).getByRole('button', { name: '深青' })).toHaveAttribute('aria-pressed', 'true');
  });
});
