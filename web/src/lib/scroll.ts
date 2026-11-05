/**
 * 滚动位置记忆：以「带查询参数的列表地址」为键，返回列表页时恢复阅读位置（§8 状态保持）。
 * 只存在 sessionStorage，关闭标签页即失效。
 */
const PREFIX = 'iot-admin:scroll:';

function key(url: string): string {
  return PREFIX + url;
}

export function rememberScroll(url: string, y: number): void {
  try {
    window.sessionStorage.setItem(key(url), String(Math.max(0, Math.round(y))));
  } catch {
    // 隐私模式下 sessionStorage 不可用，降级为不记忆，不影响主流程。
  }
}

export function takeScroll(url: string): number | null {
  try {
    const raw = window.sessionStorage.getItem(key(url));
    if (raw === null) return null;
    const y = Number(raw);
    return Number.isFinite(y) && y >= 0 ? y : null;
  } catch {
    return null;
  }
}

export function forgetScroll(url: string): void {
  try {
    window.sessionStorage.removeItem(key(url));
  } catch {
    // 同上，忽略。
  }
}
