package exception

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Digest 生成异常样本摘要：hash + 原始长度 + 截断样本。禁止存完整原始报文。
func Digest(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	sum := sha256.Sum256(raw)
	sample := raw
	if len(sample) > 120 {
		sample = sample[:120]
	}
	return fmt.Sprintf("sha256=%x len=%d sample=%q", sum[:8], len(raw), string(sample))
}

// Recorder 写入两张异常表。所有写入失败只记日志，不打断主流程。
type Recorder struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
	Log  *slog.Logger
}

func New(pool *pgxpool.Pool, now func() time.Time, log *slog.Logger) *Recorder {
	return &Recorder{Pool: pool, Now: now, Log: log}
}

// Event 累加事件型异常；hour 必须是已取整到小时的时间戳。
func (r *Recorder) Event(ctx context.Context, kind ErrorType, terminalID, gatewayID string, hour time.Time, n int, sample string) {
	if n <= 0 {
		n = 1
	}
	digest := Digest([]byte(sample))
	now := r.Now()
	_, err := r.Pool.Exec(ctx, `
		insert into exception_count (terminal_id, gateway_id, hour_start, error_type, count, sample_digest, first_seen, last_seen)
		values ($1, $2, $3, $4, $5, nullif($6, ''), $7, $7)
		on conflict (terminal_id, hour_start, error_type) do update
		set count = exception_count.count + excluded.count,
		    last_seen = greatest(exception_count.last_seen, excluded.last_seen),
		    first_seen = least(exception_count.first_seen, excluded.first_seen),
		    sample_digest = coalesce(exception_count.sample_digest, excluded.sample_digest)`,
		terminalID, gatewayID, hour, string(kind), n, digest, now)
	if err != nil {
		r.Log.Error("记录事件型异常失败", "type", kind, "terminal", terminalID, "err", err)
	}
}

// OpenInterval 开启状态型异常区间。
// 已有未结束区间时由 EXCLUDE 约束拦截（23P01），视为已开启，不产生第二条。
// 抖动合并：上一条已关闭区间距本次开启 < 5 分钟则并入上一条，raw_count 累加。
// 本次开启的区间时长此刻为 0（< 5 分钟），故只需判定间隔。
func (r *Recorder) OpenInterval(ctx context.Context, kind ErrorType, terminalID, gatewayID string, itemCode *string) error {
	now := r.Now()
	var id int64
	err := r.Pool.QueryRow(ctx, `
		insert into exception_interval (terminal_id, gateway_id, item_code, error_type, period)
		values ($1, $2, $3, $4, tstzrange($5, null))
		returning id`, terminalID, gatewayID, itemCode, string(kind), now).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23P01" {
			return nil // 已存在未结束区间，不重复开启
		}
		return err
	}
	var prevID int64
	var prevCount int
	err = r.Pool.QueryRow(ctx, `
		select id, raw_count from exception_interval
		where terminal_id = $1 and error_type = $2
		  and coalesce(item_code, '') = coalesce($3, '')
		  and not upper_inf(period)
		  and upper(period) >= $4::timestamptz - interval '5 minutes'
		order by upper(period) desc
		limit 1`, terminalID, string(kind), itemCode, now).Scan(&prevID, &prevCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from exception_interval where id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			update exception_interval
			set period = tstzrange(lower(period), null), raw_count = $2
			where id = $1`, prevID, prevCount+1)
		return err
	})
	return err
}

// CloseInterval 关闭未结束区间。本期平台没有"恢复信号"来源，契约测试与后续接入使用。
func (r *Recorder) CloseInterval(ctx context.Context, kind ErrorType, terminalID string, itemCode *string) error {
	_, err := r.Pool.Exec(ctx, `
		update exception_interval
		set period = tstzrange(lower(period), $4)
		where terminal_id = $1 and error_type = $2
		  and coalesce(item_code, '') = coalesce($3, '')
		  and upper_inf(period)`, terminalID, string(kind), itemCode, r.Now())
	return err
}
