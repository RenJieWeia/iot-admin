// Package settle 负责小时结算、快照落库与月度分区维护。
package settle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// Querier 只取窗口查询需要的 PG 能力。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WindowSeconds 解析结算窗口：type_id 精确匹配 > domain_id 匹配 > 全局默认 900 秒。
func WindowSeconds(ctx context.Context, q Querier, typeID, domainID int64) (int, error) {
	var w int
	err := q.QueryRow(ctx, `
		select window_seconds from settle_config
		where (type_id = $1) or (type_id is null and domain_id = $2) or (type_id is null and domain_id is null)
		order by (type_id is not null) desc, (domain_id is not null) desc
		limit 1`, typeID, domainID).Scan(&w)
	if errors.Is(err, pgx.ErrNoRows) {
		return staging.DefaultWindowSeconds, nil
	}
	return w, err
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

// settle 在一个事务里完成"取每项最后值 -> 幂等插入快照 -> 删除该小时暂存"。
// 窗口关闭时刻 = hour_start + 1h + window（小时结束后再等窗口）。
func (s *Settler) settle(ctx context.Context, now time.Time) (int64, error) {
	var inserted int64
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			with cfg as (
			    select t.id as terminal_id,
			           coalesce(
			               (select sc.window_seconds from settle_config sc where sc.type_id = t.type_id),
			               (select sc.window_seconds from settle_config sc where sc.type_id is null and sc.domain_id = ty.domain_id),
			               (select sc.window_seconds from settle_config sc where sc.type_id is null and sc.domain_id is null),
			               900) as window_seconds
			    from terminal t join device_type ty on ty.id = t.type_id
			)
			insert into hourly_snapshot
			    (terminal_id, item_code, hour_start, value_num, value_text, unit, value_time)
			select s.terminal_id, s.item_code, s.hour_start, s.value_num, s.value_text, s.unit, s.value_time
			from hourly_staging s
			left join cfg c on c.terminal_id = s.terminal_id
			where s.hour_start + interval '1 hour' + make_interval(secs => coalesce(c.window_seconds, 900)) <= $1
			on conflict (terminal_id, item_code, hour_start) do nothing`, now)
		if err != nil {
			return fmt.Errorf("插入快照: %w", err)
		}
		inserted = tag.RowsAffected()
		if _, err := tx.Exec(ctx, `
			with cfg as (
			    select t.id as terminal_id,
			           coalesce(
			               (select sc.window_seconds from settle_config sc where sc.type_id = t.type_id),
			               (select sc.window_seconds from settle_config sc where sc.type_id is null and sc.domain_id = ty.domain_id),
			               (select sc.window_seconds from settle_config sc where sc.type_id is null and sc.domain_id is null),
			               900) as window_seconds
			    from terminal t join device_type ty on ty.id = t.type_id
			), closed as (
			    select s.terminal_id, s.item_code, s.hour_start
			    from hourly_staging s
			    left join cfg c on c.terminal_id = s.terminal_id
			    where s.hour_start + interval '1 hour' + make_interval(secs => coalesce(c.window_seconds, 900)) <= $1
			)
			delete from hourly_staging s using closed x
			where s.terminal_id = x.terminal_id and s.item_code = x.item_code and s.hour_start = x.hour_start`, now); err != nil {
			return fmt.Errorf("清理暂存: %w", err)
		}
		return nil
	})
	return inserted, err
}
