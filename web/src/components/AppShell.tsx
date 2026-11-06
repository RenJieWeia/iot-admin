import {
  Activity,
  AlertTriangle,
  Boxes,
  Circle,
  Clock,
  Cpu,
  Database,
  History,
  LayoutList,
  Menu,
  Settings2,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { NavLink } from 'react-router';
import { useMeta } from '../api/meta';
import { cn } from '../lib/cn';
import { Drawer } from './Drawer';

interface Props {
  title: string;
  /** 标题下的副标题（面包屑或说明行）。 */
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
}

/** 与 tailwind.css 里 `shell` 的断点一致：≤1024px 单列，侧栏收进抽屉。 */
const NARROW_QUERY = '(max-width: 1024px)';

/**
 * 窄屏判定：宽度是浏览器状态，只能问 matchMedia。
 * jsdom/旧环境没有 matchMedia 时按桌面处理，保持「侧栏常驻」的既有行为。
 */
function useIsNarrow(): boolean {
  const [narrow, setNarrow] = useState(
    () => typeof window.matchMedia === 'function' && window.matchMedia(NARROW_QUERY).matches,
  );

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(NARROW_QUERY);
    const sync = () => setNarrow(query.matches);
    sync();
    query.addEventListener('change', sync);
    return () => query.removeEventListener('change', sync);
  }, []);

  return narrow;
}

/** 实体/只读页/异常页的图标：按 name 映射，未知实体回退圆点。 */
const ICONS: Record<string, LucideIcon> = {
  'data-domains': Database,
  'device-types': Boxes,
  'thing-model-properties': LayoutList,
  terminals: Cpu,
  'quality-rules': ShieldCheck,
  'settle-configs': Settings2,
  'latest-values': Activity,
  'hourly-staging': History,
  'hourly-snapshot': Clock,
  'exception-events': AlertTriangle,
  'exception-intervals': Clock,
};

const navItemClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'flex h-9 items-center gap-3 rounded-control px-3 text-body text-text hover:bg-surface touch-target',
    isActive && 'nav-item-active',
  );

function GroupTitle({ children }: { children: ReactNode }) {
  return <p className="mb-1 mt-4 px-3 text-nav uppercase tracking-nav text-faint">{children}</p>;
}

/**
 * 导航列表：桌面放在侧栏，窄屏放进抽屉；两处共用同一份 /_meta 驱动的内容。
 * 导航文案与顺序全部来自 /_meta，前端不硬编码业务词（R-019）。
 */
function SidebarNav({ id, onNavigate }: { id?: string; onNavigate?: () => void }) {
  const meta = useMeta();
  const entities = meta.data?.entities ?? [];
  const exceptions = meta.data?.exceptions ?? [];
  const readonly = meta.data?.readonly ?? [];

  const renderItem = (name: string, label: string, to: string) => {
    const Icon = ICONS[name] ?? Circle;
    return (
      <li key={name}>
        <NavLink to={to} className={navItemClass} onClick={onNavigate}>
          <Icon size={16} aria-hidden="true" className="shrink-0 text-muted" />
          <span>{label}</span>
        </NavLink>
      </li>
    );
  };

  return (
    <nav id={id} aria-label="主导航">
      <GroupTitle>实体</GroupTitle>
      <ul className="m-0 flex list-none flex-col gap-1 p-0">
        {entities.map((entity) => renderItem(entity.name, entity.title, `/${entity.name}`))}
      </ul>
      {readonly.length > 0 && (
        <>
          <GroupTitle>数据</GroupTitle>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {readonly.map((slot) => renderItem(slot.name, slot.title, `/readonly/${slot.slot}`))}
          </ul>
        </>
      )}
      {exceptions.length > 0 && (
        <>
          <GroupTitle>异常</GroupTitle>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {exceptions.map((slot) => renderItem(slot.name, slot.title, `/exceptions/${slot.slot}`))}
          </ul>
        </>
      )}
      {meta.isError && (
        <p className="mb-1 mt-4 px-3 text-meta text-muted" role="status">
          导航加载失败，请刷新重试。
        </p>
      )}
    </nav>
  );
}

/**
 * 外壳：220px 侧栏（surface-2）+ 56px 顶栏（标题/面包屑 + 主操作）+ 内容白卡。
 * 窄屏（≤1024px）侧栏收进抽屉，由顶栏左侧的菜单按钮触发（admin-ui-spec §3.1）。
 */
export function AppShell({ title, subtitle, actions, children }: Props) {
  const narrow = useIsNarrow();
  const [menuOpen, setMenuOpen] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef(false);

  // 回到桌面宽度后抽屉不再存在；不重置的话再变窄会莫名弹出。
  useEffect(() => {
    if (!narrow) setMenuOpen(false);
  }, [narrow]);

  // 打开抽屉的不是 Radix 的 Trigger，关闭后它不会自动归还焦点，这里手动交还（§14.2）。
  const closeMenu = () => {
    restoreFocusRef.current = true;
    setMenuOpen(false);
  };

  useEffect(() => {
    if (menuOpen || !restoreFocusRef.current) return;
    restoreFocusRef.current = false;
    menuButtonRef.current?.focus();
  }, [menuOpen]);

  return (
    <div className="shell">
      <a className="skip-link focus:skip-link-focus" href="#main">
        跳到主内容
      </a>
      {narrow ? (
        <Drawer
          open={menuOpen}
          title="IoT 管理后台"
          description="在实体、数据与异常页之间切换"
          onClose={closeMenu}
        >
          <SidebarNav id="nav-drawer" onNavigate={closeMenu} />
        </Drawer>
      ) : (
        <aside className="col-start-1 border-r border-border bg-surface-2 px-3 py-4 max-lg:border-b max-lg:border-r-0">
          <p className="mb-4 mt-0 px-3 text-section font-semibold">IoT 管理后台</p>
          <SidebarNav />
        </aside>
      )}

      <div className="col-start-2 flex min-w-0 flex-col max-lg:col-start-1">
        <header className="app-header flex items-center justify-between gap-4 px-6 max-lg:px-4">
          <div className="flex min-w-0 items-center gap-2">
            {narrow && (
              <button
                type="button"
                ref={menuButtonRef}
                className="btn shrink-0"
                aria-label="打开导航菜单"
                aria-expanded={menuOpen}
                aria-controls="nav-drawer"
                onClick={() => setMenuOpen(true)}
              >
                <Menu size={16} aria-hidden="true" />
              </button>
            )}
            <div className="min-w-0">
              <h1 className="m-0 truncate text-page font-semibold leading-page">{title}</h1>
              {subtitle && <p className="m-0 text-meta leading-meta text-muted">{subtitle}</p>}
            </div>
          </div>
          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </header>
        <main id="main" className="min-w-0 flex-1 p-6 max-lg:p-4">
          <section className="content-card p-5">{children}</section>
        </main>
      </div>
    </div>
  );
}
