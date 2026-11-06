import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CursorPager } from './CursorPager';

const baseProps = { hasMore: true, limit: 20, loaded: 20, onMore: () => {} };

describe('游标分页器（table-query §6 / §5.1 / §14.1）', () => {
  it('显式声明不可跳页：只能继续加载，不能回到指定页', () => {
    render(<CursorPager {...baseProps} loading={false} />);
    expect(screen.getByText(/只能继续加载，不能回到指定页/)).toBeInTheDocument();
    expect(screen.getByText(/游标分页，不显示总数/)).toBeInTheDocument();
  });

  it('加载中保持文案「加载更多」，只置 disabled + aria-busy，宽度不跳', () => {
    const { rerender } = render(<CursorPager {...baseProps} loading={false} />);
    const idle = screen.getByRole('button', { name: '加载更多' });
    expect(idle).toBeEnabled();
    expect(idle).toHaveAttribute('aria-busy', 'false');
    expect(idle.className).toContain('touch-hit');

    rerender(<CursorPager {...baseProps} loading />);
    const busy = screen.getByRole('button', { name: '加载更多' });
    expect(busy).toBeDisabled();
    expect(busy).toHaveAttribute('aria-busy', 'true');
    // 文案不变：不再出现「加载中…」，按钮宽度不会因文案变短而跳动。
    expect(busy.textContent).toBe('加载更多');
    expect(screen.queryByText('加载中…')).not.toBeInTheDocument();
  });
});
