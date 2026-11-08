import * as Dialog from '@radix-ui/react-dialog';
import type { BatchResult } from '../api/client';

interface BatchResultDialogProps {
  result: BatchResult | null;
  onOpenChange: (open: boolean) => void;
}

const ACTION_LABEL: Record<BatchResult['action'], string> = {
  enable: '启用',
  disable: '停用',
  delete: '删除',
};

/** 批量操作结果：成功/失败计数 + 逐项失败原因 + 网关一次性密码。 */
export function BatchResultDialog({ result, onOpenChange }: BatchResultDialogProps) {
  return (
    <Dialog.Root open={result !== null} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[60] bg-black/40" />
        <Dialog.Content className="fixed top-1/2 left-1/2 z-[70] flex max-h-[80vh] w-[calc(100vw-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-line bg-surface p-5 shadow-panel outline-none">
          <Dialog.Title className="text-base font-semibold text-fg">
            批量{result ? ACTION_LABEL[result.action] : ''}结果
          </Dialog.Title>
          {result ? (
            <>
              <Dialog.Description className="mt-1 text-sm text-muted">
                共 {result.requested} 项 · 成功 {result.succeeded} 项 · 失败 {result.failed.length} 项
              </Dialog.Description>
              {result.failed.length ? (
                <ul className="mt-3 max-h-56 space-y-1.5 overflow-y-auto rounded-md border border-line bg-surface-2 p-2 text-xs">
                  {result.failed.map((item) => (
                    <li key={item.id} className="flex gap-2">
                      <code className="shrink-0 font-mono text-fg">{item.id}</code>
                      <span className="text-muted">{item.reason}</span>
                    </li>
                  ))}
                </ul>
              ) : null}
              {result.credentials?.length ? (
                <div className="mt-3 rounded-md border border-warning bg-warning-soft p-2 text-xs text-warning">
                  <p className="font-medium">以下网关已重新注册，密码只显示一次：</p>
                  <ul className="mt-1 space-y-0.5 font-mono">
                    {result.credentials.map((item) => (
                      <li key={item.id}>
                        {item.id} · {item.password}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </>
          ) : null}
          <div className="mt-4 flex justify-end">
            <Dialog.Close asChild>
              <button
                type="button"
                className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover"
              >
                知道了
              </button>
            </Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
