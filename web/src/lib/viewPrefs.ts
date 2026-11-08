/** 列表视图偏好：按实体分别记忆，刷新/重开浏览器都保持。 */
export type ViewMode = 'table' | 'card';
export type CardDensity = 'comfortable' | 'compact';

const viewKey = (entity: string) => `iot.list.view.${entity}`;
const densityKey = (entity: string) => `iot.list.density.${entity}`;

export function readViewMode(entity: string): ViewMode {
  try {
    return localStorage.getItem(viewKey(entity)) === 'card' ? 'card' : 'table';
  } catch {
    return 'table';
  }
}

export function writeViewMode(entity: string, mode: ViewMode): void {
  try {
    localStorage.setItem(viewKey(entity), mode);
  } catch {
    // 隐私模式下 localStorage 可能不可写：视图仍生效，只是不持久化。
  }
}

export function readDensity(entity: string): CardDensity {
  try {
    return localStorage.getItem(densityKey(entity)) === 'compact' ? 'compact' : 'comfortable';
  } catch {
    return 'comfortable';
  }
}

export function writeDensity(entity: string, density: CardDensity): void {
  try {
    localStorage.setItem(densityKey(entity), density);
  } catch {
    // 同上。
  }
}
