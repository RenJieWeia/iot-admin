import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { ApiError, fetchList, updateRow } from '../api/client';
import { useMeta } from '../api/meta';
import type { RowData } from '../api/types';

const SLOTS_ENTITY = 'snapshot-slots';

/** 六档固定顺序（与迁移 0013 的 granularity 取值一致）。 */
const ORDER: readonly string[] = ['second', 'minute', 'hour', 'day', 'month', 'year'];
const LABELS: Record<string, string> = {
  second: '秒', minute: '分', hour: '时', day: '日', month: '月', year: '年',
};

/**
 * 六档步长白名单：与迁移 0013 的 CHECK 同源。
 * 档位是「六行固定」，所以这里是常量而不是可增删列表；后端 POST/DELETE 会被 400。
 */
/**
 * 兜底步长白名单（`_meta.enums` 未下发时使用）。正常路径以 `_meta.enums.<档>_step` 为准：
 * 这样改 0013 的 CHECK 时只改后端，不用同步前端常量。
 */
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

function errorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) return error.detail;
  return String(error);
}

/**
 * 全局六档配置：六行固定，每行「启用 + 步长」，保存后全站生效。
 * 不提供新增/删除档位的入口（档位是固定枚举，不是可增长的行集）。
 */
export function SnapshotSlotConfigSection() {
  const meta = useMeta();
  // 六档配置现在是导航实体（snapshot-slots），也兼容以子资源下发的形态。
  const entity =
    meta.data?.entities.find((item) => item.name === SLOTS_ENTITY) ??
    meta.data?.subresources?.find((item) => item.name === SLOTS_ENTITY);
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [error, setError] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ['list', SLOTS_ENTITY, 'all'],
    queryFn: ({ signal }) => fetchList(SLOTS_ENTITY, { pageSize: 10 }, signal),
    enabled: Boolean(entity),
  });

  const save = useMutation({
    mutationFn: (input: { granularity: string; enabled: boolean; step: number | null }) =>
      updateRow(SLOTS_ENTITY, input.granularity, { enabled: input.enabled, step: input.step }),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ['list', SLOTS_ENTITY] });
    },
  });

  /** 步长选项：优先用 _meta.enums[`<granularity>_step`]，缺失时回落到本地常量。 */
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

  const rows = useMemo(() => {
    const list1 = list.data?.rows ?? [];
    return [...list1].sort((a, b) => ORDER.indexOf(String(a.granularity)) - ORDER.indexOf(String(b.granularity)));
  }, [list.data]);

  const draftOf = (row: RowData): Draft => {
    const key = String(row.granularity);
    if (drafts[key]) return drafts[key]!;
    return {
      enabled: String(row.enabled) === 'true',
      step: row.step === null || row.step === undefined ? '' : String(row.step),
    };
  };

  const submit = (row: RowData) => {
    const granularity = String(row.granularity);
    const draft = draftOf(row);
    if (!ORDER.includes(granularity)) return;
    if (draft.enabled && draft.step === '') {
      setError(`${LABELS[granularity]}档启用时必须选择步长`);
      return;
    }
    setError(null);
    save.mutate({ granularity, enabled: draft.enabled, step: draft.enabled ? Number(draft.step) : null });
  };

  return (
    <section className="mb-4 rounded-md border border-border bg-surface p-4" aria-label="六档配置">
      <h2 className="m-0 text-lg">六档配置</h2>
      <p className="mb-3 mt-1 text-meta text-muted">
        六行固定（秒 / 分 / 时 / 日 / 月 / 年），保存后全站生效；档位不可增删，不用的档位把「启用」关掉即可。
      </p>

      {list.isError ? (
        <p className="m-0 text-negative-text" role="alert">
          六档配置加载失败：{errorText(list.error) ?? '未知错误'}
          <button type="button" className="btn btn-link" onClick={() => void list.refetch()}>
            重试
          </button>
        </p>
      ) : list.isPending ? (
        <p className="m-0 text-meta text-muted" role="status">正在加载六档配置…</p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((row) => {
            const granularity = String(row.granularity);
            const draft = draftOf(row);
            const busy = save.isPending && save.variables?.granularity === granularity;
            return (
              <div key={granularity} className="flex flex-wrap items-center gap-3">
                <span className="w-16 text-body">{LABELS[granularity] ?? granularity}</span>
                <label className="inline-flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    aria-label={`${LABELS[granularity]}档启用`}
                    checked={draft.enabled}
                    onChange={(event) =>
                      setDrafts((prev) => ({ ...prev, [granularity]: { ...draft, enabled: event.target.checked } }))
                    }
                  />
                  启用
                </label>
                <select
                  className="select h-8"
                  aria-label={`${LABELS[granularity]}档步长`}
                  value={draft.step}
                  disabled={!draft.enabled}
                  onChange={(event) =>
                    setDrafts((prev) => ({ ...prev, [granularity]: { ...draft, step: event.target.value } }))
                  }
                >
                  <option value="">选择步长</option>
                  {stepOptionsOf(granularity).map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
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
