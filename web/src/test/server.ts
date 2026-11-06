import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  allTerminals,
  deviceTypeOptions,
  exceptionEvents,
  latestValues,
  metaFixture,
  snapshotValues,
  stagingValues,
} from './fixtures';
import type { RowData } from '../api/types';

export interface WriteCall {
  method: string;
  path: string;
  body: unknown;
}

export const writeCalls: WriteCall[] = [];

/** options 端点收到的查询串（用于断言防抖/搜索参数）。 */
export const optionQueries: string[] = [];

export function resetWriteCalls() {
  writeCalls.length = 0;
  optionQueries.length = 0;
}

const ok = <T>(data: T, extra: Record<string, unknown> = {}) =>
  HttpResponse.json({ code: 0, message: 'ok', data, ...extra });

const envelopeMeta = (total: number, page: number, pageSize: number, sort: string, order: string) => ({
  meta: { total, page, pageSize, sort, order },
});

function paginate(rows: RowData[], url: URL) {
  const page = Number(url.searchParams.get('page') ?? '1') || 1;
  const pageSize = Number(url.searchParams.get('pageSize') ?? '10') || 10;
  const start = (page - 1) * pageSize;
  return { slice: rows.slice(start, start + pageSize), page, pageSize };
}

function filterTerminals(url: URL) {
  let rows = [...allTerminals];
  const q = (url.searchParams.get('q') ?? '').trim();
  if (q) {
    rows = rows.filter((row) => String(row.name).includes(q));
  }
  const enabled = url.searchParams.getAll('filter.enabled.eq');
  if (enabled.length > 0) {
    rows = rows.filter((row) => enabled.includes(String(row.enabled)));
  }
  const prefix = url.searchParams.getAll('filter.id.prefix');
  if (prefix.length > 0) {
    rows = rows.filter((row) => prefix.some((p) => String(row.id).startsWith(p)));
  }
  return rows;
}

export const server = setupServer(
  http.get('/admin/v1/_meta', () => ok(metaFixture)),

  // task-012 F4：支持 ?q=（label 模糊）与 ?ids=（精确取 label），两者互斥由后端保证。
  http.get('/admin/v1/options/:entity', ({ params, request }) => {
    const url = new URL(request.url);
    optionQueries.push(url.search);
    if (params.entity !== 'device-types') return ok([]);
    const ids = url.searchParams.get('ids');
    if (ids) {
      const wanted = ids.split(',').map((id) => id.trim());
      return ok(deviceTypeOptions.filter((option) => wanted.includes(option.value)));
    }
    const q = (url.searchParams.get('q') ?? '').trim();
    if (q) {
      return ok(deviceTypeOptions.filter((option) => option.label.includes(q)));
    }
    return ok(deviceTypeOptions);
  }),

  // 小时快照（task-010）：游标分页，meta 只有 hasMore/nextCursor/limit。
  http.get('/admin/v1/readonly/hourly-snapshot', ({ request }) => {
    const url = new URL(request.url);
    const cursor = url.searchParams.get('cursor') ?? '';
    const limit = Math.min(Number(url.searchParams.get('limit') ?? '20') || 20, 1000);

    let rows = [...snapshotValues] as RowData[];
    const revisionGte = url.searchParams.getAll('filter.revision.gte');
    if (revisionGte.length > 0) {
      const threshold = Number(revisionGte[0] ?? '0');
      rows = rows.filter((row) => Number(row.revision) >= threshold);
    }
    const qualityGte = url.searchParams.getAll('filter.quality.gte');
    if (qualityGte.length > 0) {
      const threshold = Number(qualityGte[0] ?? '0');
      rows = rows.filter((row) => Number(row.quality) >= threshold);
    }

    let offset = 0;
    if (cursor) {
      const matched = /^off:(\d+)$/.exec(cursor);
      if (!matched) {
        return HttpResponse.json({ code: 400, message: 'invalid_cursor' }, { status: 400 });
      }
      offset = Number(matched[1]);
    }

    const slice = rows.slice(offset, offset + limit);
    const nextOffset = offset + slice.length;
    const hasMore = nextOffset < rows.length;
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      meta: { hasMore, nextCursor: hasMore ? `off:${nextOffset}` : null, limit },
    });
  }),

  // 通用只读源：latest-values（offset）、hourly-staging（offset）。
  http.get('/admin/v1/readonly/:slot', ({ params, request }) => {
    const url = new URL(request.url);
    if (params.slot !== 'latest-values' && params.slot !== 'hourly-staging') {
      return HttpResponse.json({ code: 404, message: 'unknown_slot' }, { status: 404 });
    }
    let rows = (params.slot === 'latest-values' ? [...latestValues] : [...stagingValues]) as RowData[];
    const qualityGte = url.searchParams.getAll('filter.quality.gte');
    if (qualityGte.length > 0) {
      const threshold = Number(qualityGte[0] ?? '0');
      rows = rows.filter((row) => Number(row.quality) >= threshold);
    }
    const terminal = url.searchParams.getAll('filter.terminal_id.contains');
    if (terminal.length > 0) {
      rows = rows.filter((row) =>
        terminal.some((needle) => String(row.terminal_id).includes(needle)),
      );
    }
    const q = (url.searchParams.get('q') ?? '').trim();
    if (q) {
      rows = rows.filter(
        (row) =>
          String(row.terminal_id).includes(q) ||
          String(row.item_code).includes(q) ||
          String(row.quality_reason ?? '').includes(q),
      );
    }
    const { slice, page, pageSize } = paginate(rows, url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(rows.length, page, pageSize, 'value_time', 'desc'),
    });
  }),

  http.get('/admin/v1/exceptions/:slot', ({ params, request }) => {
    const url = new URL(request.url);
    if (params.slot !== 'events' && params.slot !== 'intervals') {
      return HttpResponse.json({ code: 404, message: 'unknown_slot' }, { status: 404 });
    }
    const rows = params.slot === 'events' ? (exceptionEvents as RowData[]) : [];
    const { slice, page, pageSize } = paginate(rows, url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(rows.length, page, pageSize, 'id', 'desc'),
    });
  }),

  http.get('/admin/v1/terminals/:id', ({ params }) => {
    const row = allTerminals.find((item) => item.id === params.id);
    if (!row) return HttpResponse.json({ code: 404, message: 'not_found' }, { status: 404 });
    return ok(row);
  }),

  http.get('/admin/v1/:entity', ({ params, request }) => {
    const url = new URL(request.url);
    if (params.entity !== 'terminals') {
      return HttpResponse.json({
        code: 0,
        message: 'ok',
        data: [],
        ...envelopeMeta(0, 1, 10, 'id', 'desc'),
      });
    }
    const filtered = filterTerminals(url);
    const { slice, page, pageSize } = paginate(filtered, url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(filtered.length, page, pageSize, 'id', 'desc'),
    });
  }),

  http.post('/admin/v1/:entity', async ({ params, request }) => {
    const body = await request.json();
    writeCalls.push({ method: 'POST', path: String(params.entity), body });
    return ok({});
  }),

  http.put('/admin/v1/:entity/:id', async ({ params, request }) => {
    const body = await request.json();
    writeCalls.push({ method: 'PUT', path: `${String(params.entity)}/${String(params.id)}`, body });
    return ok({});
  }),

  http.delete('/admin/v1/:entity/:id', ({ params }) => {
    writeCalls.push({
      method: 'DELETE',
      path: `${String(params.entity)}/${String(params.id)}`,
      body: null,
    });
    return ok(null);
  }),
);
