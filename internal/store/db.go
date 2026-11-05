// Package store 负责 PG 连接与迁移执行。
package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect 建立连接池。会话时区固定 Asia/Shanghai，避免 hour_start 取整受服务端默认时区影响。
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Migrate 按文件名顺序执行 migrations/*.sql，已执行的跳过。每个文件一个事务。
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) error {
	entries, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	if _, err := pool.Exec(ctx, `create table if not exists schema_migrations (
		version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	for _, name := range entries {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var applied bool
			if err := tx.QueryRow(ctx,
				`select exists(select 1 from schema_migrations where version = $1)`, name).Scan(&applied); err != nil {
				return err
			}
			if applied {
				return nil
			}
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("apply %s: %w", name, err)
			}
			_, err := tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, name)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
