import { useEffect, useMemo, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import { Cpu, Menu, Moon, Sun } from 'lucide-react';
import type { MetaPayload } from '../api/types';
import { cn } from '../lib/cn';
import { useMeta } from '../meta';

const THEME_KEY = 'iot-theme';
type Theme = 'light' | 'dark';

interface NavItem {
  to: string;
  label: string;
}

interface NavSection {
  title: string;
  items: NavItem[];
}

/** 导航分组：业务（非 snapshot- 实体）、快照（snapshot- 实体与只读页）、观测（其余只读页与异常页）。 */
export function navSections(meta: MetaPayload): NavSection[] {
  const entities = meta.entities.map((e) => ({ name: e.name, title: e.title }));
  const pages = meta.readonly ?? [];
  const isSnapshot = (name: string) => name.startsWith('snapshot-');

  const sections: NavSection[] = [
    {
      title: '业务',
      items: entities
        .filter((e) => !isSnapshot(e.name))
        .map((e) => ({ to: `/e/${e.name}`, label: e.title })),
    },
    {
      title: '快照',
      items: [
        ...entities
          .filter((e) => isSnapshot(e.name))
          .map((e) => ({ to: `/e/${e.name}`, label: e.title })),
        ...pages
          .filter((p) => isSnapshot(p.slot))
          .map((p) => ({ to: `/r/${p.slot}`, label: p.title })),
      ],
    },
    {
      title: '观测',
      items: [
        ...pages
          .filter((p) => !isSnapshot(p.slot))
          .map((p) => ({ to: `/r/${p.slot}`, label: p.title })),
        ...meta.exceptions.map((p) => ({ to: `/x/${p.slot}`, label: p.title })),
      ],
    },
  ];
  return sections.filter((s) => s.items.length > 0);
}

/** `/` 重定向目标：导航第一项。 */
export function firstPath(meta: MetaPayload): string {
  return navSections(meta).flatMap((s) => s.items)[0]?.to ?? '';
}

function readTheme(): Theme {
  const saved = localStorage.getItem(THEME_KEY);
  if (saved === 'light' || saved === 'dark') return saved;
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle('dark', theme === 'dark');
}

/** 首帧前调用，避免暗色用户白闪。 */
export function applyStoredTheme() {
  applyTheme(readTheme());
}

export function AppShell() {
  const meta = useMeta();
  const { pathname } = useLocation();
  const [navOpen, setNavOpen] = useState(false);
  const [theme, setTheme] = useState<Theme>(readTheme);

  const sections = useMemo(() => navSections(meta), [meta]);
  const title = useMemo(() => {
    const items = sections.flatMap((s) => s.items);
    // 详情页（如 /e/device-types/1）按前缀归到所属导航项。
    return (
      items.find((item) => item.to === pathname)?.label ??
      items.find((item) => pathname.startsWith(item.to + '/'))?.label ??
      '控制台'
    );
  }, [sections, pathname]);

  useEffect(() => applyTheme(theme), [theme]);
  useEffect(() => setNavOpen(false), [pathname]);

  useEffect(() => {
    if (!navOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setNavOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navOpen]);

  const nextTheme: Theme = theme === 'dark' ? 'light' : 'dark';
  const toggleTheme = () => {
    localStorage.setItem(THEME_KEY, nextTheme);
    setTheme(nextTheme);
  };

  return (
    <div className="min-h-screen bg-canvas text-fg">
      {navOpen && (
        <div
          className="fixed inset-0 z-30 bg-fg/40 lg:hidden"
          onClick={() => setNavOpen(false)}
          aria-hidden
        />
      )}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 flex w-60 flex-col border-r border-line bg-surface transition-transform duration-200 lg:translate-x-0',
          navOpen ? 'translate-x-0' : 'invisible -translate-x-full lg:visible',
        )}
        aria-label="侧边导航"
      >
        <div className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-4">
          <Cpu className="size-4 text-primary" aria-hidden />
          <span className="text-sm font-semibold tracking-tight">IoT 控制台</span>
        </div>
        <nav className="flex-1 overflow-y-auto px-2 py-2">
          {sections.map((section) => (
            <div key={section.title} className="pb-1">
              <p className="px-3 pt-3 pb-1 text-xs font-medium text-muted">{section.title}</p>
              <ul>
                {section.items.map((item) => (
                  <li key={item.to}>
                    <NavLink
                      to={item.to}
                      className={({ isActive }) =>
                        cn(
                          'block truncate rounded-md px-3 py-1.5 text-sm transition-colors',
                          isActive
                            ? 'bg-primary-soft font-medium text-primary'
                            : 'text-muted hover:bg-neutral-soft hover:text-fg',
                        )
                      }
                    >
                      {item.label}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
      </aside>

      <div className="lg:pl-60">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-line bg-surface px-4">
          <button
            type="button"
            className="rounded-md p-1.5 text-muted hover:bg-neutral-soft hover:text-fg lg:hidden"
            onClick={() => setNavOpen(true)}
            aria-label="打开导航"
          >
            <Menu className="size-4" aria-hidden />
          </button>
          <h1 className="min-w-0 truncate text-sm font-medium">{title}</h1>
          <button
            type="button"
            className="ml-auto rounded-md p-1.5 text-muted hover:bg-neutral-soft hover:text-fg"
            onClick={toggleTheme}
            aria-label={`切换到${nextTheme === 'dark' ? '深色' : '浅色'}主题`}
          >
            {theme === 'dark' ? (
              <Sun className="size-4" aria-hidden />
            ) : (
              <Moon className="size-4" aria-hidden />
            )}
          </button>
        </header>
        <main className="p-4 lg:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
