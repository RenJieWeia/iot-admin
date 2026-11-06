import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * CSS Modules 不做驼峰转换：`AppShell.module.css` 里写 `.skip-link`，TSX 里写 `styles.skipLink`
 * 会得到 `undefined`（类名丢失、元素失去定位，整页 grid 被重排）。
 * jsdom 测不出布局，但「引用集合 ⊆ 导出集合」这条测得出，见 REVIEW-004 F1 的护栏要求。
 */
const SRC = join(process.cwd(), 'src');

function walk(dir: string, suffix: RegExp): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) return walk(full, suffix);
    return suffix.test(entry) ? [full] : [];
  });
}

function exportedClasses(cssPath: string): Set<string> {
  const css = readFileSync(cssPath, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const names = new Set<string>();
  // 选择器里出现的 .className；值里的 0.5 之类不会命中（点后必须以字母/下划线/短横线开头）。
  for (const match of css.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) {
    const name = match[1];
    if (name) names.add(name);
  }
  return names;
}

const moduleFiles = walk(SRC, /\.module\.css$/);
const sourceFiles = walk(SRC, /\.(tsx?|ts)$/);

interface Violation {
  file: string;
  alias: string;
  key: string;
  module: string;
}

const violations: Violation[] = [];
let checkedReferences = 0;

for (const file of sourceFiles) {
  if (/[\\/](test|__tests__)[\\/]/.test(file) || file.endsWith('.test.ts') || file.endsWith('.test.tsx')) {
    continue;
  }
  const code = readFileSync(file, 'utf8');
  for (const imp of code.matchAll(
    /import\s+(\w+)\s+from\s+['"]([^'"]+\.module\.css)['"]/g,
  )) {
    const alias = imp[1];
    const spec = imp[2];
    if (!alias || !spec) continue;
    const modulePath = resolve(dirname(file), spec);
    const classes = exportedClasses(modulePath);
    const pattern = new RegExp(`\\b${alias}\\.([A-Za-z_$][\\w$]*)`, 'g');
    for (const use of code.matchAll(pattern)) {
      const key = use[1];
      if (!key) continue;
      checkedReferences += 1;
      if (!classes.has(key)) {
        violations.push({ file, alias, key, module: modulePath });
      }
    }
  }
}

describe('CSS Modules 类名一致性', () => {
  it('每个 styles.<key> 引用都能在对应 *.module.css 里找到同名类', () => {
    expect(moduleFiles.length).toBeGreaterThan(0);
    expect(checkedReferences).toBeGreaterThan(0);
    expect(violations.map((v) => `${v.file} → ${v.alias}.${v.key}（${v.module} 无此类）`)).toEqual([]);
  });

  it('AppShell 的 skipLink 类确实存在（REVIEW-004 F1 回归）', () => {
    const classes = exportedClasses(join(SRC, 'components/AppShell.module.css'));
    expect(classes.has('skipLink')).toBe(true);
  });
});
