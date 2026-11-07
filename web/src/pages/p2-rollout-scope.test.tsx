import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';

/**
 * P2 铺开守卫（1107019）。
 *
 * 取代已删除的 `p2-pilot-scope.test.tsx`：那 3 条守的是「Radix 控件只在 /terminals」，
 * 铺开后该前提不再成立。这里换成方向相反、仍然可失败的断言。
 */
describe('P2 铺开：全站单一实现', () => {
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

  it('三个路由都没有旧类残留，且都有 Radix 控件', async () => {
    for (const path of ['/terminals', '/data-domains', '/readonly/latest-values']) {
      const view = renderApp(path);
      await screen.findAllByRole('table');
      await waitFor(() => expect(document.querySelectorAll('[data-card]').length).toBeGreaterThan(0));
      expect(document.querySelectorAll('button.btn'), path).toHaveLength(0);
      expect(document.querySelectorAll('input.input'), path).toHaveLength(0);
      expect(document.querySelectorAll('textarea.input'), path).toHaveLength(0);
      expect(document.querySelectorAll('button.rt-Button').length, path).toBeGreaterThan(0);
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
      expect(document.querySelectorAll('[data-card].rt-Card'), path).toHaveLength(expected);
      view.unmount();
    }
  });
});
