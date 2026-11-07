/**
 * jsdom 在部分环境下不提供 localStorage（opaque origin）。
 * 测试里用一个内存实现兜底，语义与浏览器一致（get/set/remove/clear/length/key）。
 */
export function ensureLocalStorage(): void {
  if (typeof window === 'undefined' || window.localStorage) return;
  const memory = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return memory.size;
    },
    clear: () => memory.clear(),
    getItem: (key: string) => (memory.has(key) ? (memory.get(key) as string) : null),
    key: (index: number) => Array.from(memory.keys())[index] ?? null,
    removeItem: (key: string) => void memory.delete(key),
    setItem: (key: string, value: string) => void memory.set(key, String(value)),
  };
  Object.defineProperty(window, 'localStorage', { configurable: true, value: storage });
}
