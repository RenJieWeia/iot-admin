import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Plus, RotateCw } from 'lucide-react';
import { fetchList, fetchOne } from '../api/client';
import type { EntityMeta, RowData } from '../api/types';
import { FieldValue } from '../components/FieldValue';
import { RecordDrawer } from '../components/RecordDrawer';
import { EmptyState, ErrorState, Skeleton } from '../components/States';
import { cn } from '../lib/cn';
import { useMeta } from '../meta';

interface DrawerState {
  open: boolean;
  mode: 'view' | 'create';
  row: RowData | null;
}

/** 终端类型详情：把「类型绑定的字段（物模型属性）」与「该类型的终端实例」放在一页。 */
export function DeviceTypeDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const meta = useMeta();
  const typeEntity = meta.entities.find((entity) => entity.name === 'device-types');
  const propertyEntity = (meta.subresources ?? []).find(
    (entity) => entity.name === 'thing-model-properties',
  );
  const terminalEntity = meta.entities.find((entity) => entity.name === 'terminals');

  const [typeRow, setTypeRow] = useState<RowData | null>(null);
  const [properties, setProperties] = useState<RowData[]>([]);
  const [terminals, setTerminals] = useState<RowData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [drawer, setDrawer] = useState<DrawerState>({ open: false, mode: 'view', row: null });
  // 类型本身的查看/编辑/删除（终端的字段与实例是本页两块，但类型行仍要能改）。
  const [typeDrawerOpen, setTypeDrawerOpen] = useState(false);

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      const [row, props, terms] = await Promise.all([
        fetchOne('device-types', id),
        fetchList(
          'entity',
          'thing-model-properties',
          new URLSearchParams({ 'filter.type_id': id, pageSize: '100' }),
        ),
        fetchList('entity', 'terminals', new URLSearchParams({ 'filter.type_id': id, pageSize: '100' })),
      ]);
      setTypeRow(row);
      setProperties(props.rows);
      setTerminals(terms.rows);
    } catch (err) {
      setError(err instanceof Error ? err.message : '终端类型详情加载失败');
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  // 本页上下文就是某个类型：type_id 列冗余，展示时去掉。
  const propertyFields = useMemo(
    () => (propertyEntity?.fields ?? []).filter((field) => field.visible && field.key !== 'type_id'),
    [propertyEntity],
  );
  const terminalFields = useMemo(
    () => (terminalEntity?.fields ?? []).filter((field) => field.visible),
    [terminalEntity],
  );

  if (!typeEntity || !propertyEntity || !terminalEntity) {
    return (
      <div className="mx-auto max-w-[1400px] px-6 py-5">
        <EmptyState title="元数据缺失" hint="device-types / thing-model-properties / terminals 未在 _meta 中下发。" />
      </div>
    );
  }

  const typeName = typeRow ? String(typeRow.name ?? typeRow.code ?? id) : id;

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted">管理后台 / 终端类型 / 详情</p>
          <h1 className="mt-1 truncate text-xl font-semibold text-fg">{typeName}</h1>
          <p className="mt-1 truncate text-xs text-muted">
            编码 {typeRow ? String(typeRow.code ?? '—') : '—'} · 绑定字段 {properties.length} 个 · 终端实例{' '}
            {terminals.length} 台
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setTypeDrawerOpen(true)}
            disabled={!typeRow}
            className="inline-flex h-9 items-center rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
          >
            编辑类型
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
          <Section
            className="mt-4"
            title="绑定字段（物模型属性）"
            hint="类型决定终端的字段集合；终端上报的 item_code 必须在这里声明"
            action={
              <button
                type="button"
                onClick={() => setDrawer({ open: true, mode: 'create', row: null })}
                className="inline-flex h-8 items-center gap-1 rounded-md bg-primary px-2.5 text-xs font-medium text-white hover:bg-primary-hover"
              >
                <Plus className="size-3.5" aria-hidden />
                新增字段
              </button>
            }
          >
            <SimpleTable
              fields={propertyFields}
              rows={properties}
              loading={loading}
              empty="这个类型还没有绑定任何字段"
              onRowClick={(row) => setDrawer({ open: true, mode: 'view', row })}
              entityName="thing-model-properties"
            />
          </Section>

          <Section
            className="mt-4"
            title="终端实例"
            hint="终端是类型的实例：同类型终端共享上面这套字段"
            action={
              <Link
                to="/e/terminals"
                className="inline-flex h-8 items-center rounded-md border border-line px-2.5 text-xs text-fg hover:bg-surface-2"
              >
                去终端列表
              </Link>
            }
          >
            <SimpleTable
              fields={terminalFields}
              rows={terminals}
              loading={loading}
              empty="这个类型下还没有终端"
              onRowClick={(row) => {
                if (row.id === undefined || row.id === null) return;
                navigate(`/e/terminals/${String(row.id)}`);
              }}
              entityName="terminals"
            />

          </Section>
        </>
      )}

      <RecordDrawer
        entity={propertyEntity as unknown as EntityMeta}
        row={drawer.row}
        mode={drawer.mode}
        open={drawer.open}
        defaults={{ type_id: id }}
        onOpenChange={(open) => setDrawer((current) => ({ ...current, open }))}
        onSaved={() => void load()}
      />
      <RecordDrawer
        entity={typeEntity}
        row={typeRow}
        mode="view"
        open={typeDrawerOpen}
        onOpenChange={setTypeDrawerOpen}
        onSaved={() => void load()}
      />
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
  onRowClick,
  entityName,
}: {
  fields: EntityMeta['fields'];
  rows: RowData[];
  loading: boolean;
  empty: string;
  onRowClick: (row: RowData) => void;
  entityName: string;
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
      <table className="w-full min-w-[640px] text-sm" data-entity={entityName}>
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
            <tr
              key={String(row.id ?? JSON.stringify(row))}
              tabIndex={0}
              aria-label="查看详情"
              onClick={() => onRowClick(row)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  onRowClick(row);
                }
              }}
              className="cursor-pointer border-b border-line last:border-b-0 hover:bg-surface-2 focus-visible:bg-surface-2"
            >
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
