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
  Settings2,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react';
import type { ReactNode } from 'react';
import { NavLink } from 'react-router';
import { useMeta } from '../api/meta';
import { cn } from '../lib/cn';

interface Props {
  title: string;
  /** 标题下的副标题（面包屑或说明行）。 */
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
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
    'flex h-9 items-center gap-3 rounded-control px-3 text-body text-text hover:bg-surface',
    isActive && 'nav-item-active font-medium',
  );

function GroupTitle({ children }: { children: ReactNode }) {
  return <p className="mb-1 mt-4 px-3 text-nav uppercase tracking-nav text-faint">{children}</p>;
}

/**
 * 外壳：220px 侧栏（surface-2）+ 56px 顶栏（标题/面包屑 + 主操作）+ 内容白卡。
 * 导航文案与顺序全部来自 /_meta，前端不硬编码业务词（R-019）。
 */
export function AppShell({ title, subtitle, actions, children }: Props) {
  const meta = useMeta();
  const entities = meta.data?.entities ?? [];
  const exceptions = meta.data?.exceptions ?? [];
  const readonly = meta.data?.readonly ?? [];

  const renderItem = (name: string, label: string, to: string) => {
    const Icon = ICONS[name] ?? Circle;
    return (
      <li key={name}>
        <NavLink to={to} className={navItemClass}>
          <Icon size={16} aria-hidden="true" className="shrink-0 text-muted" />
          <span>{label}</span>
        </NavLink>
      </li>
    );
  };

  return (
    <div className="shell">
      <a className="skip-link focus:skip-link-focus" href="#main">
        跳到主内容
      </a>
      <aside className="col-start-1 border-r border-border bg-surface-2 px-3 py-4 max-lg:border-b max-lg:border-r-0">
        <p className="mb-4 mt-0 px-3 text-section font-semibold">IoT 管理后台</p>
        <nav aria-label="主导航">
          <GroupTitle>实体</GroupTitle>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {entities.map((entity) => renderItem(entity.name, entity.title, `/${entity.name}`))}
          </ul>
          {readonly.length > 0 && (
            <>
              <GroupTitle>数据</GroupTitle>
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {readonly.map((slot) =>
                  renderItem(slot.name, slot.title, `/readonly/${slot.slot}`),
                )}
              </ul>
            </>
          )}
          {exceptions.length > 0 && (
            <>
              <GroupTitle>异常</GroupTitle>
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {exceptions.map((slot) =>
                  renderItem(slot.name, slot.title, `/exceptions/${slot.slot}`),
                )}
              </ul>
            </>
          )}
          {meta.isError && (
            <p className="mb-1 mt-4 px-3 text-meta text-muted" role="status">
              导航加载失败，请刷新重试。
            </p>
          )}
        </nav>
      </aside>

      <div className="col-start-2 flex min-w-0 flex-col max-lg:col-start-1">
        <header className="app-header flex items-center justify-between gap-4 px-6 max-lg:px-4">
          <div className="min-w-0">
            <h1 className="m-0 truncate text-page font-semibold leading-page">{title}</h1>
            {subtitle && <p className="m-0 text-meta leading-meta text-muted">{subtitle}</p>}
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
