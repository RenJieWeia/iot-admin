import type { ReactNode } from 'react';
import { NavLink } from 'react-router';
import { useMeta } from '../api/meta';
import { cn } from '../lib/cn';

interface Props {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'block min-h-11 rounded-sm px-3 py-2 leading-lg text-text hover:bg-neutral-bg hover:no-underline',
    isActive && 'bg-neutral-bg font-semibold',
  );

/**
 * 外壳：左侧固定导航（6 实体 + 数据只读页 + 异常两页）+ 主内容区。
 * 导航文案来自 /admin/v1/_meta，前端不硬编码业务词（R-019）。
 */
export function AppShell({ title, actions, children }: Props) {
  const meta = useMeta();
  const entities = meta.data?.entities ?? [];
  const exceptions = meta.data?.exceptions ?? [];
  const readonly = meta.data?.readonly ?? [];

  return (
    <div className="shell">
      <a className="skip-link focus:skip-link-focus" href="#main">
        跳到主内容
      </a>
      <aside className="col-start-1 border-r border-border bg-surface px-3 py-4 max-lg:border-b max-lg:border-r-0">
        <p className="mb-4 mt-0 text-lg font-semibold">IoT 管理后台</p>
        <nav aria-label="主导航">
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {entities.map((entity) => (
              <li key={entity.name}>
                <NavLink to={`/${entity.name}`} className={navLinkClass}>
                  {entity.title}
                </NavLink>
              </li>
            ))}
          </ul>
          {readonly.length > 0 && (
            <>
              <p className="mb-1 mt-4 px-3 text-sm text-muted">数据</p>
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {readonly.map((slot) => (
                  <li key={slot.slot}>
                    <NavLink to={`/readonly/${slot.slot}`} className={navLinkClass}>
                      {slot.title}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </>
          )}
          {exceptions.length > 0 && (
            <>
              <p className="mb-1 mt-4 px-3 text-sm text-muted">异常</p>
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {exceptions.map((slot) => (
                  <li key={slot.slot}>
                    <NavLink to={`/exceptions/${slot.slot}`} className={navLinkClass}>
                      {slot.title}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </>
          )}
          {meta.isError && (
            <p className="mb-1 mt-4 px-3 text-sm text-muted" role="status">
              导航加载失败，请刷新重试。
            </p>
          )}
        </nav>
      </aside>
      <main id="main" className="col-start-2 min-w-0 p-6 max-lg:col-start-1 max-lg:p-4">
        <header className="mb-4 flex items-center justify-between gap-4">
          <h1 className="m-0 text-xl leading-lg">{title}</h1>
          {actions}
        </header>
        {children}
      </main>
    </div>
  );
}
