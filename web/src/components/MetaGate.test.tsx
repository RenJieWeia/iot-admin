import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { MetaGate } from './MetaGate';

function renderGate() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MetaGate>
        <p>页面主体</p>
      </MetaGate>
    </QueryClientProvider>,
  );
}

describe('MetaGate 的加载态（§16.4）', () => {
  it('未就绪时给壳名 + 骨架，不写「加载中…」', async () => {
    renderGate();

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('IoT 管理后台');
    expect(screen.queryByText('加载中…')).not.toBeInTheDocument();
    // 状态文案只给屏幕阅读器，视觉上是骨架条。
    expect(screen.getByRole('status')).toHaveTextContent('正在加载字段元数据');
    expect(document.querySelectorAll('.bg-skeleton')).toHaveLength(3);

    // 元数据就绪后交给页面主体，占位消失。
    expect(await screen.findByText('页面主体')).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });
});
