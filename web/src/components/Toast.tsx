import { useEffect, useRef } from 'react';
import { cn } from '../lib/cn';

export interface ToastMessage {
  kind: 'ok' | 'error';
  text: string;
}

interface Props {
  message: ToastMessage | null;
  onDismiss: () => void;
}

/**
 * 轻提示：不打断、不遮挡，aria-live 播报。
 * 成功 4 秒自动消失；失败必须留在屏幕上等用户处理（§15：失败要给原因与恢复路径，不能消失）。
 */
export function Toast({ message, onDismiss }: Props) {
  // 用 ref 存最新回调：父组件重渲染会换 onDismiss 引用，但它不该成为计时依赖，
  // 否则 4 秒会被反复重置。计时只锚定 message 对象本身（setToast 每次产生新对象）。
  const dismissRef = useRef(onDismiss);
  useEffect(() => {
    dismissRef.current = onDismiss;
  });

  useEffect(() => {
    if (!message || message.kind !== 'ok') return undefined;
    const timer = window.setTimeout(() => dismissRef.current(), 4000);
    return () => window.clearTimeout(timer);
  }, [message]);

  if (!message) {
    return <div role="status" aria-live="polite" className="visually-hidden" />;
  }
  const isError = message.kind === 'error';
  return (
    <div
      role={isError ? 'alert' : 'status'}
      aria-live={isError ? 'assertive' : 'polite'}
      className={cn(
        'toast-box fixed right-6 top-6 flex items-start gap-3 rounded-sm border px-4 py-3',
        isError
          ? 'border-negative-text bg-negative-bg text-negative-text'
          : 'border-positive-text bg-positive-bg text-positive-text',
      )}
    >
      <span>{message.text}</span>
      {isError && (
        <button
          type="button"
          className="shrink-0 cursor-pointer border-0 bg-transparent px-1 leading-none"
          aria-label="关闭提示"
          onClick={onDismiss}
        >
          ✕
        </button>
      )}
    </div>
  );
}
