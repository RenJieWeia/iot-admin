import { useQuery } from '@tanstack/react-query';
import { ApiError } from './client';
import type { Envelope, FieldMeta, MetaPayload, Option } from './types';

/**
 * 字段元数据端点（R-019 / task-004 §4）：
 *   GET /admin/v1/_meta → { code, message, data: { entities, exceptions, enums, booleans } }
 * 前端一切中文标签、列、枚举、筛选能力都从这里来，页面不硬编码业务词。
 */
export const META_QUERY_KEY = ['meta'] as const;

/**
 * 把顶层 enums/booleans 字典归一到每个字段的 options 上，组件层只认 field.options，
 * 避免每个渲染点都要查两套字典。
 */
export function normalizeMeta(payload: MetaPayload): MetaPayload {
  const booleanOptions: Option[] = Object.entries(payload.booleans ?? {}).map(([value, label]) => ({
    value,
    label,
  }));

  const normalizeFields = (fields: FieldMeta[]): FieldMeta[] =>
    fields.map((field) => {
      if (field.options && field.options.length > 0) return field;
      if (field.type === 'boolean' && booleanOptions.length > 0) {
        return { ...field, options: booleanOptions };
      }
      const dict = payload.enums?.[field.key];
      if (dict) {
        return {
          ...field,
          options: Object.entries(dict).map(([value, label]) => ({ value, label })),
        };
      }
      return field;
    });

  return {
    entities: (payload.entities ?? []).map((entity) => ({
      ...entity,
      fields: normalizeFields(entity.fields ?? []),
    })),
    exceptions: (payload.exceptions ?? []).map((slot) => ({
      ...slot,
      fields: normalizeFields(slot.fields ?? []),
    })),
    enums: payload.enums ?? {},
    booleans: payload.booleans ?? {},
    pagination: payload.pagination ?? { default: 10, max: 50, options: [10, 20, 50] },
  };
}

export async function fetchMeta(signal?: AbortSignal): Promise<MetaPayload> {
  const response = await fetch('/admin/v1/_meta', {
    headers: { Accept: 'application/json' },
    signal,
  });
  let payload: Envelope<MetaPayload> | null = null;
  try {
    payload = (await response.json()) as Envelope<MetaPayload>;
  } catch {
    payload = null;
  }
  if (!response.ok || !payload || payload.code !== 0 || !payload.data) {
    throw new ApiError(
      response.status,
      'meta_unavailable',
      payload?.message || `字段元数据加载失败（HTTP ${response.status}）`,
    );
  }
  return normalizeMeta(payload.data);
}

/** 元数据在会话内基本不变；失败只重试一次，之后由错误态提供手动重试。 */
export function useMeta() {
  return useQuery({
    queryKey: META_QUERY_KEY,
    queryFn: ({ signal }) => fetchMeta(signal),
    staleTime: 5 * 60 * 1000,
    retry: 1,
  });
}
