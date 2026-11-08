import * as AlertDialog from '@radix-ui/react-alert-dialog';
import type { ReactNode } from 'react';
import { cn } from '../lib/cn';

export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  loading?: boolean;
  onConfirm: () => void;
}

/** 破坏性操作的二次确认；确认按钮不自动关闭，交给调用方在成功后关闭。 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = '确认',
  cancelLabel = '取消',
  danger = true,
  loading = false,
  onConfirm,
}: ConfirmDialogProps) {
  return (
    <AlertDialog.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="fixed inset-0 z-[60] bg-black/40" />
        <AlertDialog.Content className="fixed left-1/2 top-1/2 z-[70] w-[calc(100vw-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-line bg-surface p-5 shadow-panel outline-none">
          <AlertDialog.Title className="text-base font-semibold text-fg">{title}</AlertDialog.Title>
          {description && (
            <AlertDialog.Description className="mt-2 text-sm leading-6 text-muted">
              {description}
            </AlertDialog.Description>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <button
                type="button"
                disabled={loading}
                className="h-9 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
              >
                {cancelLabel}
              </button>
            </AlertDialog.Cancel>
            <button
              type="button"
              disabled={loading}
              onClick={onConfirm}
              className={cn(
                'h-9 rounded-md px-3 text-sm font-medium text-white disabled:opacity-50',
                danger ? 'bg-negative hover:opacity-90' : 'bg-primary hover:bg-primary-hover',
              )}
            >
              {loading ? '处理中…' : confirmLabel}
            </button>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
