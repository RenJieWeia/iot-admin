import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Toast, type ToastMessage } from './Toast';

describe('Toast 分档通知', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('成功提示 4 秒后自动消失', () => {
    const onDismiss = vi.fn();
    render(<Toast message={{ kind: 'ok', text: '已保存修改，列表已刷新。' }} onDismiss={onDismiss} />);

    act(() => {
      vi.advanceTimersByTime(3_999);
    });
    expect(onDismiss).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('失败提示不自动消失，只能手动关闭', () => {
    const onDismiss = vi.fn();
    const message: ToastMessage = { kind: 'error', text: '删除失败：db_unavailable' };
    render(<Toast message={message} onDismiss={onDismiss} />);

    expect(screen.getByRole('alert')).toHaveTextContent('删除失败');
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    // 失败提示必须留在屏幕上等用户处理（§15）。
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: '关闭提示' }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('播报语义分档：成功 polite，失败打断级 alert', () => {
    const { rerender } = render(
      <Toast message={{ kind: 'ok', text: '已创建记录。' }} onDismiss={() => {}} />,
    );
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite');

    rerender(<Toast message={{ kind: 'error', text: '保存失败' }} onDismiss={() => {}} />);
    expect(screen.getByRole('alert')).toHaveAttribute('aria-live', 'assertive');
    // 同一时刻只留一条播报路径，避免重复朗读。
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('父组件重渲染换掉 onDismiss 引用，也不会重置 4 秒计时', () => {
    const first = vi.fn();
    const second = vi.fn();
    const message: ToastMessage = { kind: 'ok', text: '已保存修改。' };
    const { rerender } = render(<Toast message={message} onDismiss={first} />);

    act(() => {
      vi.advanceTimersByTime(3_000);
    });
    // 父组件重渲染：同一个 message 对象、新的 onDismiss 引用。
    rerender(<Toast message={message} onDismiss={second} />);
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    // 计时从挂载算起满 4 秒就消失，不因重渲染重新计时。
    expect(second).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();
  });

  it('失败提示在多次重渲染后 10 秒仍在，且不调用 onDismiss', () => {
    const first = vi.fn();
    const second = vi.fn();
    const message: ToastMessage = { kind: 'error', text: '删除失败：db_unavailable' };
    const { rerender } = render(<Toast message={message} onDismiss={first} />);
    rerender(<Toast message={message} onDismiss={second} />);

    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(first).not.toHaveBeenCalled();
    expect(second).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toBeInTheDocument();
  });
});
