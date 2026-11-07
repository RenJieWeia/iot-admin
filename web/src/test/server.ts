import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  allDeviceTypes,
  allSnapshotBindings,
  allSnapshotSlots,
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

/** 绑定列表收到的查询串。 */
export const bindingQueries: string[] = [];

export function resetWriteCalls() {
  writeCalls.length = 0;
  optionQueries.length = 0;
  propertyQueries.length = 0;
  snapshotPlanQueries.length = 0;
  bindingQueries.length = 0;
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
    // 绑定表单的终端搜索按 ID 或名称匹配。
    rows = rows.filter((row) => String(row.name).includes(q) || String(row.id).includes(q));
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

/** 模拟后端校验：目标唯一 + 字段必须属于目标类型的物模型；返回 400 响应或 null。 */
function bindingFailure(body: RowData, selfID = '') {
  const codes = Array.isArray(body.item_codes) ? body.item_codes.map(String) : [];
  if (codes.length === 0) {
    return HttpResponse.json({ code: 400, message: '至少需要一个字段' }, { status: 400 });
  }
  const typeID =
    body.device_type_id != null
      ? String(body.device_type_id)
      : String(allTerminals.find((t) => t.id === body.terminal_id)?.type_id ?? '');
  const unknown = codes.filter(
    (code) => !allProperties.some((row) => row.type_id === typeID && row.item_code === code),
  );
  if (unknown.length > 0) {
    return HttpResponse.json(
      { code: 400, message: `以下字段不属于该终端类型：${unknown.join(', ')}` },
      { status: 400 },
    );
  }
  const duplicate = allSnapshotBindings.some(
    (row) =>
      String(row.id) !== selfID &&
      ((body.device_type_id != null && String(row.device_type_id) === String(body.device_type_id)) ||
        (body.terminal_id != null && String(row.terminal_id) === String(body.terminal_id))),
  );
  if (duplicate) {
    return HttpResponse.json(
      { code: 400, message: '该目标已有一条绑定，请编辑那一条' },
      { status: 400 },
    );
  }
  return null;
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

  // 全局六档配置（六行固定，PKey=granularity）。
  http.get('/admin/v1/snapshot-slots', () =>
    ok(allSnapshotSlots, { meta: { total: allSnapshotSlots.length, page: 1, pageSize: 10, sort: 'granularity', order: 'asc' } }),
  ),

  http.put('/admin/v1/snapshot-slots/:granularity', async ({ params, request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    writeCalls.push({ method: 'PUT', path: `snapshot-slots/${String(params.granularity)}`, body });
    if (body.enabled === true && (body.step === null || body.step === undefined)) {
      return HttpResponse.json({ code: 400, message: '启用档位必须给出步长' }, { status: 400 });
    }
    return ok({});
  }),

  // 绑定：一条 = 一个数据项；作用域二选一。
  http.get('/admin/v1/snapshot-bindings', ({ request }) => {
    const url = new URL(request.url);
    bindingQueries.push(url.search);
    const { slice, page, pageSize } = paginate(allSnapshotBindings as RowData[], url);
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: slice,
      ...envelopeMeta(allSnapshotBindings.length, page, pageSize, 'id', 'desc'),
    });
  }),

  http.post('/admin/v1/snapshot-bindings', async ({ request }) => {
    const body = (await request.json()) as RowData;
    writeCalls.push({ method: 'POST', path: 'snapshot-bindings', body });
    const failure = bindingFailure(body);
    if (failure) return failure;
    return ok({});
  }),

  http.put('/admin/v1/snapshot-bindings/:id', async ({ params, request }) => {
    const body = (await request.json()) as RowData;
    writeCalls.push({ method: 'PUT', path: `snapshot-bindings/${String(params.id)}`, body });
    const failure = bindingFailure(body, String(params.id));
    if (failure) return failure;
    return ok({});
  }),

  http.delete('/admin/v1/snapshot-bindings/:id', ({ params }) => {
    writeCalls.push({ method: 'DELETE', path: `snapshot-bindings/${String(params.id)}`, body: null });
    return ok(null);
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
