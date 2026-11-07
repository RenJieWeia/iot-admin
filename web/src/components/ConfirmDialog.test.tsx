import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { ConfirmDialog } from './ConfirmDialog';

const LABEL = '终端 1（t-001）';
const CONFIRM_NAME = `删除 ${LABEL}`;

interface HarnessProps {
  busy?: boolean;
  /** 成功后由调用方关闭弹窗（页面里的真实路径：`pendingDelete` 置 null）。 */
  closeOnConfirm?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/** 受控宿主：open 归调用方所有，和三个调用点（列表页 / 详情页 / 快照绑定）的行为一致。 */
function Harness({ busy = false, closeOnConfirm = false, onCancel, onConfirm }: HarnessProps) {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmDialog
      open={open}
      objectName={LABEL}
      busy={busy}
      onCancel={() => {
        onCancel();
        setOpen(false);
      }}
      onConfirm={() => {
        onConfirm();
        if (closeOnConfirm) setOpen(false);
      }}
    />
  );
}

function setup(props: { busy?: boolean; closeOnConfirm?: boolean } = {}) {
  const onCancel = vi.fn();
  const onConfirm = vi.fn();
  const view = render(<Harness {...props} onCancel={onCancel} onConfirm={onConfirm} />);
  return { onCancel, onConfirm, ...view };
}

/** 打开后焦点应落在「取消」（DOM 顺序第一个可聚焦元素）。 */
async function waitForInitialFocus() {
  await waitFor(() => expect(screen.getByRole('button', { name: '取消' })).toHaveFocus());
}

describe('ConfirmDialog（Themes AlertDialog）', () => {
  it('Esc 关闭：触发一次取消回调，不触发确认', async () => {
    const user = userEvent.setup();
    const { onCancel, onConfirm } = setup();
    await waitForInitialFocus();

    const dialog = screen.getByRole('alertdialog');
    // role / 标题 / 描述的可访问性关联由 Radix 自动挂，不是手写属性。
    expect(dialog).toHaveAttribute('aria-labelledby', screen.getByText('确认删除').id);
    expect(dialog).toHaveAttribute(
      'aria-describedby',
      screen.getByText(/删除后不可恢复/).id,
    );

    await user.keyboard('{Escape}');

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('焦点陷阱：Tab / Shift+Tab 在两个按钮之间循环，不逃出弹窗', async () => {
    const user = userEvent.setup();
    setup();
    await waitForInitialFocus();

    const confirm = screen.getByRole('button', { name: CONFIRM_NAME });
    const cancel = screen.getByRole('button', { name: '取消' });

    await user.tab();
    expect(confirm).toHaveFocus();
    await user.tab();
    expect(cancel).toHaveFocus();
    await user.tab({ shift: true });
    expect(confirm).toHaveFocus();
  });

  it('确认回调恰好一次，且弹窗不自行关闭（关闭权在调用方）', async () => {
    const user = userEvent.setup();
    const { onCancel, onConfirm } = setup();
    await waitForInitialFocus();

    await user.click(screen.getByRole('button', { name: CONFIRM_NAME }));

    expect(onConfirm).toHaveBeenCalledTimes(1);
    // AlertDialog.Action 自带 Close 语义；这里吞掉它，弹窗留在原地等调用方收尾。
    await waitFor(() => expect(screen.getByRole('alertdialog')).toBeInTheDocument());
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('取消回调恰好一次，关闭不误触发确认', async () => {
    const user = userEvent.setup();
    const { onCancel, onConfirm } = setup();
    await waitForInitialFocus();

    await user.click(screen.getByRole('button', { name: '取消' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('点遮罩不关闭（alertdialog 必须显式选按钮）', async () => {
    setup();
    await waitForInitialFocus();

    fireEvent.pointerDown(document.body);
    fireEvent.click(document.body);

    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  it('busy 下确认按钮禁用且不重复触发；解除 busy 后再点才走调用方关闭', async () => {
    const user = userEvent.setup();
    const { onCancel, onConfirm, rerender } = setup({ busy: true });
    // busy 时两个按钮都 disabled，没有可聚焦控件，焦点先落在弹窗容器上——不按「焦点在取消」断言。
    await waitFor(() => expect(screen.getByRole('alertdialog')).toBeInTheDocument());

    const busyConfirm = screen.getByRole('button', { name: CONFIRM_NAME });
    expect(busyConfirm).toBeDisabled();
    await user.click(busyConfirm);

    expect(onConfirm).not.toHaveBeenCalled();
    expect(onCancel).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    // 请求结束、busy 解除：此时点击才真的走确认，并由调用方（closeOnConfirm）关闭。
    rerender(<Harness busy={false} closeOnConfirm onCancel={onCancel} onConfirm={onConfirm} />);
    await user.click(screen.getByRole('button', { name: CONFIRM_NAME }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(onConfirm).toHaveBeenCalledTimes(1);
    // 关键：Action 的 Close 被吞掉，没有顺着 onOpenChange(false) 误触发取消。
    expect(onCancel).not.toHaveBeenCalled();
  });
});
