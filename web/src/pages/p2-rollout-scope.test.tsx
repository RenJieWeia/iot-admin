import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';

/**
 * 全站单一实现守卫（1107019 P2 铺开 → 1107021 P5 收口）。
 *
 * 方向：从「旧类名不出现」的**反向**断言，换成「Radix 控件必须出现」的**正向**断言。
 * 反向断言在控件被写成裸 `<div>` 时仍然会通过（旧类名本来就没有），正向断言不会：
 * 一个页面少了 Radix 组件、或有人把控件降级成原生元素，这里必红。
 *
 * P5 起 `.btn` / `.input` / `.select` 的样式已从 global.css 整段删除，
 * 因此同时保留「这些类名一个都不许再出现」的收尾断言。
 */

/** 5 页 / 6 条路由（列表 · 只读 · 异常 · 快照 · 详情）的 Radix 控件基线。 */
const PAGES: ReadonlyArray<readonly [path: string, hasTextField: boolean]> = [
  ['/terminals', true],
  ['/data-domains', true],
  ['/readonly/latest-values', true],
  ['/exceptions/events', true],
  // 快照配置页没有筛选栏，但保留期区块有 Radix TextField（SnapshotRetentionSection）。
  ['/snapshot-slots', true],
  // 终端类型详情页是纯展示 + 分页，没有文本输入。
  ['/device-types/100', false],
];

/** 自研控件类名（P5 已删样式）：DOM 上出现任何一个都算回潮。 */
const LEGACY_SELECTORS = ['.btn', '.btn-primary', '.btn-danger', '.btn-link', '.input', '.select'];

async function mountPage(path: string) {
  const view = renderApp(path);
  await screen.findAllByRole('table');
  await waitFor(() => expect(document.querySelectorAll('[data-card]').length).toBeGreaterThan(0));
  return view;
}

describe('P5 收口：全站都是 Radix', () => {
  it('label[for] 命中 Radix TextField 的内部 input（id 透传不断链）', async () => {
    const view = renderApp('/terminals');
    await screen.findByTestId('list-summary');
    for (const name of ['关键词', '终端 ID', '网关 ID']) {
      const label = screen.getByText(name, { selector: 'label' });
      const id = label.getAttribute('for');
      expect(id, `${name} 的 label 缺少 for`).toBeTruthy();
      const target = document.getElementById(id ?? '');
      expect(target, `${name} 的 for=${id} 没有命中元素`).not.toBeNull();
      expect(target?.tagName).toBe('INPUT');
      expect(target).toHaveClass('rt-TextFieldInput');
      expect(label.contains(target)).toBe(false);
    }
    view.unmount();
  });

  it('每条路由的按钮 / 卡片都是 Radix 组件，有文本输入的页面必须走 Radix TextField', async () => {
    for (const [path, hasTextField] of PAGES) {
      const view = await mountPage(path);
      const counts = {
        card: document.querySelectorAll('.rt-Card').length,
        button: document.querySelectorAll('button.rt-Button').length,
        textField: document.querySelectorAll('.rt-TextFieldRoot').length,
      };
      const where = `${path} 的 Radix 控件覆盖：${JSON.stringify(counts)}`;
      expect(counts.card, where).toBeGreaterThan(0);
      expect(counts.button, where).toBeGreaterThan(0);
      if (hasTextField) {
        // 只要页面有文本输入，就必须是 Radix TextField（底层 input 带 rt-TextFieldInput），
        // 不允许退回自研的旧文本输入类（P5 已删除该类名与对应样式）。
        expect(counts.textField, where).toBeGreaterThan(0);
        expect(
          document.querySelectorAll('input.rt-TextFieldInput').length,
          where,
        ).toBeGreaterThan(0);
      }
      view.unmount();
    }
  });

  it('全站无自研控件类残留（.btn* / .input / .select 及其复合用法）', async () => {
    for (const [path] of PAGES) {
      const view = await mountPage(path);
      for (const selector of LEGACY_SELECTORS) {
        expect(document.querySelectorAll(selector), `${path} 出现了 ${selector}`).toHaveLength(0);
      }
      // 复合形态：老代码里「Radix 之外还套一层 .field-error .input」的写法也不许回归。
      expect(document.querySelectorAll('.field-error .input'), path).toHaveLength(0);
      expect(document.querySelectorAll('input.input, textarea.input, button.btn'), path).toHaveLength(0);
      view.unmount();
    }
  });

  it('5 页的卡片数量与基线一致且无嵌套（列表 / 只读 / 异常 / 快照 / 详情）', async () => {
    const cases: Array<[string, number]> = [
      ['/terminals', 2],
      ['/data-domains', 2],
      ['/readonly/latest-values', 2],
      ['/exceptions/events', 2],
      ['/snapshot-slots', 3],
      ['/device-types/100', 1],
    ];
    for (const [path, expected] of cases) {
      const view = renderApp(path);
      await waitFor(() => expect(document.querySelectorAll('[data-card]').length, path).toBe(expected));
      expect(document.querySelectorAll('[data-card] [data-card]'), path).toHaveLength(0);
      // 每一张业务卡片都必须真的是 Radix Card（反向断言：数量与基线一致，且无自研卡片类）。
      expect(document.querySelectorAll('[data-card].rt-Card'), path).toHaveLength(expected);
      view.unmount();
    }
  });
});
