import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Copy, LayoutGrid, Pencil, Play, Plus, RotateCw, Trash2 } from 'lucide-react';
import { createRow, deleteRow, fetchList } from '../api/client';
import type { RowData } from '../api/types';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { EmptyState, ErrorState, Skeleton } from '../components/States';
import { useToast } from '../components/Toast';
import { cn } from '../lib/cn';
import { formatDateTime } from '../lib/format';

interface TemplateRow extends RowData {
  id: number;
  name: string;
  description: string;
  device_type_ids: number[];
  item_codes: string[];
  updated_at: string;
}

function asTemplate(row: RowData): TemplateRow {
  const ids = Array.isArray(row.device_type_ids) ? (row.device_type_ids as number[]) : [];
  const codes = Array.isArray(row.item_codes) ? (row.item_codes as string[]) : [];
  return {
    ...row,
    id: Number(row.id),
    name: String(row.name ?? ''),
    description: String(row.description ?? ''),
    device_type_ids: ids.map(Number),
    item_codes: codes.map(String),
    updated_at: String(row.updated_at ?? ''),
  };
}

/** 快照查询入口：已保存模板的卡片列表。 */
export function SnapshotQueryListPage() {
  const navigate = useNavigate();
  const toast = useToast();
  const [rows, setRows] = useState<TemplateRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<TemplateRow | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchList('entity', 'snapshot-queries', new URLSearchParams({ pageSize: '200' }));
      setRows(result.rows.map(asTemplate));
    } catch (err) {
      setError(err instanceof Error ? err.message : '模板加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const duplicate = async (template: TemplateRow) => {
    try {
      await createRow('snapshot-queries', {
        name: `${template.name} 副本`,
        description: template.description,
        device_type_ids: template.device_type_ids,
        item_codes: template.item_codes,
      });
      toast.success('已复制模板');
      void load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '复制失败');
    }
  };

  const remove = async () => {
    if (!pendingDelete) return;
    setDeleting(true);
    try {
      await deleteRow('snapshot-queries', String(pendingDelete.id));
      toast.success('已删除模板');
      setPendingDelete(null);
      void load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除失败');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted">管理后台 / 快照</p>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">快照查询</h1>
          <p className="mt-1 truncate text-xs text-muted">
            按模板查看时 / 日 / 月 / 年快照：行是终端实例，列是终端字段，取所选范围内最新值
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void load()}
            disabled={loading}
            className="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
          >
            <RotateCw className={cn('size-4', loading && 'animate-spin')} aria-hidden />
            刷新
          </button>
          <Link
            to="/e/snapshot-queries/new"
            className="inline-flex h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover"
          >
            <Plus className="size-4" aria-hidden />
            新建模板
          </Link>
        </div>
      </div>

      {error ? (
        <div className="mt-4 rounded-xl border border-line bg-surface p-6 shadow-panel">
          <ErrorState message={error} onRetry={() => void load()} />
        </div>
      ) : loading && rows.length === 0 ? (
        <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((index) => (
            <div key={index} className="rounded-xl border border-line bg-surface p-4">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="mt-3 h-3 w-3/4" />
              <Skeleton className="mt-6 h-8 w-2/3" />
            </div>
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="mt-4 rounded-xl border border-line bg-surface p-8 shadow-panel">
          <EmptyState
            title="还没有查询模板"
            hint="模板决定表格的行（终端）与列（字段）；新建后即可按档位查看快照。"
            action={
              <Link
                to="/e/snapshot-queries/new"
                className="inline-flex h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover"
              >
                <Plus className="size-4" aria-hidden />
                新建模板
              </Link>
            }
          />
        </div>
      ) : (
        <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {rows.map((template) => (
            <article
              key={template.id}
              className="flex flex-col rounded-xl border border-line bg-surface p-4 shadow-panel transition-colors hover:border-line-strong"
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <h2 className="truncate text-sm font-medium text-fg" title={template.name}>
                    {template.name}
                  </h2>
                  <p className="mt-1 line-clamp-2 text-xs text-muted">
                    {template.description || '未填写说明'}
                  </p>
                </div>
                <LayoutGrid className="size-4 shrink-0 text-muted" aria-hidden />
              </div>
              <p className="mt-3 text-xs text-muted">
                {template.device_type_ids.length} 个物模型 · {template.item_codes.length} 个字段
              </p>
              <p className="mt-0.5 text-xs text-muted">更新于 {formatDateTime(template.updated_at)}</p>
              <div className="mt-4 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => navigate(`/e/snapshot-queries/${template.id}/query`)}
                  className="inline-flex h-8 items-center gap-1 rounded-md bg-primary px-2.5 text-xs font-medium text-white hover:bg-primary-hover"
                >
                  <Play className="size-3.5" aria-hidden />
                  打开查询
                </button>
                <Link
                  to={`/e/snapshot-queries/${template.id}/edit`}
                  className="inline-flex h-8 items-center gap-1 rounded-md border border-line px-2.5 text-xs text-fg hover:bg-surface-2"
                >
                  <Pencil className="size-3.5" aria-hidden />
                  编辑
                </Link>
                <button
                  type="button"
                  onClick={() => void duplicate(template)}
                  className="inline-flex h-8 items-center gap-1 rounded-md border border-line px-2.5 text-xs text-fg hover:bg-surface-2"
                >
                  <Copy className="size-3.5" aria-hidden />
                  复制
                </button>
                <button
                  type="button"
                  onClick={() => setPendingDelete(template)}
                  className="ml-auto inline-flex h-8 items-center gap-1 rounded-md px-2 text-xs text-negative hover:bg-negative-soft"
                >
                  <Trash2 className="size-3.5" aria-hidden />
                  删除
                </button>
              </div>
            </article>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title="删除查询模板"
        description={`删除「${pendingDelete?.name ?? ''}」后，用它保存的行列配置将无法恢复（快照数据不受影响）。`}
        confirmLabel="删除"
        loading={deleting}
        onConfirm={() => void remove()}
      />
    </div>
  );
}
