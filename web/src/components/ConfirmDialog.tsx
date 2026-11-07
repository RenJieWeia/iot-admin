import { useRef } from 'react';
import { AlertDialog, Button, Flex } from '@radix-ui/themes';

interface Props {
  open: boolean;
  /** 破坏性操作必须写清对象名，确认按钮上也要带（§5、§8）。 */
  objectName: string;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * 删除确认：Radix Themes 的 AlertDialog（`role="alertdialog"`）。
 *
 * 行为与换肤前一致，全部来自 Radix primitives：
 * - 焦点陷阱：Tab/Shift+Tab 在弹窗内循环；打开后焦点落在第一个可聚焦元素（DOM 顺序上「取消」在前）；
 * - Esc 关闭 → `onCancel`；点遮罩**不关闭**（alertdialog 语义：破坏性操作必须显式选一个按钮）；
 * - 关闭后焦点归还触发元素（Radix Dialog 自带）；
 * - `aria-labelledby` / `aria-describedby` 由 Title / Description 自动关联。
 *
 * 对外 props 契约没变（调用方三处不改）：open / objectName / busy / onCancel / onConfirm。
 */
export function ConfirmDialog({ open, objectName, busy = false, onCancel, onConfirm }: Props) {
  // AlertDialog.Action 是 Close 语义：点一下就会触发 onOpenChange(false)。
  // 本项目的确认是**异步**的——请求期间弹窗要留在原地显示 busy（按钮禁用、不重复触发），
  // 成功后由调用方把 open 置 false。所以吞掉「Action 触发的那一次」关闭请求；
  // Esc / 取消按钮 / 遮罩三条路径不受影响（它们照旧走 onCancel）。
  const swallowActionClose = useRef(false);

  return (
    <AlertDialog.Root
      open={open}
      onOpenChange={(next) => {
        if (next) return;
        if (swallowActionClose.current) {
          swallowActionClose.current = false;
          return;
        }
        onCancel();
      }}
    >
      {/* maxWidth 440px = 迁移前 confirm-panel 的宽度上限；窄屏由 Radix 的 scroll padding 兜底。 */}
      <AlertDialog.Content maxWidth="440px">
        <AlertDialog.Title>确认删除</AlertDialog.Title>
        <AlertDialog.Description color="gray">
          删除后不可恢复：{objectName}。请确认这不是误操作。
        </AlertDialog.Description>
        <Flex mt="2" gap="2" justify="end">
          <AlertDialog.Cancel>
            <Button type="button" disabled={busy} variant="surface" size="3">
              取消
            </Button>
          </AlertDialog.Cancel>
          <AlertDialog.Action>
            <Button
              type="button"
              disabled={busy}
              aria-label={`删除 ${objectName}`}
              variant="soft"
              size="3"
              color="red"
              onClick={() => {
                swallowActionClose.current = true;
                onConfirm();
              }}
            >
              删除 {objectName}
            </Button>
          </AlertDialog.Action>
        </Flex>
      </AlertDialog.Content>
    </AlertDialog.Root>
  );
}
