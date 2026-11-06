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

  it('绑定列表渲染「作用域/目标/数据项/创建时间」，类型级与实例级各一行', async () => {
    renderApp('/snapshot-slots');
    await waitForPage();

    const bindings = await screen.findByRole('region', { name: '绑定' });
    await waitFor(() => expect(document.querySelectorAll('tbody tr')).toHaveLength(2));

    for (const head of ['作用域', '目标', '数据项', '创建时间', '操作']) {
      expect(within(bindings).getByRole('columnheader', { name: head })).toBeInTheDocument();
    }
    const rows = Array.from(document.querySelectorAll('tbody tr')).map((row) => row.textContent ?? '');
    // 类型级：作用域=终端类型，目标=类型名（泵站）；实例级：目标是终端 ID。
    expect(rows[0]).toContain('终端类型');
    expect(rows[0]).toContain('泵站');
    expect(rows[0]).toContain('temp');
    expect(rows[1]).toContain('终端');
    expect(rows[1]).toContain('t-001');
    expect(rows[1]).toContain('power');
  });

  it('绑定表单：切作用域换目标控件；选类型后数据项来自该类型的属性', async () => {
    renderApp('/snapshot-slots');
    await waitForPage();
    const { user, dialog } = await openBindingForm();

    // 默认类型级：先选类型（远程下拉），数据项候选随后来自该类型。
    const typeTrigger = within(dialog).getByRole('combobox', { name: '终端类型' });
    await user.click(typeTrigger);
    const listbox = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(listbox).getByRole('option', { name: '泵站' }));

    const itemSelect = within(dialog).getByLabelText('数据项');
    await waitFor(() => expect(itemSelect).toBeEnabled());
    const values = Array.from((itemSelect as HTMLSelectElement).options).map((option) => option.value);
    expect(values).toEqual(expect.arrayContaining(['temp', 'power', 'status', 'online']));

    // 切到实例级：类型下拉消失，出现终端搜索。
    await user.click(within(dialog).getByRole('radio', { name: /终端（单个）/ }));
    expect(within(dialog).queryByRole('combobox', { name: '终端类型' })).not.toBeInTheDocument();
    const search = within(dialog).getByLabelText('终端');
    await user.type(search, 't-001');
    await waitFor(async () => {
      const candidate = await within(dialog).findByRole('button', { name: /t-001/ }, { timeout: 3000 });
      expect(candidate).toBeInTheDocument();
    }, { timeout: 5000 });
    await user.click(within(dialog).getByRole('button', { name: /t-001/ }));
    // 选终端后按该终端的 type_id（t-001 → 1）取候选。
    await waitFor(() => {
      const options = Array.from((within(dialog).getByLabelText('数据项') as HTMLSelectElement).options).map(
        (option) => option.value,
      );
      expect(options).toEqual(expect.arrayContaining(['temp', 'power']));
    });
  });

  it('后端 400（数据项不属于该类型）显示在表单内，抽屉不关', async () => {
    server.use(
      http.post('/admin/v1/snapshot-bindings', () =>
        HttpResponse.json({ code: 400, message: '数据项 temp 不属于该终端类型' }, { status: 400 }),
      ),
    );
    renderApp('/snapshot-slots');
    await waitForPage();
    const { user, dialog } = await openBindingForm();

    await user.click(within(dialog).getByRole('combobox', { name: '终端类型' }));
    const listbox = await screen.findByRole('listbox', { name: '终端类型选项' });
    await user.click(within(listbox).getByRole('option', { name: '泵站' }));
    await waitFor(() => expect(within(dialog).getByLabelText('数据项')).toBeEnabled());
    await user.selectOptions(within(dialog).getByLabelText('数据项'), 'temp');
    await user.click(within(dialog).getByRole('button', { name: '保存' }));

    expect(await within(dialog).findByText(/数据项 temp 不属于该终端类型/)).toBeInTheDocument();
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
});
