import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';
import { metaFixture } from '../test/fixtures';
import { server, writeCalls } from '../test/server';

async function waitForPage() {
  await screen.findByRole('group', { name: '六档配置' }, { timeout: 5000 });
  await waitFor(() => expect(screen.queryByText('正在加载六档配置…')).not.toBeInTheDocument());
}

async function openBindingForm() {
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: '新建绑定' }));
  return { user, dialog: await screen.findByRole('dialog') };
}

const slotSave = () =>
  within(screen.getByRole('group', { name: '六档配置' })).getByRole('button', { name: '保存' });
const retentionSave = () =>
  within(screen.getByRole('group', { name: '保留期' })).getByRole('button', { name: '保存' });

describe('快照配置页（存储策略 / 数据项绑定 两段式）', () => {
  it('三张平级卡（六档配置 / 保留期 / 数据项绑定），保存按钮恰好两个且各自在卡头右侧', async () => {
    renderApp('/snapshot-slots');
    await waitForPage();

    // 1107010：不再有「存储策略」外壳，三张卡平级、互不嵌套。
    expect(screen.queryByRole('region', { name: '存储策略' })).toBeNull();
    for (const name of ['六档配置', '保留期', '数据项绑定']) {
      expect(screen.getByRole('group', { name })).toBeInTheDocument();
    }
    expect(document.querySelectorAll('[data-card] [data-card]').length).toBe(0);
    // 全页只有两个「保存」（旧的 9 个逐行按钮已收成两个批量保存）。
    expect(screen.getAllByRole('button', { name: '保存' })).toHaveLength(2);
    expect(slotSave()).toBeInTheDocument();
    expect(retentionSave()).toBeInTheDocument();
    // 档位固定：没有新增入口。
    expect(screen.queryByRole('button', { name: /新增档位/ })).not.toBeInTheDocument();
  });

  /** Radix Select 不是原生 select：必须点开触发符再点选项（selectOptions 不适用）。 */
  const pickStep = async (
    user: ReturnType<typeof userEvent.setup>,
    scope: HTMLElement,
    triggerName: string,
    optionName: string | RegExp,
  ) => {
    await user.click(within(scope).getByRole('combobox', { name: triggerName }));
    await user.click(await screen.findByRole('option', { name: optionName }));
  };

  it('六档批量保存：无改动时禁用；改两行后点一次发出两条 PUT，成功后恢复禁用', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    expect(slotSave()).toBeDisabled();

    const slots = screen.getByRole('group', { name: '六档配置' });
    await user.click(within(slots).getByLabelText('秒档启用'));
    await pickStep(user, slots, '秒档步长', /30 秒/);
    await pickStep(user, slots, '分档步长', /10 分/);

    expect(within(slots).getByText('有未保存修改')).toBeInTheDocument();
    expect(slotSave()).toBeEnabled();

    await user.click(slotSave());
    await waitFor(() => expect(writeCalls).toHaveLength(2));
    expect(writeCalls.map((call) => call.path)).toEqual(['snapshot-slots/second', 'snapshot-slots/minute']);
    expect(writeCalls[0]?.body).toMatchObject({ enabled: true, step: 30 });
    expect(writeCalls[1]?.body).toMatchObject({ step: 10 });

    // 保存成功后草稿清空：未保存标记消失、按钮回到禁用。
    await waitFor(() => expect(within(slots).queryByText('有未保存修改')).not.toBeInTheDocument());
    await waitFor(() => expect(slotSave()).toBeDisabled());
  });

  it('六档部分失败：成功行清出草稿，失败行保留标红；再保存只提交失败行', async () => {
    const user = userEvent.setup();
    server.use(
      // 覆盖 handler 也要记账，否则断言看到的请求数会少一条。
      http.put('/admin/v1/snapshot-slots/minute', async ({ request }) => {
        const body = await request.json();
        writeCalls.push({ method: 'PUT', path: 'snapshot-slots/minute', body });
        return HttpResponse.json({ code: 400, message: '步长越界' }, { status: 400 });
      }),
    );
    renderApp('/snapshot-slots');
    await waitForPage();

    const slots = screen.getByRole('group', { name: '六档配置' });
    await user.click(within(slots).getByLabelText('秒档启用'));
    await pickStep(user, slots, '秒档步长', /30 秒/);
    await pickStep(user, slots, '分档步长', /10 分/);
    await user.click(slotSave());

    await waitFor(() => expect(writeCalls).toHaveLength(2));
    // 提示点名失败行；失败行仍有未保存草稿，成功行已清出。
    expect(await within(slots).findByText(/有 1 行没存上：分档/)).toBeInTheDocument();
    expect(within(slots).getByText(/未存上：步长越界/)).toBeInTheDocument();
    await waitFor(() =>
      // 触发器没有 .value：断言它显示的当前项文案（与旧原生 select 显示的「10 分」一致）。
      expect(within(slots).getByRole('combobox', { name: '分档步长' })).toHaveTextContent(/10 分/),
    );
    // 秒档已保存成功：它的行不再是 dirty，草稿里也没有它了（再保存只发 1 条）。
    expect(within(slots).getByText('有未保存修改')).toBeInTheDocument();
    await user.click(slotSave());
    await waitFor(() => expect(writeCalls).toHaveLength(3));
    expect(writeCalls[2]?.path).toBe('snapshot-slots/minute');
  });

  it('保留期批量保存：无改动禁用；改两行点一次发两条 PUT', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    expect(retentionSave()).toBeDisabled();
    const retention = screen.getByRole('group', { name: '保留期' });
    const seconds = within(retention).getByLabelText('秒表（秒） 保留天数');
    const coarse = within(retention).getByLabelText('粗表（时 / 日 / 月 / 年） 保留天数');

    await user.clear(seconds);
    await user.type(seconds, '30');
    await user.clear(coarse);
    await user.type(coarse, '90');

    expect(within(retention).getByText('有未保存修改')).toBeInTheDocument();
    await user.click(retentionSave());

    await waitFor(() => expect(writeCalls).toHaveLength(2));
    // 顺序按行顺序（粗表在前），只断言集合与内容。
    expect(writeCalls.map((call) => call.path).sort()).toEqual([
      'snapshot-settings/snapshot_coarse',
      'snapshot-settings/snapshot_second',
    ]);
    const byPath = new Map(writeCalls.map((call) => [call.path, call.body]));
    expect(byPath.get('snapshot-settings/snapshot_second')).toMatchObject({ retention_days: 30 });
    expect(byPath.get('snapshot-settings/snapshot_coarse')).toMatchObject({ retention_days: 90 });
    // 说明文案仍在。
    expect(
      within(retention).getByText(/最小清理粒度 = 整月（粗表 \/ 分表）、整日（秒表），实际保留可能比设定值更长。/),
    ).toBeInTheDocument();
  });

  it('绑定列表：一行一个目标，列含「字段数」；操作是文字链（无描边按钮）', async () => {
    renderApp('/snapshot-slots');
    await waitForPage();

    const bindings = screen.getByRole('group', { name: '数据项绑定' });
    await waitFor(() => expect(document.querySelectorAll('tbody tr')).toHaveLength(2));
    for (const head of ['作用域', '目标', '字段数', '创建时间', '操作']) {
      expect(within(bindings).getByRole('columnheader', { name: head })).toBeInTheDocument();
    }
    const rows = Array.from(document.querySelectorAll('tbody tr')).map((row) => row.textContent ?? '');
    expect(rows[0]).toContain('终端类型');
    expect(rows[0]).toContain('泵站');
    expect(rows[0]).toContain('2');
    expect(rows[1]).toContain('t-001');
    expect(rows[1]).toContain('1');

    const edit = within(bindings).getAllByRole('button', { name: '编辑' })[0]!;
    expect(edit.className).toContain('text-base');
    expect(edit.className).not.toContain('border-border');
  });

  it('多选保存：body.item_codes 为数组；已选计数与标签外显', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();
    const { dialog } = await openBindingForm();

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '泵站' }));

    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /temp/ }));
    await user.click(within(fieldList).getByRole('option', { name: /power/ }));

    expect(within(dialog).getByText('已选 2 个字段')).toBeInTheDocument();
    const chips = within(dialog).getByLabelText('已选字段');
    expect(within(chips).getByRole('button', { name: /移除字段 temp/ })).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
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

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getByRole('option', { name: '泵站' }));
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /temp/ }));
    expect(within(dialog).getByText('已选 1 个字段')).toBeInTheDocument();

    await user.keyboard('{Escape}');
    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList2 = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList2).getByRole('option', { name: '阀门' }));
    expect(within(dialog).getByText('已选 0 个字段')).toBeInTheDocument();
    expect(within(dialog).getByRole('radio', { name: /终端类型（整类）/ })).toBeChecked();

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
    expect(within(dialog).getByText('已选 2 个字段')).toBeInTheDocument();

    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const fieldList = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(fieldList).getByRole('option', { name: /status/ }));
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.method).toBe('PUT');
    expect(writeCalls[0]?.path).toBe('snapshot-bindings/1');
    expect((writeCalls[0]?.body as { item_codes: string[] }).item_codes).toHaveLength(3);
  });

  it('后端 400（目标重复）渲染在表单内，抽屉不关', async () => {
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

  it('_meta 未下发步长字典时回落到本地白名单', async () => {
    server.use(
      http.get('/admin/v1/_meta', () => {
        const { enums: _dropped, ...rest } = metaFixture;
        return HttpResponse.json({ code: 0, message: 'ok', data: { ...rest, enums: {} } });
      }),
    );
    renderApp('/snapshot-slots');
    await waitForPage();
    const section = screen.getByRole('group', { name: '六档配置' });
    // Radix Select 的选项只在展开后挂载，且该行默认禁用（enabled=false）：
    // 先启用这一档，再展开断言「字典缺失时回落到本地白名单」仍然生效（语义不变）。
    const user = userEvent.setup();
    await user.click(within(section).getByLabelText('秒档启用'));
    await user.click(within(section).getByRole('combobox', { name: '秒档步长' }));
    expect(await screen.findByRole('option', { name: '30 秒' })).toBeInTheDocument();
    await user.keyboard('{Escape}');
  });

  it('启用档位但没选步长：提交前本地拦下，不发请求', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    const slots = screen.getByRole('group', { name: '六档配置' });
    // 只开启用、不选步长。
    await user.click(within(slots).getByLabelText('秒档启用'));
    expect(within(slots).getByText('有未保存修改')).toBeInTheDocument();
    await user.click(slotSave());

    expect(await within(slots).findByText(/启用档位必须选择步长/)).toBeInTheDocument();
    expect(writeCalls).toHaveLength(0);
    // 拦截后草稿保留，用户补选步长即可继续保存。
    expect(slotSave()).toBeEnabled();
  });

  it('编辑切作用域：类型级 → 实例级，提交体里 device_type_id 显式为 null', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    await user.click(screen.getAllByRole('button', { name: '编辑' })[0]!);
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('radio', { name: /终端（单个）/ }));
    await user.type(within(dialog).getByLabelText('终端'), 't-001');
    const candidates = await within(dialog).findByRole('list', { name: '终端候选' });
    await user.click(within(candidates).getByRole('button', { name: /t-001/ }));
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const list = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(list).getByRole('option', { name: /temp/ }));

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    // 后端 update 会把库里旧 device_type_id 合并进校验视图，只传新作用域列会 400。
    expect(writeCalls[0]?.body).toMatchObject({ device_type_id: null, terminal_id: 't-001' });
  });

  it('编辑切作用域：实例级 → 类型级，提交体里 terminal_id 显式为 null', async () => {
    const user = userEvent.setup();
    renderApp('/snapshot-slots');
    await waitForPage();

    await user.click(screen.getAllByRole('button', { name: '编辑' })[1]!);
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('radio', { name: /终端类型（整类）/ }));
    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const typeList = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(typeList).getAllByRole('option')[0]!);
    await user.click(within(dialog).getByRole('combobox', { name: '字段' }));
    const list = await screen.findByRole('listbox', { name: '字段选项' });
    await user.click(within(list).getAllByRole('option')[0]!);

    await user.click(within(dialog).getByRole('button', { name: '保存' }));
    await waitFor(() => expect(writeCalls).toHaveLength(1));
    expect(writeCalls[0]?.body).toMatchObject({ terminal_id: null });
    expect((writeCalls[0]?.body as { device_type_id?: unknown }).device_type_id).not.toBeNull();
  });

  it('老路由 /snapshot-plans 重定向到 /snapshot-slots（replace）', async () => {
    const view = renderApp('/snapshot-plans');
    await waitFor(() => expect(view.location()).toBe('/snapshot-slots'));
    expect(await screen.findByRole('group', { name: '六档配置' })).toBeInTheDocument();
  });
});
