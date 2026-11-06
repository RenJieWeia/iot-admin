import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it } from 'vitest';
import { AppShell } from './AppShell';

/**
 * jsdom 没有 matchMedia，而窄屏/桌面是 AppShell 唯一依赖视口的分支，
 * 因此每个用例先给一个只回答 matches 的假实现（admin-ui-spec §3.1）。
 */
function stubViewport(narrow: boolean) {
  const mediaQueryList = {
    matches: narrow,
    media: '(max-width: 1023.98px)',
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

function renderShell() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/terminals']}>
        <AppShell title="终端" subtitle="设备终端">
          <p>内容区</p>
        </AppShell>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppShell 窄屏导航（§3.1 外壳 / §14 触摸目标）', () => {
  it('窄屏：顶栏出现菜单按钮，导航只存在于抽屉内', async () => {
    stubViewport(true);
    const user = userEvent.setup();
    renderShell();

    const menu = screen.getByRole('button', { name: '打开导航菜单' });
    expect(menu).toHaveAttribute('aria-expanded', 'false');
    expect(menu).toHaveAttribute('aria-controls', 'nav-drawer');
    // 侧栏已收起：关闭状态下页面里没有任何主导航。
    expect(screen.queryByRole('navigation', { name: '主导航' })).not.toBeInTheDocument();

    await user.click(menu);
    const dialog = await screen.findByRole('dialog');
    const nav = within(dialog).getByRole('navigation', { name: '主导航' });
    expect(await within(nav).findByRole('link', { name: '终端' })).toBeInTheDocument();
    expect(nav).toHaveAttribute('id', 'nav-drawer');
    expect(menu).toHaveAttribute('aria-expanded', 'true');
  });

  it('窄屏：Esc 关闭抽屉，焦点回到菜单按钮', async () => {
    stubViewport(true);
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole('button', { name: '打开导航菜单' }));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();

    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // 用实时查询取节点：关闭后重渲染可能换掉旧的 DOM 引用。
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '打开导航菜单' })).toHaveFocus();
    });
  });

  it('窄屏：选中导航项后抽屉自动关闭', async () => {
    stubViewport(true);
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole('button', { name: '打开导航菜单' }));
    const dialog = await screen.findByRole('dialog');
    await user.click(await within(dialog).findByRole('link', { name: '终端' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('桌面：没有菜单按钮，导航常驻侧栏', async () => {
    stubViewport(false);
    renderShell();

    expect(screen.queryByRole('button', { name: '打开导航菜单' })).not.toBeInTheDocument();
    const nav = screen.getByRole('navigation', { name: '主导航' });
    expect(await within(nav).findByRole('link', { name: '终端' })).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
