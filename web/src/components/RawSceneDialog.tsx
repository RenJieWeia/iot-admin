import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { fetchRawItems } from '../api/client';
import type { RawItemRow } from '../api/client';
import { formatDateTime, formatNumber } from '../lib/format';

interface RawSceneDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  terminalID: string;
  from: string;
  to: string;
}

const STATUS_LABEL: Record<RawItemRow['status'], string> = {
  written: '入库',
  dropped: '剔除',
  ignored: '忽略',
};

/** 异常现场回放：按终端 + 时间窗展示接入流水（含被剔除项）。 */
export function RawSceneDialog({ open, onOpenChange, terminalID, from, to }: RawSceneDialogProps) {
  const [rows, setRows] = useState<RawItemRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    fetchRawItems(terminalID, from, to)
      .then((data) => {
        if (!controller.signal.aborted) setRows(data);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setError(err instanceof Error ? err.message : '读取流水失败');
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [open, terminalID, from, to]);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[60] bg-black/40" />
        <Dialog.Content className="fixed top-1/2 left-1/2 z-[70] flex max-h-[85vh] w-[calc(100vw-2rem)] max-w-4xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-line bg-surface p-5 shadow-panel outline-none">
          <Dialog.Title className="text-base font-semibold text-fg">现场回放 · {terminalID}</Dialog.Title>
          <Dialog.Description className="mt-1 text-xs text-muted">
            {formatDateTime(from)} ~ {formatDateTime(to)} · 共 {rows.length} 条（最多 1000 条）
          </Dialog.Description>

          {loading ? <p className="mt-4 text-sm text-muted">读取中…</p> : null}
          {error ? <p className="mt-4 text-sm text-negative">{error}</p> : null}

          {!loading && !error ? (
            <div className="mt-3 min-h-0 flex-1 overflow-auto rounded-md border border-line">
              <table className="w-full border-collapse text-xs">
                <thead className="sticky top-0 bg-surface-2 text-muted">
                  <tr>
                    <th className="px-2 py-1.5 text-left font-medium">到达时间</th>
                    <th className="px-2 py-1.5 text-left font-medium">数据项</th>
                    <th className="px-2 py-1.5 text-left font-medium">值</th>
                    <th className="px-2 py-1.5 text-left font-medium">单位</th>
                    <th className="px-2 py-1.5 text-left font-medium">采集时间</th>
                    <th className="px-2 py-1.5 text-left font-medium">状态</th>
                    <th className="px-2 py-1.5 text-left font-medium">原因</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr
                      key={row.id}
                      className={row.status !== 'written' ? 'bg-negative-soft/40' : undefined}
                    >
                      <td className="px-2 py-1.5 tabular">{formatDateTime(row.received_at)}</td>
                      <td className="px-2 py-1.5 font-mono">{row.item_code}</td>
                      <td className="px-2 py-1.5 tabular">
                        {row.value_num != null ? formatNumber(row.value_num) : (row.value_text ?? '—')}
                      </td>
                      <td className="px-2 py-1.5">{row.unit || '—'}</td>
                      <td className="px-2 py-1.5 tabular">
                        {row.value_time ? formatDateTime(row.value_time) : '—'}
                      </td>
                      <td className="px-2 py-1.5">{STATUS_LABEL[row.status]}</td>
                      <td className="px-2 py-1.5 text-muted">{row.reason || '—'}</td>
                    </tr>
                  ))}
                  {rows.length === 0 ? (
                    <tr>
                      <td colSpan={7} className="px-2 py-6 text-center text-muted">
                        该时间窗没有流水
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          ) : null}

          <div className="mt-4 flex justify-end">
            <Dialog.Close asChild>
              <button
                type="button"
                className="h-9 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2"
              >
                关闭
              </button>
            </Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
