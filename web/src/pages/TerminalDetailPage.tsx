import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { RotateCw } from 'lucide-react';
import { fetchList, fetchOne } from '../api/client';
import type { EntityMeta, MetaPayload, RowData } from '../api/types';
import { FieldValue } from '../components/FieldValue';
import { RecordDrawer } from '../components/RecordDrawer';
import { EmptyState, ErrorState, Skeleton } from '../components/States';
import { cn } from '../lib/cn';
import { isBlank } from '../lib/format';
import { useMeta } from '../meta';

/** 终端详情：它是什么（基础信息）、能上报什么（类型字段）、当前值（最新值）。 */
export function TerminalDetailPage() {
  const { id = '' } = useParams();
  const meta = useMeta();
  const terminalEntity = meta.entities.find((entity) => entity.name === 'terminals');
  const propertyEntity = (meta.subresources ?? []).find(
    (entity) => entity.name === 'thing-model-properties',
  );
  const latestPage = (meta.readonly ?? []).find((page) => page.slot === 'latest-values');

  const [terminal, setTerminal] = useState<RowData | null>(null);
  const [typeRow, setTypeRow] = useState<RowData | null>(null);
  const [properties, setProperties] = useState<RowData[]>([]);
  const [latest, setLatest] = useState<RowData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      const row = await fetchOne('terminals', id);
      setTerminal(row);
      const typeID = row.type_id;
      const [type, props, values] = await Promise.all([
        typeID === undefined || typeID === null
          ? Promise.resolve<RowData | null>(null)
          : fetchOne('device-types', String(typeID)),
        typeID === undefined || typeID === null
          ? Promise.resolve({ rows: [] as RowData[] })
          : fetchList(
              'entity',
              'thing-model-properties',
              new URLSearchParams({ 'filter.type_id': String(typeID), pageSize: '100' }),
            ),
        fetchList(
          'readonly',
          'latest-values',
          new URLSearchParams({ 'filter.terminal_id.eq': id, pageSize: '50' }),
        ),
      ]);
      setTypeRow(type);
      setProperties(props.rows);
      setLatest(values.rows);
    } catch (err) {
      setError(err instanceof Error ? err.message : '终端详情加载失败');
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  const propertyFields = useMemo(
    () => (propertyEntity?.fields ?? []).filter((field) => field.visible && field.key !== 'type_id'),
    [propertyEntity],
  );
  const latestFields = useMemo(
    () => (latestPage?.fields ?? []).filter((field) => field.visible && field.key !== 'terminal_id'),
    [latestPage],
  );

  if (!terminalEntity || !propertyEntity || !latestPage) {
    return (
      <div className="mx-auto max-w-[1400px] px-6 py-5">
        <EmptyState
          title="元数据缺失"
          hint="terminals / thing-model-properties / latest-values 未在 _meta 中下发。"
        />
      </div>
    );
  }

  const typeID = terminal?.type_id;
  const typeLabel = typeRow
    ? `${String(typeRow.name ?? typeRow.code ?? '')}（${String(typeRow.code ?? '')}）`
    : isBlank(typeID)
      ? '—'
      : String(typeID);

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted">管理后台 / 终端 / 详情</p>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">
            {terminal ? String(terminal.name ?? terminal.id ?? id) : id}
          </h1>
          <p className="mt-1 truncate text-xs text-muted">
            {id} · 类型 {typeLabel} · 网关 {isBlank(terminal?.gateway_id) ? '—' : String(terminal?.gateway_id)} ·
            状态 {terminal?.enabled === true ? '启用' : '停用'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            disabled={!terminal}
            className="inline-flex h-9 items-center rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
          >
            编辑终端
          </button>
          <button
            type="button"
            onClick={() => void load()}
            disabled={loading}
            className="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
          >
            <RotateCw className={cn('size-4', loading && 'animate-spin')} aria-hidden />
            刷新
          </button>
        </div>
      </div>

      {error ? (
        <div className="mt-4 rounded-xl border border-line bg-surface p-6 shadow-panel">
          <ErrorState message={error} onRetry={() => void load()} />
        </div>
      ) : (
        <>
          <Section className="mt-4" title="基础信息" hint="终端是终端类型的实例">
            <dl className="grid grid-cols-1 gap-x-6 gap-y-2 px-4 py-3 text-sm sm:grid-cols-2">
              <Info label="终端 ID" value={<span className="font-mono text-xs">{id}</span>} />
              <Info label="名称" value={terminal ? String(terminal.name ?? '—') : '—'} />
              <Info
                label="终端类型"
                value={
                  isBlank(typeID) ? (
                    '—'
                  ) : (
                    <Link className="text-primary hover:underline" to={`/e/device-types/${String(typeID)}`}>
                      {typeLabel}
                    </Link>
                  )
                }
              />
              <Info label="网关" value={<span className="font-mono text-xs">{String(terminal?.gateway_id ?? '—')}</span>} />
              <Info label="状态" value={terminal?.enabled === true ? '启用' : '停用'} />
            </dl>
          </Section>

          <Section
            className="mt-4"
            title="类型字段（物模型属性）"
            hint="该终端能上报的数据项由所属类型决定"
            action={
              <Link
                to={isBlank(typeID) ? '/e/device-types' : `/e/device-types/${String(typeID)}`}
                className="inline-flex h-8 items-center rounded-md border border-line px-2.5 text-xs text-fg hover:bg-surface-2"
              >
                查看类型详情
              </Link>
            }
          >
            <SimpleTable
              fields={propertyFields}
              rows={properties}
              loading={loading}
              empty="所属类型还没有绑定字段"
            />
          </Section>

          <Section
            className="mt-4"
            title="最新值"
            hint="最近一次上报；空表示还没收到该终端的数据"
          >
            <SimpleTable fields={latestFields} rows={latest} loading={loading} empty="该终端还没有上报数据" />
          </Section>
        </>
      )}

      <RecordDrawer
        entity={terminalEntity as EntityMeta}
        row={terminal}
        mode="view"
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        onSaved={() => void load()}
      />
    </div>
  );
}

function Info({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-line/60 py-1.5 last:border-b-0 sm:border-b-0 sm:py-0">
      <dt className="shrink-0 text-xs text-muted">{label}</dt>
      <dd className="min-w-0 truncate text-right text-sm text-fg">{value}</dd>
    </div>
  );
}

function Section({
  title,
  hint,
  action,
  className,
  children,
}: {
  title: string;
  hint: string;
  action?: React.ReactNode;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <section className={cn('overflow-hidden rounded-xl border border-line bg-surface shadow-panel', className)}>
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-line px-4 py-3">
        <div>
          <h2 className="text-sm font-medium text-fg">{title}</h2>
          <p className="mt-0.5 text-xs text-muted">{hint}</p>
        </div>
        {action}
      </header>
      {children}
    </section>
  );
}

function SimpleTable({
  fields,
  rows,
  loading,
  empty,
}: {
  fields: MetaPayload['entities'][number]['fields'];
  rows: RowData[];
  loading: boolean;
  empty: string;
}) {
  if (loading && rows.length === 0) {
    return (
      <div className="space-y-2 p-4">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-8 w-full" />
        ))}
      </div>
    );
  }
  if (rows.length === 0) {
    return <p className="px-4 py-6 text-sm text-muted">{empty}</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-sm">
        <thead>
          <tr className="bg-surface-2 text-xs font-medium text-muted">
            {fields.map((field) => (
              <th key={field.key} scope="col" className="border-b border-line px-4 py-2.5 text-left font-medium">
                {field.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={String(row.id ?? JSON.stringify(row))} className="border-b border-line last:border-b-0">
              {fields.map((field) => (
                <td key={field.key} className="px-4 py-2.5 align-middle">
                  <div className="truncate">
                    <FieldValue field={field} value={row[field.key]} />
                  </div>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
