import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/utils';

/**
 * P2 试点范围守卫（1107018）：Radix 控件**只允许出现在终端列表页**。
 *
 * FilterBar / Pagination 是共享组件（实体列表 / 只读页 / 异常页 / 终端类型详情页），
 * 试点靠 `pilot` 开关收口。这个用例把「只此一页」钉死：哪天有人把 pilot 变默认 true，
 * 或直接把 Radix 控件写进共享组件的旧分支，只读页/异常页会立刻变红。
 */
describe('P2 试点范围：只有终端列表页换 Radix 控件', () => {
  it('终端列表页：Button / TextField / Card 都是 Radix 的，卡片仍两张平级', async () => {
    renderApp('/terminals');
    // 骨架屏也有一张 table，必须等到真实内容（摘要行）挂载后再断言。
    await screen.findByTestId('list-summary');
    await waitFor(() => expect(document.querySelectorAll('button.rt-Button').length).toBeGreaterThan(0));
    expect(document.querySelectorAll('.rt-TextFieldRoot').length).toBeGreaterThan(0);
    expect(document.querySelectorAll('[data-card].rt-Card')).toHaveLength(2);
    expect(document.querySelectorAll('[data-card] [data-card]')).toHaveLength(0);
  });

  it('只读页 / 异常页：仍是旧控件（无 Radix Button / TextField / Card）', async () => {
    for (const path of ['/readonly/latest-values', '/exceptions/events']) {
      const view = renderApp(path);
      await screen.findAllByRole('table');
      await waitFor(() => expect(document.querySelectorAll('[data-card]').length).toBe(2));
      expect(document.querySelectorAll('button.rt-Button'), path).toHaveLength(0);
      expect(document.querySelectorAll('.rt-TextFieldRoot'), path).toHaveLength(0);
      expect(document.querySelectorAll('.rt-Card'), path).toHaveLength(0);
      // 旧实现仍在：分页按钮还是 .btn
      expect(document.querySelectorAll('button.btn').length, path).toBeGreaterThan(0);
      view.unmount();
    }
  });
});
