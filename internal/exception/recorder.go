package exception

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

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

// Recorder 写入异常计数表。所有写入失败只记日志，不打断主流程。
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
