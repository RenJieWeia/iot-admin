import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  allDeviceTypes,
  snapshotCoarseRows,
  snapshotMinuteRows,
  snapshotSecondRows,
  allSnapshotPlans,
  allSnapshotSettings,
  allProperties,
  allTerminals,
  deviceTypeOptions,
  exceptionEvents,
  latestValues,
  metaFixture,
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

/** 终端属性列表收到的查询串（用于断言详情页固定注入 type_id）。 */
export const propertyQueries: string[] = [];

/** 快照计划列表收到的查询串。 */
export const snapshotPlanQueries: string[] = [];

export function resetWriteCalls() {
  writeCalls.length = 0;
  optionQueries.length = 0;
  propertyQueries.length = 0;
  snapshotPlanQueries.length = 0;
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

  // 通用只读源：latest-values（offset）。
  http.get('/admin/v1/readonly/:slot', ({ params, request }) => {
    const url = new URL(request.url);

    // 快照三存储：游标分页（无 total），粗表支持档位过滤 filter.granularity.in。
    const snapshotRows: Record<string, RowData[]> = {
      'snapshot-coarse': snapshotCoarseRows as RowData[],
      'snapshot-minute': snapshotMinuteRows as RowData[],
      'snapshot-second': snapshotSecondRows as RowData[],
    };
    const snapshot = snapshotRows[String(params.slot)];
    if (snapshot) {
      let rows = [...snapshot];
      const granularities = url.searchParams.getAll('filter.granularity.in');
      if (granularities.length > 0) {
        rows = rows.filter((row) => granularities.includes(String(row.granularity)));
      }
      const limit = Math.min(Number(url.searchParams.get('limit') ?? '10') || 10, 100);
      const cursor = url.searchParams.get('cursor') ?? '';
      let offset = 0;
      if (cursor) {
        const matched = /^off:(\d+)$/.exec(cursor);
        if (!matched) return HttpResponse.json({ code: 400, message: 'invalid_cursor' }, { status: 400 });
        offset = Number(matched[1]);
      }
      const slice = rows.slice(offset, offset + limit);
      const nextOffset = offset + slice.length;
      const hasMore = nextOffset < rows.length;
      return HttpResponse.json({
        code: 0,
        message: 'ok',
        data: slice,
        // 游标页没有 total：count(*) 在分区表上会随月退化。
        meta: { hasMore, nextCursor: hasMore ? `off:${nextOffset}` : null, limit },
      });
    }

    if (params.slot !== 'latest-values') {
      return HttpResponse.json({ code: 404, message: 'unknown_slot' }, { status: 404 });
    }
    let rows = [...latestValues] as RowData[];
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
    if (params.slot !== 'events') {
      return HttpResponse.json({ code: 404, message: 'unknown_slot' }, { status: 404 });
    }
    const rows = exceptionEvents as RowData[];
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

  // 终端类型 / 终端属性（task-016 详情页）：属性列表按固定 type_id 过滤。
  http.get('/admin/v1/device-types', ({ request }) => {
    const url = new URL(request.url);
    const { slice, page, pageSize } = paginate(allDeviceTypes as RowData[], url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(allDeviceTypes.length, page, pageSize, 'id', 'desc'),
    });
  }),

  http.get('/admin/v1/device-types/:id', ({ params }) => {
    const row = allDeviceTypes.find((item) => item.id === params.id);
    if (!row) return HttpResponse.json({ code: 404, message: 'not_found' }, { status: 404 });
    return ok(row);
  }),

  http.get('/admin/v1/thing-model-properties', ({ request }) => {
    const url = new URL(request.url);
    propertyQueries.push(url.search);
    // 按后端真实白名单收紧：type_id 是 relation 字段，只支持 in（裸字段按字段声明的第一个操作符）。
    // 其它操作符（如 eq）后端会 400，这里必须同样 400，否则测试会放过后端不认的请求。
    for (const key of url.searchParams.keys()) {
      if (!key.startsWith('filter.type_id')) continue;
      if (key !== 'filter.type_id' && key !== 'filter.type_id.in') {
        const op = key.slice('filter.type_id.'.length);
        return HttpResponse.json(
          { code: 400, message: `invalid_query: 字段 \"type_id\" 不支持操作符 \"${op}\"` },
          { status: 400 },
        );
      }
    }
    const typeIDs = [
      ...url.searchParams.getAll('filter.type_id.in'),
      ...url.searchParams.getAll('filter.type_id'),
    ];
    const rows = (typeIDs.length > 0
      ? allProperties.filter((row) => typeIDs.includes(row.type_id))
      : [...allProperties]) as RowData[];
    const { slice, page, pageSize } = paginate(rows, url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(rows.length, page, pageSize, 'item_code', 'asc'),
    });
  }),

  http.get('/admin/v1/thing-model-properties/:id', ({ params }) => {
    const row = allProperties.find((item) => item.id === params.id);
    if (!row) return HttpResponse.json({ code: 404, message: 'not_found' }, { status: 404 });
    return ok(row);
  }),

  // 快照计划 / 表级保留期（B4）：保留期只允许 PUT，禁增删。
  http.get('/admin/v1/snapshot-plans', ({ request }) => {
    const url = new URL(request.url);
    snapshotPlanQueries.push(url.search);
    const { slice, page, pageSize } = paginate(allSnapshotPlans as RowData[], url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(allSnapshotPlans.length, page, pageSize, 'id', 'desc'),
    });
  }),

  http.get('/admin/v1/snapshot-plans/:id', ({ params }) => {
    const row = allSnapshotPlans.find((item) => String(item.id) === params.id);
    if (!row) return HttpResponse.json({ code: 404, message: 'not_found' }, { status: 404 });
    return ok(row);
  }),

  http.get('/admin/v1/snapshot-settings', () =>
    ok(allSnapshotSettings, { meta: { total: allSnapshotSettings.length, page: 1, pageSize: 50, sort: 'updated_at', order: 'desc' } }),
  ),

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
