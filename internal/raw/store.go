// Package raw 负责接入流水：整包原文 + 逐项清洗结果，用于异常现场回放。
package raw

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 读写 raw_message / raw_item。
type Store struct {
	Pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

// execer 让写入既能走连接池，也能挂在调用方事务里。
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Item 是一条待落库的数据项（成功项与被剔除项都记）。
type Item struct {
	TerminalID    string
	ItemCode      string
	ValueNum      *float64
	ValueText     *string
	Unit          string
	ValueTime     time.Time
	Quality       int16
	QualityReason string
	Status        string // written | dropped | ignored
	Reason        string
	RawValue      []byte // JSON；nil 表示不记录原值
}

// InsertMessage 写入整包原文，返回流水 ID。
func (s *Store) InsertMessage(ctx context.Context, gatewayID, source string, receivedAt time.Time,
	payload []byte, terminalCount int) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		insert into raw_message (gateway_id, source, received_at, payload, terminal_count, byte_size)
		values ($1, $2, $3, $4::jsonb, $5, $6)
		returning id`,
		gatewayID, source, receivedAt, string(payload), terminalCount, len(payload)).Scan(&id)
	return id, err
}

// InsertItems 把一批数据项写入流水；messageID=0 时允许（理论上不会发生）。
func InsertItems(ctx context.Context, q execer, messageID int64, receivedAt time.Time, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	for _, it := range items {
		if _, err := q.Exec(ctx, `
			insert into raw_item (message_id, received_at, terminal_id, item_code, value_num, value_text,
			                      unit, value_time, quality, quality_reason, status, reason, raw_value)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb)`,
			messageID, receivedAt, it.TerminalID, it.ItemCode, it.ValueNum, it.ValueText,
			it.Unit, nullTime(it.ValueTime), it.Quality, it.QualityReason, it.Status, it.Reason,
			nullJSON(it.RawValue)); err != nil {
			return err
		}
	}
	return nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nullJSON(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

// RetentionDays 读取保留配置；nil 表示永久保留。
func (s *Store) RetentionDays(ctx context.Context) (*int, error) {
	var days *int
	err := s.Pool.QueryRow(ctx, `select retention_days from raw_retention where id = 1`).Scan(&days)
	if err != nil {
		return nil, err
	}
	return days, nil
}

// EnsurePartitions 续建当天 ~ +3 天的日分区（幂等）。
func (s *Store) EnsurePartitions(ctx context.Context, now time.Time) error {
	day := now.Truncate(24 * time.Hour)
	for i := 0; i <= 3; i++ {
		start := day.AddDate(0, 0, i)
		end := start.AddDate(0, 0, 1)
		for _, parent := range []string{"raw_message", "raw_item"} {
			name := fmt.Sprintf("%s_%s", parent, start.Format("20060102"))
			stmt := fmt.Sprintf(
				"create table if not exists %s partition of %s for values from ('%s') to ('%s')",
				name, parent, start.Format(time.RFC3339), end.Format(time.RFC3339))
			if _, err := s.Pool.Exec(ctx, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

// PurgeExpired 按配置 drop 过期日分区；未配置（NULL）时什么都不删。
// 返回被删除的分区数。
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	days, err := s.RetentionDays(ctx)
	if err != nil || days == nil || *days <= 0 {
		return 0, err
	}
	cutoff := now.Truncate(24*time.Hour).AddDate(0, 0, -*days)
	dropped := 0
	for _, parent := range []string{"raw_message", "raw_item"} {
		rows, err := s.Pool.Query(ctx, `
			select c.relname
			from pg_class c
			join pg_inherits i on i.inhrelid = c.oid
			where i.inhparent = $1::regclass`, parent)
		if err != nil {
			return dropped, err
		}
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return dropped, err
			}
			names = append(names, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return dropped, err
		}
		for _, name := range names {
			start, ok := partitionStart(name)
			if !ok {
				continue
			}
			if start.AddDate(0, 0, 1).After(cutoff) {
				continue
			}
			if _, err := s.Pool.Exec(ctx, "drop table if exists "+name); err != nil {
				return dropped, err
			}
			dropped++
		}
	}
	return dropped, nil
}

// partitionStart 从 raw_message_YYYYMMDD / raw_item_YYYYMMDD 解析分区起点。
func partitionStart(name string) (time.Time, bool) {
	var date string
	if len(name) > 12 {
		date = name[len(name)-8:]
	}
	if len(date) != 8 {
		return time.Time{}, false
	}
	day, err := time.ParseInLocation("20060102", date, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

// Maintain 建未来分区并按配置清理过期分区（未配置保留期时只建分区）。
func (s *Store) Maintain(ctx context.Context, now time.Time) (int, error) {
	if err := s.EnsurePartitions(ctx, now); err != nil {
		return 0, err
	}
	return s.PurgeExpired(ctx, now)
}
