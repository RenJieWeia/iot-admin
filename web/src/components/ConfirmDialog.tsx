import * as AlertDialog from '@radix-ui/react-alert-dialog';

interface Props {
  open: boolean;
  /** 破坏性操作必须写清对象名，确认按钮上也要带（§5、§8）。 */
  objectName: string;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/** 删除确认：Radix AlertDialog（role=alertdialog），焦点默认落在「取消」。 */
export function ConfirmDialog({ open, objectName, busy = false, onCancel, onConfirm }: Props) {
  return (
    <AlertDialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="fixed inset-0 bg-overlay" />
        <AlertDialog.Content className="confirm-panel fixed left-1/2 top-1/2 flex -translate-x-1/2 -translate-y-1/2 flex-col gap-3 rounded-md bg-surface p-5">
          <AlertDialog.Title className="m-0 text-lg">确认删除</AlertDialog.Title>
          <AlertDialog.Description className="m-0 text-muted">
            删除后不可恢复：{objectName}。请确认这不是误操作。
          </AlertDialog.Description>
          <div className="mt-2 flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <button type="button" className="btn" disabled={busy}>
                取消
              </button>
            </AlertDialog.Cancel>
            <button
              type="button"
              className="btn btn-danger"
              onClick={onConfirm}
              disabled={busy}
              aria-label={`删除 ${objectName}`}
            >
              删除 {objectName}
            </button>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
