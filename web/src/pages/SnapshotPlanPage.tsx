import { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, RotateCw } from 'lucide-react';
import { fetchList, fetchOne, updateRow } from '../api/client';
import type { RowData } from '../api/types';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { ErrorState, Skeleton } from '../components/States';
import { Switch } from '../components/Switch';
import { useToast } from '../components/Toast';
import { cn } from '../lib/cn';
import { formatDateTime } from '../lib/format';

/**
 * 快照只剩一个档位：snapshot 统管时/日/月/年四档，按自然边界各出一份。
 * 分钟档已下线，异常现场回放改看接入流水（保留期在下方配置）。
 */
const ROWS = [
  { granularity: 'snapshot', label: '快照', hint: '时 / 日 / 月 / 年各一份，自然边界对齐' },
] as const;

const LABELS: Record<string, string> = { snapshot: '快照' };

interface SlotState {
  enabled: boolean;
  step: number | null;
}

function toSlot(row: RowData | undefined): SlotState {
  return {
    enabled: row?.enabled === true,
    step: typeof row?.step === 'number' ? row.step : row?.step == null ? null : Number(row.step),
  };
}

function sameSlot(a: SlotState, b: SlotState): boolean {
  return a.enabled === b.enabled && a.step === b.step;
}

/** 快照计划：按自然时间档 / 细粒度档分组，开关即时编辑，保存按钮固定在最后一栏。 */
export function SnapshotPlanPage() {
  const toast = useToast();
  const [rows, setRows] = useState<Record<string, RowData>>({});
  const [draft, setDraft] = useState<Record<string, SlotState>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState<string | null>(null);
  const [retention, setRetention] = useState<string>('');
  const [retentionSaving, setRetentionSaving] = useState(false);
  const [confirmRetention, setConfirmRetention] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ pageSize: '50' });
      const [result, retentionRow] = await Promise.all([
        fetchList('entity', 'snapshot-slots', params),
        fetchOne('raw-retention', '1'),
      ]);
      const byGranularity: Record<string, RowData> = {};
      for (const row of result.rows) byGranularity[String(row.granularity)] = row;
      setRows(byGranularity);
      setDraft(Object.fromEntries(Object.entries(byGranularity).map(([g, row]) => [g, toSlot(row)])));
      setRetention(retentionRow?.retention_days == null ? '' : String(retentionRow.retention_days));
    } catch (err) {
      setError(err instanceof Error ? err.message : '快照计划加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const change = (granularity: string, patch: Partial<SlotState>) => {
    setDraft((current) => ({
      ...current,
      [granularity]: { ...toSlot(rows[granularity]), ...current[granularity], ...patch },
    }));
  };

  const reset = (granularity: string) => {
    setDraft((current) => ({ ...current, [granularity]: toSlot(rows[granularity]) }));
  };

  const save = async (granularity: string) => {
    const next = draft[granularity];
    if (!next) return;
    setSaving(granularity);
    try {
      // 关闭时把步长置空：DB 只在启用时要求步长，重新启用时前端给自然单位 1。
      const body: Record<string, unknown> = {
        enabled: next.enabled,
        step: next.enabled ? (next.step ?? 1) : null,
      };
      await updateRow('snapshot-slots', granularity, body);
      toast.success(`${LABELS[granularity] ?? granularity}档已保存`);
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(null);
    }
  };

  const enabledCount = useMemo(
    () => ROWS.filter((item) => draft[item.granularity]?.enabled).length,
    [draft],
  );

  const saveRetention = async (days: number | null) => {
    setRetentionSaving(true);
    try {
      await updateRow('raw-retention', '1', { retention_days: days });
      toast.success(days == null ? '已改为永久保留' : `保留 ${days} 天，过期分区将每日清理`);
      setConfirmRetention(false);
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存失败');
    } finally {
      setRetentionSaving(false);
    }
  };

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted">管理后台 / 快照</p>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">快照计划</h1>
          <p className="mt-1 truncate text-xs text-muted">
            时 / 日 / 月 / 年合并为一个快照开关；关闭只停写、不清历史。已启用{' '}
            {enabledCount}/{ROWS.length} 项
          </p>
        </div>
        <button
          type="button"
          onClick={() => void load()}
          disabled={loading}
          className="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <RotateCw className={cn('size-4', loading && 'animate-spin')} aria-hidden />
          刷新
        </button>
      </div>

      {error ? (
        <div className="mt-4 rounded-xl border border-line bg-surface p-6 shadow-panel">
          <ErrorState message={error} onRetry={() => void load()} />
        </div>
      ) : (
        <>
          <PlanCard
            title="档位"
            hint="快照 = 时/日/月/年（一个开关统管）；关闭只停写、不清历史"
            rows={ROWS.map((item) => ({
              granularity: item.granularity,
              label: item.label,
              step: (
                <div className="flex items-center gap-2">
                  <span className="text-muted tabular">每自然时 / 日 / 月 / 年</span>
                  <span className="text-xs text-muted">{item.hint}</span>
                </div>
              ),
            }))}
            loading={loading}
            rowsByGranularity={rows}
            draft={draft}
            saving={saving}
            onChange={change}
            onReset={reset}
            onSave={(g) => void save(g)}
          />

          <section className="mt-4 rounded-xl border border-line bg-surface p-4 shadow-panel">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div className="min-w-0">
                <h2 className="text-sm font-medium text-fg">数据流水（异常现场回放）</h2>
                <p className="mt-1 text-xs text-muted">
                  每次上报都保存整包原文与逐项清洗结果，用于按终端+时间窗回放；留空 = 永久保留。
                </p>
              </div>
              <div className="flex items-center gap-2">
                <label className="text-xs text-muted" htmlFor="raw-retention">
                  保留天数
                </label>
                <input
                  id="raw-retention"
                  inputMode="numeric"
                  value={retention}
                  placeholder="留空 = 永久"
                  onChange={(event) => setRetention(event.target.value.replace(/[^0-9]/g, ''))}
                  className="h-9 w-32 rounded-md border border-line bg-surface px-2 text-sm text-fg placeholder:text-muted focus:border-primary"
                />
                <button
                  type="button"
                  disabled={retentionSaving}
                  onClick={() => {
                    if (retention !== '' && Number(retention) <= 0) {
                      toast.error('保留天数必须大于 0，或留空表示永久保留');
                      return;
                    }
                    if (retention !== '') setConfirmRetention(true);
                    else void saveRetention(null);
                  }}
                  className="inline-flex h-9 items-center rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover disabled:opacity-50"
                >
                  保存
                </button>
              </div>
            </div>
          </section>
        </>
      )}

      <ConfirmDialog
        open={confirmRetention}
        onOpenChange={setConfirmRetention}
        title="启用流水清理"
        description={`保存后每天将删除早于 ${retention || '?'} 天的流水分区，不可恢复。`}
        confirmLabel="保存并启用"
        loading={retentionSaving}
        onConfirm={() => void saveRetention(Number(retention))}
      />
    </div>
  );
}

interface PlanRow {
  granularity: string;
  label: string;
  step: React.ReactNode;
}

interface PlanCardProps {
  title: string;
  hint: string;
  rows: PlanRow[];
  className?: string;
  loading: boolean;
  rowsByGranularity: Record<string, RowData>;
  draft: Record<string, SlotState>;
  saving: string | null;
  onChange: (granularity: string, patch: Partial<SlotState>) => void;
  onReset: (granularity: string) => void;
  onSave: (granularity: string) => void;
}

/** 分组卡片：开关即时改本地草稿，保存按钮固定在最后一栏（窄屏横向滚动时始终可见）。 */
function PlanCard({
  title,
  hint,
  rows,
  className,
  loading,
  rowsByGranularity,
  draft,
  saving,
  onChange,
  onReset,
  onSave,
}: PlanCardProps) {
  return (
    <section className={cn('mt-4 overflow-hidden rounded-xl border border-line bg-surface shadow-panel', className)}>
      <header className="border-b border-line px-4 py-3">
        <h2 className="text-sm font-medium text-fg">{title}</h2>
        <p className="mt-0.5 text-xs text-muted">{hint}</p>
      </header>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[760px] text-sm">
          <thead>
            <tr className="bg-surface-2 text-xs font-medium text-muted">
              <th scope="col" className="border-b border-line px-4 py-2.5 text-left">档位</th>
              <th scope="col" className="border-b border-line px-4 py-2.5 text-left">步长</th>
              <th scope="col" className="border-b border-line px-4 py-2.5 text-left">启用</th>
              <th scope="col" className="border-b border-line px-4 py-2.5 text-left">更新时间</th>
              <th
                scope="col"
                className="sticky right-0 z-10 border-b border-l border-line bg-surface-2 px-4 py-2.5 text-right"
              >
                操作
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const current = draft[row.granularity] ?? toSlot(rowsByGranularity[row.granularity]);
              const original = toSlot(rowsByGranularity[row.granularity]);
              const dirty = !sameSlot(current, original);
              const isSaving = saving === row.granularity;
              return (
                <tr key={row.granularity} className="border-b border-line last:border-b-0 hover:bg-surface-2/60">
                  <td className="px-4 py-2.5 font-medium text-fg">{row.label}</td>
                  <td className="px-4 py-2.5">{loading ? <Skeleton className="h-4 w-24" /> : row.step}</td>
                  <td className="px-4 py-2.5">
                    {loading ? (
                      <Skeleton className="h-5 w-20" />
                    ) : (
                      <Switch
                        checked={current?.enabled ?? false}
                        onCheckedChange={(checked) => onChange(row.granularity, { enabled: checked })}
                        label={current?.enabled ? '启用' : '停用'}
                        aria-label={`${row.label}启用`}
                      />
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-muted tabular">
                    {loading ? <Skeleton className="h-4 w-32" /> : formatDateTime(rowsByGranularity[row.granularity]?.updated_at)}
                  </td>
                  <td className="sticky right-0 z-10 border-l border-line bg-surface px-4 py-2.5">
                    <div className="flex items-center justify-end gap-2">
                      {dirty ? <span className="text-xs text-warning">未保存</span> : null}
                      <button
                        type="button"
                        disabled={!dirty || isSaving}
                        onClick={() => onReset(row.granularity)}
                        className="h-8 rounded-md border border-line px-2.5 text-xs text-fg hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        重置
                      </button>
                      <button
                        type="button"
                        disabled={!dirty || isSaving}
                        onClick={() => onSave(row.granularity)}
                        className="inline-flex h-8 items-center gap-1 rounded-md bg-primary px-3 text-xs font-medium text-white hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        {isSaving ? <Loader2 size={12} className="animate-spin" aria-hidden /> : null}
                        保存
                      </button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}
