import { useEffect } from 'react';
import { cn } from '../lib/cn';

export interface ToastMessage {
  kind: 'ok' | 'error';
  text: string;
}

interface Props {
  message: ToastMessage | null;
  onDismiss: () => void;
}

/** 轻提示：不打断、不遮挡，aria-live 播报，4 秒后自动消失（§15 通知）。 */
export function Toast({ message, onDismiss }: Props) {
  useEffect(() => {
    if (!message) return undefined;
    const timer = window.setTimeout(onDismiss, 4000);
    return () => window.clearTimeout(timer);
  }, [message, onDismiss]);

  if (!message) {
    return <div role="status" aria-live="polite" className="visually-hidden" />;
  }
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        'toast-box fixed right-6 top-6 rounded-sm border px-4 py-3',
        message.kind === 'ok'
          ? 'border-positive-text bg-positive-bg text-positive-text'
          : 'border-negative-text bg-negative-bg text-negative-text',
      )}
    >
      {message.text}
    </div>
  );
}
