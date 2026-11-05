import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  allTerminals,
  deviceTypeOptions,
  exceptionEvents,
  metaFixture,
} from './fixtures';
import type { RowData } from '../api/types';

export interface WriteCall {
  method: string;
  path: string;
  body: unknown;
}

export const writeCalls: WriteCall[] = [];

export function resetWriteCalls() {
  writeCalls.length = 0;
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

  http.get('/admin/v1/options/:entity', ({ params }) => {
    if (params.entity === 'device-types') return ok(deviceTypeOptions);
    return ok([]);
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
