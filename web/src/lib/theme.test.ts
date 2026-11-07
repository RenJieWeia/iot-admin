import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ensureLocalStorage } from '../test/localStorage';
import { DEFAULT_THEME, readStoredTheme, resolveTheme, setTheme, THEME_STORAGE_KEY } from './theme';

beforeEach(() => {
  ensureLocalStorage();
});

afterEach(() => {
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
});

describe('主题解析（纯函数）', () => {
  it('合法值原样返回（当前只有电蓝）', () => {
    expect(resolveTheme('blue')).toBe('blue');
  });

  it('非法值 / 缺失 / 空串 → 默认 blue', () => {
    for (const raw of ['', 'red', 'TEAL', 'graphite ', 'null', null, undefined]) {
      expect(resolveTheme(raw)).toBe(DEFAULT_THEME);
    }
  });
});

describe('主题持久化与应用', () => {
  it('setTheme 同时写 dataset 与 localStorage（不刷新）', () => {
    setTheme('blue');
    expect(document.documentElement.dataset.theme).toBe('blue');
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('blue');
  });

  it('localStorage 被清空后回到 blue', () => {
    window.localStorage.clear();
    expect(readStoredTheme()).toBe('blue');
  });

  it('存储写入失败也不影响视觉切换', () => {
    const original = window.localStorage.setItem;
    window.localStorage.setItem = () => {
      throw new Error('quota');
    };
    try {
      expect(() => setTheme('blue')).not.toThrow();
      expect(document.documentElement.dataset.theme).toBe('blue');
    } finally {
      window.localStorage.setItem = original;
    }
  });

  it('旧主题值（teal / graphite）被判为非法并回落 blue', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'teal');
    expect(readStoredTheme()).toBe('blue');
    window.localStorage.setItem(THEME_STORAGE_KEY, 'graphite');
    expect(readStoredTheme()).toBe('blue');
  });

  it('initialTheme 优先看 DOM 上已有的 dataset', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'blue');
  });
});
