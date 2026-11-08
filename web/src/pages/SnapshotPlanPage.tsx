import { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, RotateCw } from 'lucide-react';
import { fetchList, updateRow } from '../api/client';
import type { RowData } from '../api/types';
import { ErrorState, Skeleton } from '../components/States';
import { Switch } from '../components/Switch';
import { useToast } from '../components/Toast';
import { cn } from '../lib/cn';
import { formatDateTime } from '../lib/format';
import { useMeta } from '../meta';

/** 粗档（小时/日/月/年）按自然边界出快照：步长固定 1 个自然单位，页面上不给倍数开关。 */
const COARSE = [
  { granularity: 'hour', label: '每小时', step: '每自然小时' },
  { granularity: 'day', label: '每日', step: '每自然日' },
  { granularity: 'month', label: '每月', step: '每自然月' },
  { granularity: 'year', label: '每年', step: '每自然年' },
] as const;

/** 细粒度档保留倍数步长（秒/分），步长选项取 _meta.enums。<granularity>_step。 */
const FINE = [
  { granularity: 'second', label: '每秒' },
  { granularity: 'minute', label: '每分钟' },
] as const;

const LABELS: Record<string, string> = Object.fromEntries(
  [...COARSE, ...FINE].map((item) => [item.granularity, item.label]),
);

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
  const meta = useMeta();
  const toast = useToast();
  const [rows, setRows] = useState<Record<string, RowData>>({});
  const [draft, setDraft] = useState<Record<string, SlotState>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ pageSize: '50' });
      const result = await fetchList('entity', 'snapshot-slots', params);
      const byGranularity: Record<string, RowData> = {};
      for (const row of result.rows) byGranularity[String(row.granularity)] = row;
      setRows(byGranularity);
      setDraft(Object.fromEntries(Object.entries(byGranularity).map(([g, row]) => [g, toSlot(row)])));
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

  const enabledCoarse = useMemo(
    () => COARSE.filter((item) => draft[item.granularity]?.enabled).length,
    [draft],
  );

  const stepOptions = (granularity: string) => {
    const dict = meta.enums[`${granularity}_step`] ?? {};
    return Object.entries(dict)
      .map(([value, label]) => ({ value: Number(value), label }))
      .sort((a, b) => a.value - b.value);
  };

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted">管理后台 / 快照</p>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">快照计划</h1>
          <p className="mt-1 truncate text-xs text-muted">
            小时 / 日 / 月 / 年四档按自然边界出快照，步长固定 1 个自然单位；已启用{' '}
            {enabledCoarse}/{COARSE.length} 档
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
            title="自然时间档"
            hint="每档一份快照，按本地自然小时 / 日 / 月 / 年对齐"
            rows={COARSE.map((item) => ({
              granularity: item.granularity,
              label: item.label,
              step: <span className="text-muted tabular">{item.step}</span>,
            }))}
            loading={loading}
            rowsByGranularity={rows}
            draft={draft}
            saving={saving}
            onChange={change}
            onReset={reset}
            onSave={(g) => void save(g)}
          />
          <PlanCard
            className="mt-4"
            title="细粒度档"
            hint="秒 / 分档保留倍数步长，按所选步长切段"
            rows={FINE.map((item) => ({
              granularity: item.granularity,
              label: item.label,
              step: (
                <select
                  aria-label={`${item.label}步长`}
                  value={String(draft[item.granularity]?.step ?? '')}
                  onChange={(event) => change(item.granularity, { step: Number(event.target.value) })}
                  className="h-8 rounded-md border border-line bg-surface px-2 text-sm text-fg"
                >
                  <option value="">未设置</option>
                  {stepOptions(item.granularity).map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
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
        </>
      )}
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
