import * as Dialog from '@radix-ui/react-dialog';
import styles from './Panel.module.css';

interface Props {
  open: boolean;
  /** 破坏性操作必须写清对象名，确认按钮上也要带（§5、§8）。 */
  objectName: string;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

export function ConfirmDialog({ open, objectName, busy = false, onCancel, onConfirm }: Props) {
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className={styles.overlay} />
        <Dialog.Content className={styles.confirm} role="alertdialog">
          <Dialog.Title className={styles.drawerTitle}>确认删除</Dialog.Title>
          <Dialog.Description className={styles.stateText}>
            删除后不可恢复：{objectName}。请确认这不是误操作。
          </Dialog.Description>
          <div className={styles.confirmActions}>
            <button type="button" className="btn" onClick={onCancel} disabled={busy}>
              取消
            </button>
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
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
