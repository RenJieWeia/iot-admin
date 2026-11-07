import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { metaFixture } from '../test/fixtures';
import { server, writeCalls } from '../test/server';

async function waitForPage() {
  await screen.findByRole('region', { name: '六档配置' }, { timeout: 5000 });
  await waitFor(() => expect(screen.queryByText('正在加载六档配置…')).not.toBeInTheDocument());
}

/** 打开绑定抽屉（专用表单，不是通用 RecordDrawer）。 */
async function openBindingForm() {
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: '新建绑定' }));
  return { user, dialog: await screen.findByRole('dialog') };
}

describe('快照配置页（六档 / 保留期 / 绑定 三段式）', () => {
  it('六档配置渲染六行固定档位，改开关+步长后发 PUT；没有「新增档位」入口', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    const section = screen.getByRole('region', { name: '六档配置' });
    for (const label of ['秒', '分', '时', '日', '月', '年']) {
      expect(within(section).getByLabelText(`${label}档启用`)).toBeInTheDocument();
    }
    // 步长选项优先读 _meta.enums（夹具里 30 秒的文案带「（_meta）」后缀）。
    expect(within(section).getByRole('option', { name: '30 秒（_meta）' })).toBeInTheDocument();
    // 档位是固定六行：不给新增入口。
    expect(within(section).queryByRole('button', { name: /新增档位/ })).not.toBeInTheDocument();

    // 秒档默认关闭：打开并选 30 秒 → PUT snapshot-slots/second。
    await user.click(within(section).getByLabelText('秒档启用'));
    await user.selectOptions(within(section).getByLabelText('秒档步长'), '30');
    await user.click(within(section).getAllByRole('button', { name: '保存' })[0]!);

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('PUT');
    expect(writeCalls[0]?.path).toBe('snapshot-slots/second');
    expect(writeCalls[0]?.body).toMatchObject({ enabled: true, step: 30 });

    // 分档已启用（步长 5）：改成 10 → PUT snapshot-slots/minute。
    await user.selectOptions(within(section).getByLabelText('分档步长'), '10');
    await user.click(within(section).getAllByRole('button', { name: '保存' })[1]!);
    await waitFor(() => expect(writeCalls).toHaveLength(2));
    expect(writeCalls[1]?.body).toMatchObject({ enabled: true, step: 10 });
  });

  it('_meta 未下发步长字典时回落到本地白名单', async () => {
    server.use(
      http.get('/admin/v1/_meta', () => {
        const { enums: _dropped, ...rest } = metaFixture;
        return HttpResponse.json({ code: 0, message: 'ok', data: { ...rest, enums: {} } });
      }),
    );
    renderApp('/snapshot-slots');
    await waitForPage();
    const section = screen.getByRole('region', { name: '六档配置' });
    // 兜底文案是「30 秒」（没有 _meta 后缀）。
    expect(within(section).getByRole('option', { name: '30 秒' })).toBeInTheDocument();
  });

  it('老路由 /snapshot-plans 重定向到 /snapshot-slots（replace）', async () => {
    const view = renderApp('/snapshot-plans');
    await waitFor(() => expect(view.location()).toBe('/snapshot-slots'));
    expect(await screen.findByRole('region', { name: '六档配置' })).toBeInTheDocument();
  });

  it('启用但没选步长时本地拦下，不发请求', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    const section = screen.getByRole('region', { name: '六档配置' });
    await user.click(within(section).getByLabelText('月档启用'));
    await user.click(within(section).getAllByRole('button', { name: '保存' })[4]!);

    expect(await within(section).findByText(/启用时必须选择步长/)).toBeInTheDocument();
    expect(writeCalls).toHaveLength(0);
  });

  it('绑定列表：一行一个目标，列含「字段数」；类型级与实例级各一行', async () => {
    renderApp('/snapshot-slots');
    await waitForPage();

    const bindings = await screen.findByRole('region', { name: '绑定' });
    await waitFor(() => expect(document.querySelectorAll('tbody tr')).toHaveLength(2));

    for (const head of ['作用域', '目标', '字段数', '创建时间', '操作']) {
      expect(within(bindings).getByRole('columnheader', { name: head })).toBeInTheDocument();
    }
    const rows = Array.from(document.querySelectorAll('tbody tr')).map((row) => row.textContent ?? '');
    expect(rows[0]).toContain('终端类型');
    expect(rows[0]).toContain('泵站');
    expect(rows[0]).toContain('2'); // 类型级：temp + power 两个字段
    expect(rows[1]).toContain('终端');
    expect(rows[1]).toContain('t-001');
    expect(rows[1]).toContain('1');
    // 每行都有编辑与删除入口。
    expect(within(bindings).getAllByRole('button', { name: '编辑' })).toHaveLength(2);
  });

  it('多选保存：body.item_codes 为数组；已选计数与标签外显', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();
    const { dialog } = await openBindingForm();

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '泵站' }));

    // 打开字段多选（复用 FilterMultiSelect），逐项勾选。
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /temp/ }));
    await user.click(within(fieldList).getByRole('option', { name: /power/ }));

    expect(within(dialog).getByText('已选 2 个字段')).toBeInTheDocument();
    // 已选项以标签外显、可单个移除。
    const chips = within(dialog).getByLabelText('已选字段');
    expect(within(chips).getByRole('button', { name: /移除字段 temp/ })).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('POST');
    expect(writeCalls[0]?.path).toBe('snapshot-bindings');
    expect(writeCalls[0]?.body).toMatchObject({ device_type_id: '1', item_codes: ['temp', 'power'] });
  });

  it('支持全选 / 反选（FilterMultiSelect 的可选能力）', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();
    const { dialog } = await openBindingForm();

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '泵站' }));
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    await screen.findByRole('listbox', { name: '字段选项' });

    await user.click(screen.getByRole('button', { name: '全选' }));
    expect(within(dialog).getByText('已选 4 个字段')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '反选' }));
    expect(within(dialog).getByText('已选 0 个字段')).toBeInTheDocument();
  });

  it('N=0 时禁用保存并说明原因', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();
    const { dialog } = await openBindingForm();

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '泵站' }));

    expect(within(dialog).getByText('已选 0 个字段')).toBeInTheDocument();
    expect(within(dialog).getByText('至少选择一个字段后才能保存。')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '保存' })).toBeDisabled();
  });

  it('切作用域清空目标+已选；换目标只清空已选、保留作用域', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();
    const { dialog } = await openBindingForm();

    // 选类型 + 两个字段。
    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '泵站' }));
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /temp/ }));
    expect(within(dialog).getByText('已选 1 个字段')).toBeInTheDocument();

    // 换目标（同作用域）：只清已选字段，作用域仍是「终端类型」。
    await user.keyboard('{Escape}');
    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList2 = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList2).getByRole('option', { name: '阀门' }));
    expect(within(dialog).getByText('已选 0 个字段')).toBeInTheDocument();
    expect(within(dialog).getByRole('radio', { name: /终端类型（整类）/ })).toBeChecked();

    // 切作用域：目标与已选一起清空。
    await user.click(within(dialog).getByRole('radio', { name: /终端（单个）/ }));
    expect(within(dialog).getByLabelText('终端')).toHaveValue('');
    expect(within(dialog).getByText('已选 0 个字段')).toBeInTheDocument();
  });

  it('编辑态：提示只影响后续写入，回填字段并可改（PUT）', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    await user.click(screen.getAllByRole('button', { name: '编辑' })[0]!);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('修改只影响后续写入，已产生的快照不变。')).toBeInTheDocument();
    // 回填：类型级目标「泵站」+ 两个已选字段。
    expect(within(dialog).getByText('已选 2 个字段')).toBeInTheDocument();

    // 再勾一个字段后保存 → PUT。
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /status/ }));
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('PUT');
    expect(writeCalls[0]?.path).toBe('snapshot-bindings/1');
    // 两列始终带齐：类型级时 terminal_id 显式 null（否则后端合并旧作用域会判"二选一"失败）。
    expect(writeCalls[0]?.body).toMatchObject({ device_type_id: '1', terminal_id: null });
    expect((writeCalls[0]?.body as { item_codes: string[] }).item_codes).toHaveLength(3);
  });

  it('后端 400（目标重复 / 字段不存在）渲染在表单内，抽屉不关', async () => {
    server.use(
      http.post('/admin/v1/snapshot-bindings', () =>
        HttpResponse.json({ code: 400, message: '该目标已有一条绑定，请编辑那一条' }, { status: 400 }),
      ),
    );
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();
    const { dialog } = await openBindingForm();

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '阀门' }));
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /temp/ }));
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    expect(await within(dialog).findByText(/该目标已有一条绑定，请编辑那一条/)).toBeInTheDocument();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('存储与保留区块仍在，且说明最小清理粒度', async () => {
    renderApp('/snapshot-slots');
    await waitForPage();
    const section = screen.getByRole('region', { name: '存储与保留' });
    expect(within(section).getByLabelText('秒表（秒） 保留天数')).toBeInTheDocument();
    expect(
      within(section).getByText(/最小清理粒度 = 整月（粗表 \/ 分表）、整日（秒表），实际保留可能比设定值更长。/),
    ).toBeInTheDocument();
  });

  it('编辑切作用域：类型级 → 实例级，提交体里 device_type_id 显式为 null', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    // 第 0 行是类型级绑定（夹具顺序固定）。
    await user.click(screen.getAllByRole('button', { name: '编辑' })[0]!);
    const dialog = await screen.findByRole('dialog');

    await user.click(within(dialog).getByRole('radio', { name: /终端（单个）/ }));
    // 切作用域会清空目标与已选字段：先选终端再重选字段。
    await user.type(within(dialog).getByLabelText('终端'), 't-001');
    const candidates = await within(dialog).findByRole('list', { name: '终端候选' });
    await user.click(within(candidates).getByRole('button', { name: /t-001/ }));
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const list = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(list).getByRole('option', { name: /temp/ }));

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    // 关键：type 侧必须显式 null，否则后端把库里旧 device_type_id 合并进来 → 两列同时非空。
    expect(writeCalls[0]?.body).toMatchObject({ device_type_id: null, terminal_id: 't-001' });
  });

  it('编辑切作用域：实例级 → 类型级，提交体里 terminal_id 显式为 null', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    // 第 1 行是实例级绑定。
    await user.click(screen.getAllByRole('button', { name: '编辑' })[1]!);
    const dialog = await screen.findByRole('dialog');

    await user.click(within(dialog).getByRole('radio', { name: /终端类型/ }));
    await user.click(within(dialog).getByRole('combobox', { name: /终端类型/ }));
    const typeList = await screen.findByRole('listbox');
    await user.click(within(typeList).getAllByRole('option')[0]!);
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const list = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(list).getAllByRole('option')[0]!);

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.body).toMatchObject({ terminal_id: null });
    expect((writeCalls[0]?.body as { device_type_id?: unknown }).device_type_id).not.toBeNull();
  });
});
