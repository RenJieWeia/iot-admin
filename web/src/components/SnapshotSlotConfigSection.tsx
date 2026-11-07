import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import { ApiError, fetchList, updateRow } from '../api/client';
import { useMeta } from '../api/meta';
import type { RowData } from '../api/types';
import { Card, CardHeader } from './Card';
import { Button } from '@radix-ui/themes';

const SLOTS_ENTITY = 'snapshot-slots';

/** 六档固定顺序（与迁移 0013/0014 的 granularity 取值一致）。 */
const ORDER: readonly string[] = ['second', 'minute', 'hour', 'day', 'month', 'year'];
const LABELS: Record<string, string> = {
  second: '秒', minute: '分', hour: '时', day: '日', month: '月', year: '年',
};

/** 兜底步长白名单（`_meta.enums` 未下发时使用）；正常路径以 `_meta.enums.<档>_step` 为准。 */
const FALLBACK_STEP_OPTIONS: Record<string, number[]> = {
  second: [1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30],
  minute: [1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30],
  hour: [1, 2, 3, 4, 6, 8, 12],
  day: [1],
  month: [1, 2, 3, 4, 6, 12],
  year: [1],
};

interface Draft {
  enabled: boolean;
  step: string;
}

interface Change {
  granularity: string;
  enabled: boolean;
  step: number | null;
}

function errorText(error: unknown): string {
  if (error instanceof ApiError) return error.detail;
  return String(error);
}

/** 一次批量保存的部分失败：成功的行已清出草稿，失败的行保留并标红。 */
class PartialSaveError extends Error {
  constructor(
    readonly saved: string[],
    readonly failed: Array<{ granularity: string; detail: string }>,
  ) {
    super('partial_save');
  }
}

/**
 * 六档配置：六行固定，本地草稿聚合，**一个「保存」批量提交本块全部改动**（逐行串行 PUT）。
 * - dirty 时标题旁显示「有未保存修改」，保存按钮才可用；
 * - 部分失败：成功的行清出草稿，失败的行保留并标红，提示里点名哪几行没存上。
 */
export function SnapshotSlotConfigSection() {
  const meta = useMeta();
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [failed, setFailed] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState<string | null>(null);

  const entity =
    meta.data?.entities.find((item) => item.name === SLOTS_ENTITY) ??
    meta.data?.subresources?.find((item) => item.name === SLOTS_ENTITY);

  const list = useQuery({
    queryKey: ['list', SLOTS_ENTITY, 'all'],
    queryFn: ({ signal }) => fetchList(SLOTS_ENTITY, { pageSize: 10 }, signal),
    enabled: Boolean(entity),
  });

  const rows = useMemo(() => {
    const source = list.data?.rows ?? [];
    return [...source].sort((a, b) => ORDER.indexOf(String(a.granularity)) - ORDER.indexOf(String(b.granularity)));
  }, [list.data]);

  /** 已加载行 → 基线（用于判断 dirty）。 */
  const baselineOf = (row: RowData): Draft => ({
    enabled: String(row.enabled) === 'true',
    step: row.step === null || row.step === undefined ? '' : String(row.step),
  });
  const draftOf = (row: RowData): Draft => drafts[String(row.granularity)] ?? baselineOf(row);

  // 数据刷新后清掉与基线一致的草稿，避免「保存过了还显示未保存」。
  useEffect(() => {
    if (!list.data) return;
    setDrafts((prev) => {
      const next: Record<string, Draft> = {};
      for (const [key, draft] of Object.entries(prev)) {
        const row = (list.data?.rows ?? []).find((item) => String(item.granularity) === key);
        if (!row) continue;
        const base = baselineOf(row);
        if (base.enabled !== draft.enabled || base.step !== draft.step) next[key] = draft;
      }
      return next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [list.data]);

  const dirtyRows = rows.filter((row) => {
    const base = baselineOf(row);
    const draft = draftOf(row);
    return base.enabled !== draft.enabled || base.step !== draft.step;
  });

  const save = useMutation({
    mutationFn: async (changes: Change[]) => {
      const saved: string[] = [];
      const failures: Array<{ granularity: string; detail: string }> = [];
      for (const change of changes) {
        try {
          await updateRow(SLOTS_ENTITY, change.granularity, {
            enabled: change.enabled,
            step: change.step,
          });
          saved.push(change.granularity);
        } catch (error) {
          failures.push({ granularity: change.granularity, detail: errorText(error) });
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
      setNotice('已保存六档配置。');
      void queryClient.invalidateQueries({ queryKey: ['list', SLOTS_ENTITY] });
    },
    onError: (error) => {
      if (error instanceof PartialSaveError) {
        setDrafts((prev) => {
          const next = { ...prev };
          for (const key of error.saved) delete next[key];
          return next;
        });
        const marks: Record<string, string> = {};
        for (const item of error.failed) marks[item.granularity] = item.detail;
        setFailed(marks);
        setNotice(
          `有 ${error.failed.length} 行没存上：${error.failed
            .map((item) => `${LABELS[item.granularity] ?? item.granularity}档`)
            .join('、')}（其余 ${error.saved.length} 行已保存）`,
        );
        void queryClient.invalidateQueries({ queryKey: ['list', SLOTS_ENTITY] });
        return;
      }
      setNotice(`保存失败：${errorText(error)}`);
    },
  });

  const stepOptionsOf = (granularity: string): Array<{ value: string; label: string }> => {
    const dict = meta.data?.enums?.[`${granularity}_step`];
    if (dict && Object.keys(dict).length > 0) {
      return Object.entries(dict)
        .map(([value, label]) => ({ value, label }))
        .sort((a, b) => Number(a.value) - Number(b.value));
    }
    return (FALLBACK_STEP_OPTIONS[granularity] ?? []).map((value) => ({
      value: String(value),
      label: `${value} ${LABELS[granularity] ?? ''}`,
    }));
  };

  const updateDraft = (row: RowData, patch: Partial<Draft>) => {
    const key = String(row.granularity);
    setDrafts((prev) => ({ ...prev, [key]: { ...(prev[key] ?? baselineOf(row)), ...patch } }));
    setNotice(null);
  };

  const submit = () => {
    const changes: Change[] = dirtyRows
      .map((row) => {
        const draft = draftOf(row);
        return {
          granularity: String(row.granularity),
          enabled: draft.enabled,
          step: draft.enabled && draft.step !== '' ? Number(draft.step) : null,
        };
      })
      .filter((change) => !(change.enabled && change.step === null));
    if (changes.length !== dirtyRows.length) {
      setNotice('启用档位必须选择步长：请先补全再保存。');
      return;
    }
    save.mutate(changes);
  };

  const dirty = dirtyRows.length > 0;
  const failedKeys = new Set(Object.keys(failed));

  return (
    <Card label="六档配置" tone={failedKeys.size > 0 ? 'warning' : 'default'}>
      <CardHeader
        title="六档配置"
        description="六行固定，保存后全站生效（不用的档位关掉即可）"
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
      {list.isError ? (
        <p className="m-0 text-negative-text" role="alert">
          六档配置加载失败：{errorText(list.error)}
          <Button type="button" onClick={() => void list.refetch()} variant="ghost" size="3" className="h-10">
            重试
          </Button>
        </p>
      ) : list.isPending ? (
        <p className="m-0 text-sm text-muted" role="status">正在加载六档配置…</p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((row) => {
            const granularity = String(row.granularity);
            const draft = draftOf(row);
            const hasFailed = failedKeys.has(granularity);
            return (
              <div
                key={granularity}
                className={hasFailed ? 'flex flex-wrap items-center gap-3 rounded-sm px-1 text-negative-text' : 'flex flex-wrap items-center gap-3 px-1'}
                data-dirty={String(dirtyRows.some((item) => String(item.granularity) === granularity))}
              >
                <span className="w-16 text-base">{LABELS[granularity] ?? granularity}</span>
                <label className="inline-flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    aria-label={`${LABELS[granularity]}档启用`}
                    checked={draft.enabled}
                    onChange={(event) => updateDraft(row, { enabled: event.target.checked })}
                  />
                  启用
                </label>
                <select
                  className="select"
                  aria-label={`${LABELS[granularity]}档步长`}
                  value={draft.step}
                  disabled={!draft.enabled}
                  onChange={(event) => updateDraft(row, { step: event.target.value })}
                >
                  <option value="">选择步长</option>
                  {stepOptionsOf(granularity).map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
                {hasFailed && (
                  <span className="text-sm text-negative-text" role="alert">
                    未存上：{failed[granularity]}
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
