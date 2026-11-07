import { Link } from 'react-router';
import type { BreadcrumbItem } from '../lib/navGroups';

/**
 * 面包屑：贴在固定顶栏里（外壳固定后顶栏不滚，面包屑也就不会滚走）。
 *
 * 规则：
 * - 分组没有落地页 → 渲染成普通文本，**不用**禁用态按钮伪装（禁用按钮会让读屏用户
 *   以为"这里本来能点"）。
 * - 最后一项是当前页 → aria-current="page" 且不可点。
 * - 中间的可点项是普通链接。
 */
export function Breadcrumb({ items }: { items: BreadcrumbItem[] }) {
  return (
    <nav aria-label="面包屑" className="mb-1 flex min-w-0 items-center gap-1 text-sm text-muted">
      {items.map((item, index) => {
        const last = index === items.length - 1;
        return (
          <span key={`${item.label}-${index}`} className="flex min-w-0 items-center gap-1">
            {index > 0 && (
              <span aria-hidden="true" className="text-faint">
                /
              </span>
            )}
            {item.href && !last ? (
              <Link to={item.href} className="truncate hover:underline">
                {item.label}
              </Link>
            ) : (
              <span
                className={last ? 'truncate text-muted' : 'truncate'}
                aria-current={last ? 'page' : undefined}
              >
                {item.label}
              </span>
            )}
          </span>
        );
      })}
    </nav>
  );
}
