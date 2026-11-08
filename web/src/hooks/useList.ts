import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchList } from '../api/client';
import type { ListKind } from '../api/client';
import type { CursorMeta, ListMeta, RowData } from '../api/types';
import { toCursorParams, toParams } from '../lib/query';
import type { QueryState } from '../lib/query';

export interface UseListResult {
  rows: RowData[];
  meta: ListMeta | null;
  cursor: CursorMeta | null;
  loading: boolean;
  error: string | null;
  reload: () => void;
  loadMore: () => void;
}

/** 快照页没有 id 列：id 优先，整行 JSON 兜底去重。 */
function rowKey(row: RowData): string {
  return row.id === undefined || row.id === null ? JSON.stringify(row) : String(row.id);
}

function mergeRows(prev: RowData[], next: RowData[]): RowData[] {
  const seen = new Set(prev.map(rowKey));
  const merged = [...prev];
  for (const row of next) {
    const key = rowKey(row);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(row);
  }
  return merged;
}

function messageOf(error: unknown): string {
  return error instanceof Error && error.message ? error.message : '请求失败，请重试';
}

/**
 * 列表数据源：分页/筛选/排序全交给服务端，query 变化自动重取。
 * cursorMode 时首屏不带 cursor，loadMore 累积 rows。
 */
export function useList(
  kind: ListKind,
  name: string,
  query: QueryState,
  cursorMode = false,
): UseListResult {
  const [rows, setRows] = useState<RowData[]>([]);
  const [meta, setMeta] = useState<ListMeta | null>(null);
  const [cursor, setCursor] = useState<CursorMeta | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  const queryRef = useRef(query);
  queryRef.current = query;
  const moreRef = useRef<AbortController | null>(null);
  const key = (cursorMode ? toCursorParams(query, null) : toParams(query)).toString();

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    fetchList(
      kind,
      name,
      cursorMode ? toCursorParams(queryRef.current, null) : toParams(queryRef.current),
      controller.signal,
    )
      .then((result) => {
        if (controller.signal.aborted) return;
        setRows(result.rows);
        setMeta(result.meta);
        setCursor(result.cursor ?? null);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setRows([]);
        setMeta(null);
        setCursor(null);
        setError(messageOf(err));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => {
      controller.abort();
      moreRef.current?.abort();
    };
  }, [kind, name, key, cursorMode, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  const loadMore = useCallback(() => {
    const next = cursor?.nextCursor;
    if (!cursorMode || !next) return;
    moreRef.current?.abort();
    const controller = new AbortController();
    moreRef.current = controller;
    setLoading(true);
    setError(null);
    fetchList(kind, name, toCursorParams(queryRef.current, next), controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setRows((prev) => mergeRows(prev, result.rows));
        setMeta(result.meta);
        setCursor(result.cursor ?? null);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(messageOf(err));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
  }, [cursorMode, cursor?.nextCursor, kind, name]);

  return { rows, meta, cursor, loading, error, reload, loadMore };
}
