import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { fetchList, updateRow } from '../api/client';
import { useMeta } from '../api/meta';
import type { RowData } from '../api/types';

/** 只读源 `snapshot-settings`：三行固定（粗/分/秒），只能改 retention_days。 */
const SETTINGS_ENTITY = 'snapshot-settings';

/** 存储的中文名：表名是技术标识，界面给业务可读名。 */
const STORAGE_LABELS: Record<string, string> = {
  snapshot_coarse: '粗表（时 / 日 / 月 / 年）',
  snapshot_minute: '分表（分）',
  snapshot_second: '秒表（秒）',
};

function errorText(error: unknown): string | null {
  if (!error) return null;
  if (typeof error === 'object' && error !== null && 'message' in error) {
    return String((error as { message: unknown }).message);
  }
  return String(error);
}

/**
 * 存储与保留区块（快照计划页顶部）：
 * - 保留期是**表级**配置（三行固定，行集由迁移预置），不随档位变化，所以不放进档位列表；
 * - 服务端禁止新增/删除行（POST/DELETE → 400 fixed_rows），这里只提供「改值」；
 * - 空值 = 永久保留。
 */
export function SnapshotRetentionSection() {
  const meta = useMeta();
  const entity = meta.data?.subresources?.find((item) => item.name === SETTINGS_ENTITY);
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['list', SETTINGS_ENTITY, 'all'],
    queryFn: ({ signal }) => fetchList(SETTINGS_ENTITY, { pageSize: 50 }, signal),
    enabled: Boolean(entity),
  });

  const save = useMutation({
    mutationFn: (input: { table: string; days: number | null }) =>
      updateRow(SETTINGS_ENTITY, input.table, { retention_days: input.days }),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['list', SETTINGS_ENTITY] });
    },
  });

  const rows: RowData[] = list.data?.rows ?? [];

  const valueOf = (row: RowData): string => {
    const table = String(row.table_name ?? '');
    if (table in drafts) return drafts[table] ?? '';
    return row.retention_days === null || row.retention_days === undefined ? '' : String(row.retention_days);
  };

  const submit = (row: RowData) => {
    const table = String(row.table_name ?? '');
    const raw = valueOf(row).trim();
    if (raw === '') {
      save.mutate({ table, days: null });
      return;
    }
    if (!/^\d+$/.test(raw) || Number(raw) <= 0) {
      setError('保留天数必须是正整数；留空表示永久保留');
      return;
    }
    setError(null);
    save.mutate({ table, days: Number(raw) });
  };

  return (
    <section
      className="mb-4 rounded-md border border-border bg-surface p-4"
      aria-label="存储与保留"
    >
      <h2 className="m-0 text-lg">存储与保留</h2>
      <p className="mb-3 mt-1 text-meta text-muted">
        表级配置（三行固定，不能增删）；留空表示永久保留。档位步长在下方计划里按序列配置。
      </p>

      {list.isError ? (
        <p className="m-0 text-negative-text" role="alert">
          保留期加载失败：{errorText(list.error) ?? '未知错误'}
          <button type="button" className="btn btn-link" onClick={() => void list.refetch()}>
            重试
          </button>
        </p>
      ) : list.isPending ? (
        <p className="m-0 text-meta text-muted" role="status">
          正在加载保留期…
        </p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((row) => {
            const table = String(row.table_name ?? '');
            const busy = save.isPending && save.variables?.table === table;
            return (
              <div key={table} className="flex flex-wrap items-center gap-3">
                <span className="w-52 text-body">{STORAGE_LABELS[table] ?? table}</span>
                <input
                  className="input h-8 w-40"
                  type="number"
                  min={1}
                  placeholder="永久保留"
                  aria-label={`${STORAGE_LABELS[table] ?? table} 保留天数`}
                  value={valueOf(row)}
                  onChange={(event) => setDrafts((prev) => ({ ...prev, [table]: event.target.value }))}
                />
                <span className="text-meta text-muted">天</span>
                <button
                  type="button"
                  className="btn h-8"
                  onClick={() => submit(row)}
                  disabled={busy}
                  aria-busy={busy}
                >
                  保存
                </button>
              </div>
            );
          })}
        </div>
      )}

      {error && (
        <p className="mt-2 text-negative-text" role="alert">
          {error}
        </p>
      )}
      {save.isError && (
        <p className="mt-2 text-negative-text" role="alert">
          保存失败：{errorText(save.error)}
        </p>
      )}
    </section>
  );
}
