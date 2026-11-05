import * as Dialog from '@radix-ui/react-dialog';
import type { ReactNode } from 'react';
import styles from './Panel.module.css';

interface Props {
  open: boolean;
  title: string;
  children: ReactNode;
  /** 底部操作区（取消/保存/删除）。 */
  footer: ReactNode;
  onClose: () => void;
  onOpenChange?: (open: boolean) => void;
  /** Radix Dialog 提供的 aria-describedby 目标；没有说明时显式置空，避免读屏念出无关文本。 */
  description?: string;
}

/**
 * 侧滑抽屉：Radix Dialog 提供 role=dialog / aria-modal / 焦点陷阱 / Esc 关闭 / 关闭后焦点归还。
 * 底部操作区固定，始终可见（admin-ui-spec §5）。
 */
export function Drawer({
  open,
  title,
  children,
  footer,
  onClose,
  onOpenChange,
  description,
}: Props) {
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        onOpenChange?.(next);
        if (!next) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className={styles.overlay} />
        <Dialog.Content
          className={styles.drawer}
          aria-describedby={description ? 'drawer-description' : undefined}
        >
          <div className={styles.drawerHeader}>
            <Dialog.Title className={styles.drawerTitle}>{title}</Dialog.Title>
            <Dialog.Close asChild>
              <button type="button" className="btn" aria-label="关闭">
                ✕
              </button>
            </Dialog.Close>
          </div>
          {description && (
            <p className="visually-hidden" id="drawer-description">
              {description}
            </p>
          )}
          <div className={styles.drawerBody}>{children}</div>
          <div className={styles.drawerFooter}>{footer}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
