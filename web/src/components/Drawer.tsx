import * as Dialog from '@radix-ui/react-dialog';
import type { ReactNode } from 'react';
import { cn } from '../lib/cn';

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
export function Drawer({ open, title, children, footer, onClose, onOpenChange, description }: Props) {
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        onOpenChange?.(next);
        if (!next) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-overlay" />
        <Dialog.Content
          className={cn(
            'drawer-panel fixed inset-y-0 right-0 flex flex-col border-l border-border bg-surface',
          )}
          aria-describedby={description ? 'drawer-description' : undefined}
        >
          <div className="flex items-center justify-between gap-3 border-b border-border p-4">
            <Dialog.Title className="m-0 text-lg">{title}</Dialog.Title>
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
          <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">{children}</div>
          <div className="sticky bottom-0 flex items-center justify-end gap-2 border-t border-border bg-surface px-4 py-3">
            {footer}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
