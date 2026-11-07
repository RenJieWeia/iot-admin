import type { ReactElement } from 'react';
import { render } from '@testing-library/react';
import { Theme } from '@radix-ui/themes';
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
  /**
   * 必须和生产一致地套一层 `<Theme>`（main.tsx 就是这么渲染的）：
   * Radix 的 `Select.Content` / `Tooltip` 等组件在 body 里调用 `useThemeContext()`，
   * 没有 Theme 会直接抛错。参数与 main.tsx 的 RADIX_THEME_PROPS 保持一致。
   */
  const ui: ReactElement = (
    <QueryClientProvider client={queryClient}>
      <Theme
        appearance="light"
        accentColor="blue"
        grayColor="slate"
        radius="medium"
        scaling="100%"
        hasBackground={false}
      >
        <MemoryRouter initialEntries={[initialPath]}>
          <AppRoutes />
          <LocationProbe onChange={(value) => (location = value)} />
        </MemoryRouter>
      </Theme>
    </QueryClientProvider>
  );
  return { ...render(ui), queryClient, location: () => location };
}
