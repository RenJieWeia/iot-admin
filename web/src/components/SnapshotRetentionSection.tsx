import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { ApiError, fetchList, updateRow } from '../api/client';
import { useMeta } from '../api/meta';
import type { RowData } from '../api/types';
import { Card, CardHeader } from './Card';
import { Button } from '@radix-ui/themes';

/** 只读源 `snapshot-settings`：三行固定（粗/分/秒），只能改 retention_days。 */
const SETTINGS_ENTITY = 'snapshot-settings';

/** 存储的中文名：表名是技术标识，界面给业务可读名。 */
const STORAGE_LABELS: Record<string, string> = {
  snapshot_coarse: '粗表（时 / 日 / 月 / 年）',
  snapshot_minute: '分表（分）',
  snapshot_second: '秒表（秒）',
};

function errorText(error: unknown): string {
  if (error instanceof ApiError) return error.detail;
  return String(error);
}

class PartialSaveError extends Error {
  constructor(
    readonly saved: string[],
    readonly failed: Array<{ table: string; detail: string }>,
  ) {
    super('partial_save');
  }
}

/**
 * 存储与保留：表级三行固定（不能增删），本地草稿聚合，一个「保存」批量提交改过的行。
 * 空值 = 永久保留；部分失败时成功的行清出草稿，失败的行保留并标红。
 */
export function SnapshotRetentionSection() {
  const meta = useMeta();
  const entity = meta.data?.subresources?.find((item) => item.name === SETTINGS_ENTITY);
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [failed, setFailed] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['list', SETTINGS_ENTITY, 'all'],
    queryFn: ({ signal }) => fetchList(SETTINGS_ENTITY, { pageSize: 50 }, signal),
    enabled: Boolean(entity),
  });

  const rows: RowData[] = list.data?.rows ?? [];
  const baselineOf = (row: RowData): string =>
    row.retention_days === null || row.retention_days === undefined ? '' : String(row.retention_days);
  const valueOf = (row: RowData): string => drafts[String(row.table_name ?? '')] ?? baselineOf(row);

  // 数据刷新后清掉与基线一致的草稿。
  useEffect(() => {
    if (!list.data) return;
    setDrafts((prev) => {
      const next: Record<string, string> = {};
      for (const [key, value] of Object.entries(prev)) {
        const row = (list.data?.rows ?? []).find((item) => String(item.table_name) === key);
        if (!row) continue;
        if (baselineOf(row) !== value) next[key] = value;
      }
      return next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [list.data]);

  const dirtyRows = rows.filter((row) => baselineOf(row) !== valueOf(row));

  const save = useMutation({
    mutationFn: async (changes: Array<{ table: string; days: number | null }>) => {
      const saved: string[] = [];
      const failures: Array<{ table: string; detail: string }> = [];
      for (const change of changes) {
        try {
          await updateRow(SETTINGS_ENTITY, change.table, { retention_days: change.days });
          saved.push(change.table);
        } catch (error) {
          failures.push({ table: change.table, detail: errorText(error) });
        }
      }
      if (failures.length > 0) throw new PartialSaveError(saved, failures);
      return saved;
    },
    onSuccess: (saved) => {
      setDrafts((prev) => {
        const next = { ...prev };
        for (const key of saved) delete next[key];
        return next;
      });
      setFailed({});
      setNotice('已保存保留期。');
      void queryClient.invalidateQueries({ queryKey: ['list', SETTINGS_ENTITY] });
    },
    onError: (error) => {
      if (error instanceof PartialSaveError) {
        setDrafts((prev) => {
          const next = { ...prev };
          for (const key of error.saved) delete next[key];
          return next;
        });
        const marks: Record<string, string> = {};
        for (const item of error.failed) marks[item.table] = item.detail;
        setFailed(marks);
        setNotice(
          `有 ${error.failed.length} 行没存上：${error.failed
            .map((item) => STORAGE_LABELS[item.table] ?? item.table)
            .join('、')}（其余 ${error.saved.length} 行已保存）`,
        );
        void queryClient.invalidateQueries({ queryKey: ['list', SETTINGS_ENTITY] });
        return;
      }
      setNotice(`保存失败：${errorText(error)}`);
    },
  });

  const submit = () => {
    const changes = dirtyRows.map((row) => {
      const raw = valueOf(row).trim();
      return { table: String(row.table_name ?? ''), raw };
    });
    const invalid = changes.find((item) => item.raw !== '' && (!/^\d+$/.test(item.raw) || Number(item.raw) <= 0));
    if (invalid) {
      setNotice('保留天数必须是正整数；留空表示永久保留。');
      return;
    }
    save.mutate(changes.map((item) => ({ table: item.table, days: item.raw === '' ? null : Number(item.raw) })));
  };

  const dirty = dirtyRows.length > 0;
  const failedKeys = new Set(Object.keys(failed));

  return (
    <Card label="保留期" tone={failedKeys.size > 0 ? 'warning' : 'default'}>
      <CardHeader
        title="保留期"
        description="表级配置（三行固定，不能增删）；留空表示永久保留"
        actions={
        <>
          {dirty && (
            <span className="text-sm text-warning" role="status">
              有未保存修改
            </span>
          )}
          <Button type="button" onClick={submit} disabled={!dirty || save.isPending} aria-busy={save.isPending} variant="surface" size="3">
            保存
          </Button>
        </>
        }
      />
      <p className="mb-2 mt-0 text-sm text-muted">
        最小清理粒度 = 整月（粗表 / 分表）、整日（秒表），实际保留可能比设定值更长。
      </p>

      {list.isError ? (
        <p className="m-0 text-negative-text" role="alert">
          保留期加载失败：{errorText(list.error)}
          <Button type="button" onClick={() => void list.refetch()} variant="ghost" size="3" className="h-10">
            重试
          </Button>
        </p>
      ) : list.isPending ? (
        <p className="m-0 text-sm text-muted" role="status">正在加载保留期…</p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((row) => {
            const table = String(row.table_name ?? '');
            const hasFailed = failedKeys.has(table);
            return (
              <div
                key={table}
                className={hasFailed ? 'flex flex-wrap items-center gap-3 text-negative-text' : 'flex flex-wrap items-center gap-3'}
                data-dirty={String(dirtyRows.some((item) => String(item.table_name) === table))}
              >
                <span className="w-52 text-base">{STORAGE_LABELS[table] ?? table}</span>
                <input
                  className="input w-40"
                  type="number"
                  min={1}
                  placeholder="永久保留"
                  aria-label={`${STORAGE_LABELS[table] ?? table} 保留天数`}
                  value={valueOf(row)}
                  onChange={(event) => {
                    setDrafts((prev) => ({ ...prev, [table]: event.target.value }));
                    setNotice(null);
                  }}
                />
                <span className="text-sm text-muted">天</span>
                {hasFailed && (
                  <span className="text-sm text-negative-text" role="alert">
                    未存上：{failed[table]}
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}

      {notice && (
        <p className="mt-2 text-sm" role="status">
          {notice}
        </p>
      )}
    </Card>
  );
}
