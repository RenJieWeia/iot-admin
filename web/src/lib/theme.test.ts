import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ensureLocalStorage } from '../test/localStorage';
import { DEFAULT_THEME, THEME_STORAGE_KEY, initialTheme, readStoredTheme, resolveTheme, setTheme } from './theme';

beforeEach(() => {
  ensureLocalStorage();
});

afterEach(() => {
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
});

describe('主题解析（纯函数）', () => {
  it('三个合法值原样返回', () => {
    expect(resolveTheme('teal')).toBe('teal');
    expect(resolveTheme('blue')).toBe('blue');
    expect(resolveTheme('graphite')).toBe('graphite');
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
    setTheme('graphite');
    window.localStorage.clear();
    expect(readStoredTheme()).toBe('blue');
  });

  it('存储写入失败也不影响视觉切换', () => {
    const original = window.localStorage.setItem;
    window.localStorage.setItem = () => {
      throw new Error('quota');
    };
    try {
      setTheme('graphite');
      expect(document.documentElement.dataset.theme).toBe('graphite');
    } finally {
      window.localStorage.setItem = original;
    }
  });

  it('initialTheme 优先看 DOM 上已有的 dataset', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'blue');
    document.documentElement.dataset.theme = 'graphite';
    expect(initialTheme()).toBe('graphite');
  });
});
