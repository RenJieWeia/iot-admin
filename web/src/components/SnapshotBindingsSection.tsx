import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { ApiError, deleteRow, fetchList } from '../api/client';
import { toLabelMap, useFieldOptions } from '../api/entities';
import type { FieldMeta, RowData } from '../api/types';
import { formatCell } from '../lib/format';
import { ConfirmDialog } from './ConfirmDialog';
import { SnapshotBindingForm } from './SnapshotBindingForm';

const BINDINGS_ENTITY = 'snapshot-bindings';

function errorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) return error.detail;
  return String(error);
}

/**
 * 绑定列表：一条 = 一个数据项被纳入快照。作用域二选一（类型级 / 实例级），可增删。
 * 列表不用通用 DataTable：这里需要把「作用域 + 目标」两列合成可读语义，而不是展示原始外键。
 */
export function SnapshotBindingsSection() {
  const queryClient = useQueryClient();
  const [formOpen, setFormOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<RowData | null>(null);

  const list = useQuery({
    queryKey: ['list', BINDINGS_ENTITY, 'all'],
    queryFn: ({ signal }) => fetchList(BINDINGS_ENTITY, { pageSize: 50 }, signal),
  });

  const typeOptions = useFieldOptions(['device-types']);
  const typeLabels = useMemo(() => toLabelMap(typeOptions.data?.['device-types']), [typeOptions.data]);

  const remove = useMutation({
    mutationFn: (id: string) => deleteRow(BINDINGS_ENTITY, id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['list', BINDINGS_ENTITY] });
      setPendingDelete(null);
    },
  });

  const rows = list.data?.rows ?? [];
  const createdAtField: FieldMeta = {
    key: 'created_at', label: '创建时间', type: 'datetime', visible: true, sortable: false,
    filterable: false, filterType: 'none', operators: [], operatorLabels: {}, options: [],
    editable: false, required: false, widthStrategy: 'min',
  };

  return (
    <section className="rounded-md border border-border bg-surface p-4" aria-label="绑定">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h2 className="m-0 text-lg">绑定</h2>
        <span className="text-meta text-muted">一条 = 一个数据项被纳入快照；作用域二选一。</span>
        <button
          type="button"
          className="btn btn-primary ml-auto"
          data-action="new-binding"
          onClick={() => setFormOpen(true)}
        >
          新建绑定
        </button>
      </div>

      {list.isError ? (
        <p className="m-0 text-negative-text" role="alert">
          绑定加载失败：{errorText(list.error) ?? '未知错误'}
          <button type="button" className="btn btn-link" onClick={() => void list.refetch()}>
            重试
          </button>
        </p>
      ) : list.isPending ? (
        <p className="m-0 text-meta text-muted" role="status">正在加载绑定…</p>
      ) : rows.length === 0 ? (
        <p className="m-0 text-meta text-muted">还没有绑定。用右上角「新建绑定」把数据项纳入快照。</p>
      ) : (
        <table className="w-full border-collapse text-body">
          <caption className="pb-2 text-left text-sm text-muted">当前绑定</caption>
          <thead>
            <tr>
              {['作用域', '目标', '数据项', '创建时间', '操作'].map((title) => (
                <th key={title} scope="col" className="h-11 border-b border-border bg-surface-2 px-3 text-left text-meta font-medium text-muted">
                  {title}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const typeID = row.device_type_id === null || row.device_type_id === undefined ? '' : String(row.device_type_id);
              const terminalID = row.terminal_id === null || row.terminal_id === undefined ? '' : String(row.terminal_id);
              const target = typeID !== '' ? (typeLabels[typeID] ?? typeID) : terminalID;
              return (
                <tr key={String(row.id)}>
                  <td className="h-11 overflow-hidden border-b border-border px-3 align-middle">
                    {typeID !== '' ? '终端类型' : '终端'}
                  </td>
                  <td className="h-11 overflow-hidden border-b border-border px-3 align-middle">{target}</td>
                  <td className="h-11 overflow-hidden border-b border-border px-3 align-middle font-mono text-sm">
                    {String(row.item_code ?? '')}
                  </td>
                  <td className="h-11 overflow-hidden border-b border-border px-3 align-middle">
                    {formatCell(createdAtField, row.created_at).text}
                  </td>
                  <td className="h-11 overflow-hidden border-b border-border px-3 text-right align-middle">
                    <button
                      type="button"
                      className="touch-hit inline-flex h-8 items-center rounded-control px-2 text-body text-muted hover:bg-danger-weak hover:text-danger"
                      data-action="delete-binding"
                      onClick={() => setPendingDelete(row)}
                    >
                      删除
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

      <SnapshotBindingForm
        open={formOpen}
        onClose={() => setFormOpen(false)}
        onCreated={() => void queryClient.invalidateQueries({ queryKey: ['list', BINDINGS_ENTITY] })}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        objectName={`绑定 ${String(pendingDelete?.item_code ?? '')}`}
        busy={remove.isPending}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          if (pendingDelete) remove.mutate(String(pendingDelete.id));
        }}
      />
    </section>
  );
}
