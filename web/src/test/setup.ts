import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { resetWriteCalls, server } from './server';

// jsdom 缺失、Radix/表格会碰到的浏览器 API 兜底。
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (!('ResizeObserver' in globalThis)) {
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub;
}
if (!('scrollTo' in window) || typeof window.scrollTo !== 'function') {
  window.scrollTo = () => {};
}
window.scrollTo = () => {};

beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' });
  // jsdom 的 AbortSignal 与 Node undici fetch 不同 realm，直接传会抛
  // "Expected signal to be an instance of AbortSignal"。测试里丢弃 signal，
  // 生产代码仍然给每次查询传 AbortSignal（只应用最新响应）。
  const intercepted = globalThis.fetch;
  globalThis.fetch = ((input: RequestInfo | URL, init: RequestInit = {}) => {
    if (init.signal) {
      const { signal: _dropped, ...rest } = init;
      return intercepted(input, rest);
    }
    return intercepted(input, init);
  }) as typeof fetch;
});
afterEach(() => {
  cleanup();
  server.resetHandlers();
  resetWriteCalls();
  window.sessionStorage.clear();
});
afterAll(() => server.close());
