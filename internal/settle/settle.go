// Package settle 负责小时结算、快照落库与月度分区维护。
package settle

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// RowsQuerier 只取读取配置需要的 PG 能力。
type RowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// WindowConfig 是一次读取的结算窗口配置，按 type_id 精确匹配 > domain_id 匹配 > 全局默认解析。
// 采集管道与结算任务共用这一处口径，SQL 侧不再内联窗口 CTE。
type WindowConfig struct {
	typeWindows   map[int64]int
	domainWindows map[int64]int
	global        int
}

// LoadWindowConfig 读取全部窗口配置（行数 = 类型级 + 域级 + 全局，量级很小）。
func LoadWindowConfig(ctx context.Context, q RowsQuerier) (WindowConfig, error) {
	cfg := WindowConfig{typeWindows: map[int64]int{}, domainWindows: map[int64]int{}}
	rows, err := q.Query(ctx, `select type_id, domain_id, window_seconds from settle_config`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var typeID, domainID *int64
		var window int
		if err := rows.Scan(&typeID, &domainID, &window); err != nil {
			return cfg, err
		}
		switch {
		case typeID != nil:
			cfg.typeWindows[*typeID] = window
		case domainID != nil:
			cfg.domainWindows[*domainID] = window
		default:
			cfg.global = window
		}
	}
	return cfg, rows.Err()
}

// Resolve 返回某终端生效的窗口秒数。
func (c WindowConfig) Resolve(typeID, domainID int64) int {
	if w, ok := c.typeWindows[typeID]; ok {
		return w
	}
	if w, ok := c.domainWindows[domainID]; ok {
		return w
	}
	if c.global > 0 {
		return c.global
	}
	return staging.DefaultWindowSeconds
}

// Settler 每分钟扫描一次可结算的小时。
type Settler struct {
	Pool *pgxpool.Pool
	Rec  *exception.Recorder
	Now  func() time.Time
	Log  *slog.Logger
}

func New(pool *pgxpool.Pool, rec *exception.Recorder, now func() time.Time, log *slog.Logger) *Settler {
	return &Settler{Pool: pool, Rec: rec, Now: now, Log: log}
}

// Run 执行一轮结算：先保证目标月分区存在，再落快照并清暂存。任一步失败记一条 snapshot_failed，下轮重试。
func (s *Settler) Run(ctx context.Context) (int64, error) {
	now := s.Now()
	if err := EnsurePartitions(ctx, s.Pool, now); err != nil {
		s.fail(ctx, now, err)
		return 0, err
	}
	n, err := s.settle(ctx, now)
	if err != nil {
		s.fail(ctx, now, err)
		return 0, err
	}
	if n > 0 {
		s.Log.Info("小时结算完成", "rows", n, "now", now.Format(time.RFC3339))
	}
	return n, nil
}

// Loop 后台每分钟扫描一次，直到 ctx 取消。
func (s *Settler) Loop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Run(ctx); err != nil {
				s.Log.Error("结算失败，下一轮重试", "err", err)
			}
		}
	}
}

func (s *Settler) fail(ctx context.Context, now time.Time, err error) {
	s.Rec.Event(ctx, exception.SnapshotFailed, "", "", staging.HourStart(now), 1, err.Error())
}

// 结算条件唯一口径：小时结束后再等该终端的窗口秒数（闭区间，等于关闭时刻即可结算）。
const (
	insertClosedSQL = `
		insert into hourly_snapshot
		    (terminal_id, item_code, hour_start, value_num, value_text, unit, value_time)
		select s.terminal_id, s.item_code, s.hour_start, s.value_num, s.value_text, s.unit, s.value_time
		from hourly_staging s
		where s.terminal_id = any($2)
		  and s.hour_start + interval '1 hour' + make_interval(secs => $3) <= $1::timestamptz
		on conflict (terminal_id, item_code, hour_start) do nothing`
	deleteClosedSQL = `
		delete from hourly_staging s
		where s.terminal_id = any($2)
		  and s.hour_start + interval '1 hour' + make_interval(secs => $3) <= $1::timestamptz`
)

// settle 在一个事务里完成"取每项最后值 -> 幂等插入快照 -> 删除该小时暂存"。
// 窗口按终端生效值分组，分组所需的配置只从 WindowConfig 读取一次。
func (s *Settler) settle(ctx context.Context, now time.Time) (int64, error) {
	ids, err := s.candidates(ctx, now)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	cfg, err := LoadWindowConfig(ctx, s.Pool)
	if err != nil {
		return 0, fmt.Errorf("读取结算窗口配置: %w", err)
	}
	scopes, err := s.terminalScopes(ctx, ids)
	if err != nil {
		return 0, err
	}
	buckets := make(map[int][]string, 2)
	for _, id := range ids {
		sc, ok := scopes[id]
		window := staging.DefaultWindowSeconds
		if ok {
			window = cfg.Resolve(sc.typeID, sc.domainID)
		}
		buckets[window] = append(buckets[window], id)
	}
	windows := make([]int, 0, len(buckets))
	for w := range buckets {
		windows = append(windows, w)
	}
	sort.Ints(windows)

	var inserted int64
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, w := range windows {
			tag, err := tx.Exec(ctx, insertClosedSQL, now, buckets[w], w)
			if err != nil {
				return fmt.Errorf("插入快照(window=%d): %w", w, err)
			}
			inserted += tag.RowsAffected()
			if _, err := tx.Exec(ctx, deleteClosedSQL, now, buckets[w], w); err != nil {
				return fmt.Errorf("清理暂存(window=%d): %w", w, err)
			}
		}
		return nil
	})
	return inserted, err
}

// candidates 返回暂存中至少有一条"小时已结束"数据的终端。
func (s *Settler) candidates(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		select distinct terminal_id from hourly_staging where hour_start <= $1::timestamptz - interval '1 hour'`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type terminalScope struct {
	typeID   int64
	domainID int64
}

// terminalScopes 批量读取候选终端的类型与数据域，供窗口解析使用。
func (s *Settler) terminalScopes(ctx context.Context, ids []string) (map[string]terminalScope, error) {
	rows, err := s.Pool.Query(ctx, `
		select t.id, t.type_id, ty.domain_id
		from terminal t join device_type ty on ty.id = t.type_id
		where t.id = any($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]terminalScope, len(ids))
	for rows.Next() {
		var id string
		var sc terminalScope
		if err := rows.Scan(&id, &sc.typeID, &sc.domainID); err != nil {
			return nil, err
		}
		out[id] = sc
	}
	return out, rows.Err()
}
