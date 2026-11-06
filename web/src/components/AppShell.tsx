import type { ReactNode } from 'react';
import { NavLink } from 'react-router';
import { useMeta } from '../api/meta';
import styles from './AppShell.module.css';

interface Props {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}

/**
 * 外壳：左侧固定导航（6 实体 + 异常两页）+ 主内容区。
 * 导航文案来自 /admin/v1/_meta，前端不硬编码业务词（R-019）。
 */
export function AppShell({ title, actions, children }: Props) {
  const meta = useMeta();
  const entities = meta.data?.entities ?? [];
  const exceptions = meta.data?.exceptions ?? [];
  const readonly = meta.data?.readonly ?? [];

  return (
    <div className={styles.shell}>
      <a className={styles.skipLink} href="#main">
        跳到主内容
      </a>
      <aside className={styles.sidebar}>
        <p className={styles.brand}>IoT 管理后台</p>
        <nav aria-label="主导航">
          <ul className={styles.nav}>
            {entities.map((entity) => (
              <li key={entity.name}>
                <NavLink
                  to={`/${entity.name}`}
                  className={({ isActive }) =>
                    isActive ? `${styles.navLink} ${styles.navLinkActive}` : styles.navLink
                  }
                >
                  {entity.title}
                </NavLink>
              </li>
            ))}
          </ul>
          {readonly.length > 0 && (
            <>
              <p className={styles.sectionTitle}>数据</p>
              <ul className={styles.nav}>
                {readonly.map((slot) => (
                  <li key={slot.slot}>
                    <NavLink
                      to={`/readonly/${slot.slot}`}
                      className={({ isActive }) =>
                        isActive ? `${styles.navLink} ${styles.navLinkActive}` : styles.navLink
                      }
                    >
                      {slot.title}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </>
          )}
          {exceptions.length > 0 && (
            <>
              <p className={styles.sectionTitle}>异常</p>
              <ul className={styles.nav}>
                {exceptions.map((slot) => (
                  <li key={slot.slot}>
                    <NavLink
                      to={`/exceptions/${slot.slot}`}
                      className={({ isActive }) =>
                        isActive ? `${styles.navLink} ${styles.navLinkActive}` : styles.navLink
                      }
                    >
                      {slot.title}
                    </NavLink>
                  </li>
                ))}
              </ul>
            </>
          )}
          {meta.isError && (
            <p className={styles.sectionTitle} role="status">
              导航加载失败，请刷新重试。
            </p>
          )}
        </nav>
      </aside>
      <main id="main" className={styles.main}>
        <header className={styles.header}>
          <h1 className={styles.title}>{title}</h1>
          {actions}
        </header>
        {children}
      </main>
    </div>
  );
}
