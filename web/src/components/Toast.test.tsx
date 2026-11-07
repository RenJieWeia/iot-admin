import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Toast, type ToastMessage } from './Toast';

/** 退场时长（与组件里的 EXIT_MS 对齐，用例按这个数推进假时钟）。 */
const EXIT_MS = 160;

/**
 * jsdom 没有 matchMedia。动效分支只问一件事：是否 prefers-reduced-motion，
 * 因此给一个只回答该查询的假实现（与 AppShell.test 同一手法）。
 */
function stubReducedMotion(reduce: boolean) {
  const mediaQueryList = {
    matches: reduce,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  };
  window.matchMedia = (() => mediaQueryList) as unknown as typeof window.matchMedia;
}

function clearMatchMedia() {
  delete (window as unknown as { matchMedia?: unknown }).matchMedia;
}

describe('Toast 分档通知', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    clearMatchMedia();
  });

  it('成功提示 4 秒后开始退场，退场播完才卸载并通知父组件', () => {
    const onDismiss = vi.fn();
    render(<Toast message={{ kind: 'ok', text: '已保存修改，列表已刷新。' }} onDismiss={onDismiss} />);

    act(() => {
      vi.advanceTimersByTime(3_999);
    });
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getByRole('status')).toBeInTheDocument();

    // 到点：进退场态，但**不是**瞬间移除。
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getByRole('status')).toHaveAttribute('data-state', 'leaving');

    // 退场播完（160ms）才卸载并通知。
    act(() => {
      vi.advanceTimersByTime(EXIT_MS);
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
    // 卸载的是「可见提示」；空态仍保留一个 visually-hidden 的 aria-live 占位（既有行为）。
    expect(document.querySelector('.toast-box')).toBeNull();
    expect(screen.queryByRole('status')).toHaveAttribute('class', 'visually-hidden');
  });

  it('失败提示不自动消失，只能手动关闭（关闭也走退场）', () => {
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
    // 点 ✕ 后先播退场：元素还在，只是进了 leaving 态。
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toHaveAttribute('data-state', 'leaving');

    act(() => {
      vi.advanceTimersByTime(EXIT_MS);
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
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
    // 计时从挂载算起满 4 秒就进退场，不因重渲染重新计时。
    expect(screen.getByRole('status')).toHaveAttribute('data-state', 'leaving');
    expect(second).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(EXIT_MS);
    });
    // 退场结束只通知最新的那个回调。
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

describe('Toast 动效（1107022）', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    clearMatchMedia();
  });

  it('入场：data-state=open，播 toast-in，时长 ≤200ms 且只动 opacity/transform', () => {
    render(<Toast message={{ kind: 'ok', text: '已保存。' }} onDismiss={() => {}} />);
    const toast = screen.getByRole('status');

    expect(toast).toHaveAttribute('data-state', 'open');
    const animation = toast.style.animation;
    expect(animation).toContain('toast-in');
    // 令牌未落地时用 200ms 兜底，落地后取 --dur-base：两种写法都 ≤200ms。
    expect(animation).toMatch(/var\(--dur-base, 200ms\)|var\(--dur-base\)/);
    // 退场时长是硬编码的 160ms，这里只守「入场 != 退场」。
    expect(animation).not.toContain('toast-out');

    // 关键帧只允许动 transform / opacity（不触发重排、不引入颜色与尺寸）。
    const styleTag = document.head.querySelector('style[data-toast-motion]');
    expect(styleTag).not.toBeNull();
    const keyframes = styleTag?.textContent ?? '';
    expect(keyframes).toContain('@keyframes toast-in');
    expect(keyframes).toContain('@keyframes toast-out');
    const animatedProperties = [...keyframes.matchAll(/([a-z-]+):/g)]
      .map((match) => match[1])
      .filter((prop) => prop && !prop.startsWith('--'));
    expect(new Set(animatedProperties)).toEqual(new Set(['opacity', 'transform']));
  });

  it('退场：159ms 时仍在屏且状态为 leaving，160ms 才卸载', () => {
    const onDismiss = vi.fn();
    render(<Toast message={{ kind: 'error', text: '删除失败：db_unavailable' }} onDismiss={onDismiss} />);

    fireEvent.click(screen.getByRole('button', { name: '关闭提示' }));
    const toast = screen.getByRole('alert');
    expect(toast).toHaveAttribute('data-state', 'leaving');
    expect(toast.style.animation).toBe('toast-out 160ms ease-in forwards');

    act(() => {
      vi.advanceTimersByTime(EXIT_MS - 1);
    });
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(onDismiss).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('父组件直接清空 message 时同样走退场，而不是瞬间消失', () => {
    const onDismiss = vi.fn();
    const { rerender } = render(<Toast message={{ kind: 'ok', text: '已保存。' }} onDismiss={onDismiss} />);
    expect(screen.getByRole('status')).toHaveAttribute('data-state', 'open');

    rerender(<Toast message={null} onDismiss={onDismiss} />);
    expect(screen.getByRole('status')).toHaveAttribute('data-state', 'leaving');

    act(() => {
      vi.advanceTimersByTime(EXIT_MS);
    });
    // 外部清空不是「用户关闭」，因此不再回调 onDismiss（避免异步循环）。
    expect(document.querySelector('.toast-box')).toBeNull();
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('reduced-motion：不注入动画，直接出现、直接卸载', () => {
    stubReducedMotion(true);
    const onDismiss = vi.fn();
    render(<Toast message={{ kind: 'error', text: '删除失败：db_unavailable' }} onDismiss={onDismiss} />);

    // 入场：无动画样式，但照常渲染。
    const toast = screen.getByRole('alert');
    expect(toast.style.animation).toBe('');

    fireEvent.click(screen.getByRole('button', { name: '关闭提示' }));
    // 不播退场：一步到位卸载并通知。
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('reduced-motion：成功提示到点时也直接卸载，不占用 160ms 退场窗口', () => {
    stubReducedMotion(true);
    const onDismiss = vi.fn();
    render(<Toast message={{ kind: 'ok', text: '已保存。' }} onDismiss={onDismiss} />);

    act(() => {
      vi.advanceTimersByTime(4_000);
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(document.querySelector('.toast-box')).toBeNull();
  });
});
