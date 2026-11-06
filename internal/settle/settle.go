// Package settle 负责小时结算、快照落库与月度分区维护。
package settle

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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
//
// 返回值 = 本轮**处理**的暂存行数（"结算了多少格"）。快照实际受影响行数（含 UPDATE）
// 只进日志 "snapshot_writes"：快照里若已有同 value_time 的行，upsert 影响 0 行，
// 但那批暂存确实被结算并清掉了，所以不能拿受影响行数当结算量。
func (s *Settler) Run(ctx context.Context) (int64, error) {
	now := s.Now()
	if err := EnsurePartitions(ctx, s.Pool, now); err != nil {
		s.fail(ctx, now, err)
		return 0, err
	}
	n, affected, err := s.settle(ctx, now)
	if err != nil {
		s.fail(ctx, now, err)
		return 0, err
	}
	if n > 0 {
		s.Log.Info("小时结算完成", "rows", n, "snapshot_writes", affected, "now", now.Format(time.RFC3339))
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
// 快照写入本身不在这里拼 SQL —— 统一走 UpsertSnapshot（结算与窗口外分流共用一份实现）。
const (
	selectClosedSQL = `
		select s.terminal_id, s.item_code, s.hour_start, s.value_num, s.value_text, s.unit,
		       s.value_time, s.quality, coalesce(s.quality_reason, '')
		from hourly_staging s
		where s.terminal_id = any($2)
		  and s.hour_start + interval '1 hour' + make_interval(secs => $3) <= $1::timestamptz`
	deleteClosedSQL = `
		delete from hourly_staging s
		where s.terminal_id = any($2)
		  and s.hour_start + interval '1 hour' + make_interval(secs => $3) <= $1::timestamptz`
)

// CellRevision 是一次快照写入命中的格子与写入后的版本号（R-034：版本表据此落行）。
type CellRevision struct {
	TerminalID string
	ItemCode   string
	HourStart  time.Time
	Revision   int
}

// UpsertSnapshot 是快照写入的**唯一**实现：结算任务与接入管道（窗口外分流）都调用它。
//
// 版本化语义（R-030）：同一格 (terminal_id, item_code, hour_start) 只有 value_time **更大**时
// 才覆盖，并在 SQL 内原子执行 revision = hourly_snapshot.revision + 1、settled_at = now()；
// value_time 相等或更小则整行跳过，revision 与 settled_at 都不变（幂等）。
//
// 返回值是**实际写入（insert 或 update）的格子**及其写入后的 revision；
// 被 value_time 守卫跳过的行不出现在结果里，调用方据此决定是否写版本表。
func UpsertSnapshot(ctx context.Context, tx pgx.Tx, rows []staging.Row) ([]CellRevision, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(rows))
	args := make([]any, 0, len(rows)*9)
	for i, r := range rows {
		n := i * 9
		placeholders[i] = fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9)
		args = append(args, r.TerminalID, r.ItemCode, r.HourStart,
			r.ValueNum, r.ValueText, r.Unit, r.ValueTime, r.Quality, r.QualityReason)
	}
	rowsOut, err := tx.Query(ctx, `
		insert into hourly_snapshot
		    (terminal_id, item_code, hour_start, value_num, value_text, unit, value_time,
		     quality, quality_reason)
		values `+strings.Join(placeholders, ", ")+`
		on conflict (terminal_id, item_code, hour_start) do update
		set value_num = excluded.value_num, value_text = excluded.value_text,
		    unit = excluded.unit, value_time = excluded.value_time,
		    quality = excluded.quality, quality_reason = excluded.quality_reason,
		    revision = hourly_snapshot.revision + 1, settled_at = now()
		where excluded.value_time > hourly_snapshot.value_time
		returning terminal_id, item_code, hour_start, revision`, args...)
	if err != nil {
		return nil, err
	}
	defer rowsOut.Close()
	var written []CellRevision
	for rowsOut.Next() {
		var c CellRevision
		if err := rowsOut.Scan(&c.TerminalID, &c.ItemCode, &c.HourStart, &c.Revision); err != nil {
			return nil, err
		}
		written = append(written, c)
	}
	return written, rowsOut.Err()
}

// VersionSource 是版本行的来源（R-034）：1 = MQTT 补录（msgType=2）；2 = 手工 API。
const (
	VersionSourceMQTTBackfill int16 = 1
	VersionSourceManualAPI    int16 = 2
)

// VersionRow 是一行版本记录；与快照写入**同一事务**落库。
type VersionRow struct {
	TerminalID string
	ItemCode   string
	HourStart  time.Time
	Revision   int
	Source     int16
	ValueTime  time.Time
	Actor      string // 手工 API 的 operator，可为空
}

// InsertVersions 批量写入版本行；实时写入不得调用（用例锁定 snapshot_cell_history 计数为 0）。
func InsertVersions(ctx context.Context, tx pgx.Tx, rows []VersionRow) error {
	if len(rows) == 0 {
		return nil
	}
	placeholders := make([]string, len(rows))
	args := make([]any, 0, len(rows)*7)
	for i, r := range rows {
		n := i * 7
		placeholders[i] = fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, nullif($%d, ''))",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7)
		args = append(args, r.TerminalID, r.ItemCode, r.HourStart, r.Revision, r.Source, r.ValueTime, r.Actor)
	}
	_, err := tx.Exec(ctx, `
		insert into snapshot_cell_history
		    (terminal_id, item_code, hour_start, revision, source, value_time, actor)
		values `+strings.Join(placeholders, ", "), args...)
	return err
}

// readClosed 读出该批终端"已到关闭时刻"的暂存行，交给 UpsertSnapshot。
func readClosed(ctx context.Context, tx pgx.Tx, now time.Time, ids []string, window int) ([]staging.Row, error) {
	rows, err := tx.Query(ctx, selectClosedSQL, now, ids, window)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []staging.Row
	for rows.Next() {
		var r staging.Row
		if err := rows.Scan(&r.TerminalID, &r.ItemCode, &r.HourStart, &r.ValueNum, &r.ValueText,
			&r.Unit, &r.ValueTime, &r.Quality, &r.QualityReason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// settle 在一个事务里完成"取每项最后值 -> 幂等插入快照 -> 删除该小时暂存"。
// 窗口按终端生效值分组，分组所需的配置只从 WindowConfig 读取一次。
func (s *Settler) settle(ctx context.Context, now time.Time) (processed, affected int64, err error) {
	ids, err := s.candidates(ctx, now)
	if err != nil {
		return 0, 0, err
	}
	if len(ids) == 0 {
		return 0, 0, nil
	}
	cfg, err := LoadWindowConfig(ctx, s.Pool)
	if err != nil {
		return 0, 0, fmt.Errorf("读取结算窗口配置: %w", err)
	}
	scopes, err := s.terminalScopes(ctx, ids)
	if err != nil {
		return 0, 0, err
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

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, w := range windows {
			closed, err := readClosed(ctx, tx, now, buckets[w], w)
			if err != nil {
				return fmt.Errorf("读取待结算暂存(window=%d): %w", w, err)
			}
			cells, err := UpsertSnapshot(ctx, tx, closed)
			if err != nil {
				return fmt.Errorf("写快照(window=%d): %w", w, err)
			}
			// 返回值语义 = 本轮**处理**的暂存行数（含被 value_time 守卫跳过的行）：
			// 快照里已有同 value_time 时 upsert 影响 0 行，但那批暂存确实被结算并清掉了。
			// 快照实际受影响行数另行累加，只用于日志观测。
			processed += int64(len(closed))
			affected += int64(len(cells))
			if _, err := tx.Exec(ctx, deleteClosedSQL, now, buckets[w], w); err != nil {
				return fmt.Errorf("清理暂存(window=%d): %w", w, err)
			}
		}
		return nil
	})
	return processed, affected, err
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
