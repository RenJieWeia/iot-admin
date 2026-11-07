import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { metaFixture } from '../test/fixtures';
import { RecordDrawer } from './RecordDrawer';

const entity = metaFixture.entities.find((item) => item.name === 'terminals');
if (!entity) throw new Error('fixtures 缺少 terminals');

const baseProps = {
  open: true,
  entity,
  writable: true,
  mode: 'new' as const,
  row: null,
  deleting: false,
  error: null,
  onClose: () => {},
  onSave: () => {},
  onDelete: () => {},
};

describe('常驻挂载语义（1107022）', () => {
  it('open=false 时不渲染抽屉内容（常驻挂载 ≠ 常驻渲染，关闭期间不挂表单、不发请求）', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <RecordDrawer {...baseProps} open={false} saving={false} />
      </QueryClientProvider>,
    );
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('名称')).not.toBeInTheDocument();
  });

  it('关闭后再打开会按当次 row 重新播种（不带上一次的草稿）', async () => {
    const user = userEvent.setup();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrap = (ui: React.ReactElement) => (
      <QueryClientProvider client={client}>{ui}</QueryClientProvider>
    );
    const { rerender } = render(wrap(<RecordDrawer {...baseProps} open saving={false} row={null} />));
    await user.type(screen.getByLabelText('名称'), '临时草稿');
    expect(screen.getByLabelText('名称')).toHaveValue('临时草稿');

    // 关闭（组件不卸载）→ 再打开：表单必须回到当次的初值，而不是上次的草稿
    rerender(wrap(<RecordDrawer {...baseProps} open={false} saving={false} row={null} />));
    rerender(wrap(<RecordDrawer {...baseProps} open saving={false} row={null} />));
    await waitFor(() => expect(screen.getByLabelText('名称')).toHaveValue(''));
  });
});

describe('抽屉保存按钮的忙碌态（§5.1）', () => {
  it('保存中保持文案「保存」，只置 disabled + aria-busy，宽度不跳', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    // 抽屉里的关联选择器会取 options，rerender 也要包在同一 provider 下。
    const wrap = (ui: React.ReactElement) => (
      <QueryClientProvider client={client}>{ui}</QueryClientProvider>
    );

    const { rerender } = render(wrap(<RecordDrawer {...baseProps} saving={false} />));
    const idle = screen.getByRole('button', { name: '保存' });
    expect(idle).toBeEnabled();
    expect(idle).toHaveAttribute('aria-busy', 'false');
    expect(idle.className).toContain('touch-hit');

    rerender(wrap(<RecordDrawer {...baseProps} saving />));
    const busy = screen.getByRole('button', { name: '保存' });
    expect(busy).toBeDisabled();
    expect(busy).toHaveAttribute('aria-busy', 'true');
    // 文案不变：不再渲染「保存中…」，按钮宽度不跳。
    expect(busy.textContent).toBe('保存');
    expect(screen.queryByText('保存中…')).not.toBeInTheDocument();
  });
});
