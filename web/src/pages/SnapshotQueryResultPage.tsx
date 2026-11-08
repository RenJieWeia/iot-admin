import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Pencil, RotateCw } from 'lucide-react';
import { fetchOne, runSnapshotQuery } from '../api/client';
import type { SnapshotQueryCell, SnapshotQueryPayload } from '../api/client';
import { EmptyState, ErrorState, Skeleton } from '../components/States';
import { cn } from '../lib/cn';
import { formatDateTime, formatNumber, isBlank } from '../lib/format';

const GRANULARITIES = [
  { key: 'hour', label: '时' },
  { key: 'day', label: '日' },
  { key: 'month', label: '月' },
  { key: 'year', label: '年' },
] as const;

type Granularity = (typeof GRANULARITIES)[number]['key'];

/** datetime-local 需要的本地格式（不带秒）。 */
function toLocalInput(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** 各档位的默认时间范围：时=近 24h、日=近 14 天、月=近 6 月、年=近 5 年。 */
function defaultRange(granularity: Granularity): { from: string; to: string } {
  const now = new Date();
  const from = new Date(now);
  switch (granularity) {
    case 'hour':
      from.setHours(from.getHours() - 24);
      break;
    case 'day':
      from.setDate(from.getDate() - 14);
      break;
    case 'month':
      from.setMonth(from.getMonth() - 6);
      break;
    case 'year':
      from.setFullYear(from.getFullYear() - 5);
      break;
  }
  return { from: toLocalInput(from), to: toLocalInput(now) };
}

function cellText(cell: SnapshotQueryCell | undefined): string {
  if (!cell) return '—';
  if (!isBlank(cell.num)) return formatNumber(cell.num);
  if (!isBlank(cell.text)) return String(cell.text);
  return '—';
}

/** 快照查询结果：模板 × 档位 × 时间范围 → 终端 × 字段透视表。 */
export function SnapshotQueryResultPage() {
  const { id = '' } = useParams();
  const [templateName, setTemplateName] = useState('');
  const [granularity, setGranularity] = useState<Granularity>('hour');
  const [range, setRange] = useState(() => defaultRange('hour'));
  const [payload, setPayload] = useState<SnapshotQueryPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchOne('snapshot-queries', id)
      .then((row) => setTemplateName(String(row.name ?? `模板 ${id}`)))
      .catch(() => setTemplateName(`模板 ${id}`));
  }, [id]);

  const run = useCallback(
    async (nextRange = range) => {
      setLoading(true);
      setError(null);
      try {
        const data = await runSnapshotQuery({
          template: Number(id),
          granularity,
          from: new Date(nextRange.from).toISOString(),
          to: new Date(nextRange.to).toISOString(),
        });
        setPayload(data);
      } catch (err) {
        setPayload(null);
        setError(err instanceof Error ? err.message : '查询失败');
      } finally {
        setLoading(false);
      }
    },
    // granularity/range 通过参数与闭包读取；切换时由下面的 effect 触发。
    [id, granularity, range],
  );

  useEffect(() => {
    void run();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, granularity]);

  const switchGranularity = (next: Granularity) => {
    setGranularity(next);
    const preset = defaultRange(next);
    setRange(preset);
    void run(preset);
  };

  const columns = payload?.columns ?? [];
  const rows = payload?.rows ?? [];

  return (
    <div className="mx-auto max-w-[1500px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs text-muted">
            <Link to="/e/snapshot-queries" className="inline-flex items-center gap-1 hover:text-fg">
              <ArrowLeft className="size-3.5" aria-hidden />
              快照查询
            </Link>
          </div>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">{templateName}</h1>
          <p className="mt-1 truncate text-xs text-muted">
            行 = 终端实例，列 = 字段；单元格取所选范围内最新一条（同段覆盖取 revision 最新）
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            to={`/e/snapshot-queries/${id}/edit`}
            className="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2"
          >
            <Pencil className="size-4" aria-hidden />
            编辑模板
          </Link>
          <button
            type="button"
            onClick={() => void run()}
            disabled={loading}
            className="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
          >
            <RotateCw className={cn('size-4', loading && 'animate-spin')} aria-hidden />
            查询
          </button>
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-3 rounded-xl border border-line bg-surface px-4 py-3 shadow-panel">
        <div className="inline-flex h-8 items-center rounded-md border border-line p-0.5" role="group" aria-label="档位">
          {GRANULARITIES.map((item) => (
            <button
              key={item.key}
              type="button"
              aria-pressed={granularity === item.key}
              onClick={() => switchGranularity(item.key)}
              className={cn(
                'h-7 rounded px-3 text-xs',
                granularity === item.key ? 'bg-primary-soft text-primary' : 'text-muted hover:text-fg',
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-xs text-muted">
          起
          <input
            type="datetime-local"
            value={range.from}
            onChange={(event) => setRange((current) => ({ ...current, from: event.target.value }))}
            className="h-8 rounded-md border border-line bg-surface px-2 text-xs text-fg"
          />
        </label>
        <label className="flex items-center gap-2 text-xs text-muted">
          至
          <input
            type="datetime-local"
            value={range.to}
            onChange={(event) => setRange((current) => ({ ...current, to: event.target.value }))}
            className="h-8 rounded-md border border-line bg-surface px-2 text-xs text-fg"
          />
        </label>
        <button
          type="button"
          onClick={() => void run()}
          disabled={loading}
          className="inline-flex h-8 items-center rounded-md bg-primary px-3 text-xs font-medium text-white hover:bg-primary-hover disabled:opacity-50"
        >
          {loading ? '查询中…' : '查询'}
        </button>
        <span className="ml-auto text-xs text-muted">
          {payload ? `${rows.length} 个终端 · ${columns.length} 个字段` : ' '}
        </span>
      </div>

      {error ? (
        <div className="mt-4 rounded-xl border border-line bg-surface p-6 shadow-panel">
          <ErrorState message={error} onRetry={() => void run()} />
        </div>
      ) : loading && !payload ? (
        <div className="mt-4 space-y-2 rounded-xl border border-line bg-surface p-4 shadow-panel">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-9 w-full" />
          ))}
        </div>
      ) : rows.length === 0 || columns.length === 0 ? (
        <div className="mt-4 rounded-xl border border-line bg-surface p-8 shadow-panel">
          <EmptyState
            title="没有可展示的数据"
            hint="可能是模板选中的类型下还没有终端/字段，或所选时间范围内没有快照。"
          />
        </div>
      ) : (
        <div className="mt-4 min-h-[max(420px,calc(100vh-15rem))] overflow-x-auto rounded-xl border border-line bg-surface shadow-panel">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="bg-surface-2 text-xs font-medium text-muted">
                <th className="sticky left-0 z-10 border-b border-line bg-surface-2 px-4 py-2.5 text-left">
                  终端
                </th>
                {columns.map((column) => (
                  <th key={column.key} className="border-b border-line px-4 py-2.5 text-left whitespace-nowrap">
                    {column.label}
                    {column.unit ? <span className="ml-1 font-normal text-muted">({column.unit})</span> : null}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.terminalId} className="border-b border-line last:border-b-0 hover:bg-surface-2/60">
                  <td className="sticky left-0 z-10 border-r border-line bg-surface px-4 py-2 align-middle">
                    <div className="truncate text-sm text-fg">{row.terminalName || '—'}</div>
                    <div className="truncate font-mono text-xs text-muted">{row.terminalId}</div>
                  </td>
                  {columns.map((column) => {
                    const cell = row.values[column.key];
                    const bad = (cell?.quality ?? 0) > 0;
                    return (
                      <td
                        key={column.key}
                        className={cn('px-4 py-2 align-middle whitespace-nowrap', bad ? 'text-warning' : 'text-fg')}
                        title={cell?.time ? `采集时间 ${formatDateTime(cell.time)}` : undefined}
                      >
                        <span className="tabular">{cellText(cell)}</span>
                        {cell?.unit ? <span className="ml-1 text-xs text-muted">{cell.unit}</span> : null}
                        {bad ? <span className="ml-1 text-xs">⚠</span> : null}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
