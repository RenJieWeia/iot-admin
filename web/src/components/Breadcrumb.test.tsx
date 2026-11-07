import { screen, waitFor, within } from '@testing-library/react';
import { cleanup } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { groupCrumb } from '../lib/navGroups';
import { renderApp } from '../test/utils';

/**
 * 面包屑（1107009 追加）：
 * - 分组名与侧栏同源（lib/navGroups.ts），不在组件里复制第二份中文；
 * - 分组没有落地页 → 纯文本，不是禁用按钮；
 * - 末项 aria-current="page" 且不可点。
 */
describe('面包屑', () => {
  it('一级页只显示分组名，页面名交给 h1（不重复）', async () => {
    renderApp('/terminals');
    // 等页面真正加载完（骨架阶段的分组也是「实体」，这里直接等最终文案）。
    await screen.findByRole('navigation', { name: '面包屑' });
    const crumb = await screen.findByRole('navigation', { name: '面包屑' });
    await waitFor(() => expect(within(crumb).getByText('实体')).toBeInTheDocument());
    // 只有一项：分组名本身，且不重复页面标题「终端」。
    expect(within(crumb).queryByText('终端')).toBeNull();
    expect(within(crumb).getByText('实体').tagName).toBe('SPAN');
    expect(within(crumb).queryByRole('link')).toBeNull();
  });

  it('只读页属「数据」组，异常页属「异常」组（分组名与侧栏同源）', async () => {
    renderApp('/readonly/latest-values');
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: '面包屑' })).toHaveTextContent('数据'),
    );
    cleanup();
    renderApp('/exceptions/events');
    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: '面包屑' })).toHaveTextContent('异常'),
    );
  });

  it('分组名来自 lib/navGroups.ts（页面与侧栏共用同一份）', () => {
    expect(groupCrumb('entities')).toEqual([{ label: '实体' }]);
    expect(groupCrumb('data')).toEqual([{ label: '数据' }]);
    expect(groupCrumb('exceptions')).toEqual([{ label: '异常' }]);
  });
});
