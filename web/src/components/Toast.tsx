import { useEffect } from 'react';
import styles from './Panel.module.css';

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
  const tone = message.kind === 'ok' ? styles.toastOk : styles.toastError;
  return (
    <div role="status" aria-live="polite" className={`${styles.toast} ${tone}`}>
      {message.text}
    </div>
  );
}
