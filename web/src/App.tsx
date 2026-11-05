import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router';
import { ApiError } from './api/client';
import { AppRoutes } from './router';

/** 路由基址与 Go 侧 /ui/ 挂载点一致。 */
export const ROUTER_BASENAME = '/ui';

/**
 * Query 默认值：每次查询带 AbortSignal（只应用最新响应），网络/5xx 失败重试一次，
 * 4xx（invalid_query 等确定性错误）不重试；窗口聚焦不自动重取，避免打断正在编辑的抽屉。
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000,
      retry: (failureCount, error) =>
        failureCount < 1 && !(error instanceof ApiError && error.status < 500),
      refetchOnWindowFocus: false,
    },
  },
});

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename={ROUTER_BASENAME}>
        <AppRoutes />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
