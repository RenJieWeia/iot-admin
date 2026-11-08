import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { fetchMeta, fetchOptions } from './api/client';
import type { FieldRef, MetaPayload } from './api/types';

interface MetaState {
  meta: MetaPayload | null;
  loading: boolean;
  error: string | null;
  reload: () => void;
}

const MetaContext = createContext<MetaState | null>(null);

export function MetaProvider({ children }: { children: ReactNode }) {
  const [meta, setMeta] = useState<MetaPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    fetchMeta()
      .then((payload) => {
        if (!controller.signal.aborted) setMeta(payload);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          setError(err instanceof Error ? err.message : '元数据加载失败');
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  const value = useMemo(() => ({ meta, loading, error, reload }), [meta, loading, error, reload]);
  return <MetaContext.Provider value={value}>{children}</MetaContext.Provider>;
}

export function useMetaState(): MetaState {
  const state = useContext(MetaContext);
  if (!state) throw new Error('useMetaState 必须在 MetaProvider 内使用');
  return state;
}

export function useMeta(): MetaPayload {
  const { meta } = useMetaState();
  if (!meta) throw new Error('元数据尚未加载');
  return meta;
}

/** 关联字段 id → 展示名：开屏按实体批量取一次（上限 1000 行），够列表列回填。 */
export function useRelationLabels(): (ref: FieldRef | undefined, value: unknown) => string {
  const { meta } = useMetaState();
  const [labels, setLabels] = useState<Record<string, Record<string, string>>>({});

  useEffect(() => {
    if (!meta) return;
    const targets = new Set<string>();
    for (const page of [...meta.entities, ...(meta.subresources ?? [])]) {
      for (const field of page.fields) {
        if (field.ref) targets.add(field.ref.entity);
      }
    }
    if (!targets.size) return;
    const controller = new AbortController();
    Promise.all(
      [...targets].map(async (entity) => {
        try {
          const options = await fetchOptions(entity, { limit: 1000 }, controller.signal);
          return [entity, Object.fromEntries(options.map((o) => [o.value, o.label]))] as const;
        } catch {
          return [entity, {}] as const;
        }
      }),
    ).then((entries) => {
      if (!controller.signal.aborted) setLabels(Object.fromEntries(entries));
    });
    return () => controller.abort();
  }, [meta]);

  return useCallback(
    (ref, value) => {
      if (value === null || value === undefined || value === '') return '—';
      const raw = String(value);
      return labels[ref?.entity ?? '']?.[raw] ?? raw;
    },
    [labels],
  );
}
