import type { ReactElement } from 'react';
import { render } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router';
import { AppRoutes } from '../router';

/** 把当前 location 暴露给用例（MemoryRouter 不改 window.location）。 */
function LocationProbe({ onChange }: { onChange: (value: string) => void }) {
  const location = useLocation();
  onChange(`${location.pathname}${location.search}`);
  return null;
}

/**
 * 测试渲染：MemoryRouter 定位到目标 URL，路由表与生产一致。
 * 每个用例独立 QueryClient，避免缓存串场。
 */
export function renderApp(initialPath: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
  let location = initialPath;
  const ui: ReactElement = (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <AppRoutes />
        <LocationProbe onChange={(value) => (location = value)} />
      </MemoryRouter>
    </QueryClientProvider>
  );
  return { ...render(ui), queryClient, location: () => location };
}
