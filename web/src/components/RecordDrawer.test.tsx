import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { metaFixture } from '../test/fixtures';
import { RecordDrawer } from './RecordDrawer';

const entity = metaFixture.entities.find((item) => item.name === 'terminals');
if (!entity) throw new Error('fixtures 缺少 terminals');

const baseProps = {
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
