package snapshot

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Loader 是计划表的读取来源（独立成接口便于单测替身）。
type Loader interface {
	LoadPlans(ctx context.Context) ([]Plan, error)
}

// PGLoader 从 snapshot_plan 全量读计划（行数 = 序列数，量级很小，整体缓存）。
type PGLoader struct {
	Pool *pgxpool.Pool
}

// NewPGLoader 构造基于 PG 的计划加载器。
func NewPGLoader(pool *pgxpool.Pool) *PGLoader { return &PGLoader{Pool: pool} }

// LoadPlans 读全部序列的六档配置。
func (l *PGLoader) LoadPlans(ctx context.Context) ([]Plan, error) {
	rows, err := l.Pool.Query(ctx, `
		select device_type_id, item_code,
		       second_enabled, coalesce(second_step, 0),
		       minute_enabled, coalesce(minute_step, 0),
		       hour_enabled,   coalesce(hour_step, 0),
		       day_enabled,    coalesce(day_step, 0),
		       month_enabled,  coalesce(month_step, 0),
		       year_enabled,   coalesce(year_step, 0)
		from snapshot_plan`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Plan
	for rows.Next() {
		var p Plan
		var secOn, minOn, hourOn, dayOn, monOn, yearOn bool
		var secStep, minStep, hourStep, dayStep, monStep, yearStep int
		if err := rows.Scan(&p.TypeID, &p.ItemCode,
			&secOn, &secStep, &minOn, &minStep, &hourOn, &hourStep,
			&dayOn, &dayStep, &monOn, &monStep, &yearOn, &yearStep); err != nil {
			return nil, err
		}
		p.Slots = map[Granularity]Slot{
			Second: {Enabled: secOn, Step: secStep},
			Minute: {Enabled: minOn, Step: minStep},
			Hour:   {Enabled: hourOn, Step: hourStep},
			Day:    {Enabled: dayOn, Step: dayStep},
			Month:  {Enabled: monOn, Step: monStep},
			Year:   {Enabled: yearOn, Step: yearStep},
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DefaultTTL 是计划缓存的兜底过期时间：绕过管理 API 直接改库时，配置最多脏 30 秒。
const DefaultTTL = 30 * time.Second

type planKey struct {
	typeID   int64
	itemCode string
}

// Store 是计划表的进程内全量缓存（TTL + 显式失效），与 internal/meta 的缓存策略一致：
// 只缓存成功结果；读失败直接返回错误，由调用方按平台故障处理（internal_error → 503）。
type Store struct {
	loader   Loader
	ttl      time.Duration
	now      func() time.Time
	mu       sync.RWMutex
	plans    map[planKey]Plan
	loadedAt time.Time
	valid    bool
}

// NewStore 构造缓存。ttl <= 0 时取 DefaultTTL，now 为 nil 时取 time.Now。
func NewStore(l Loader, ttl time.Duration, now func() time.Time) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Store{loader: l, ttl: ttl, now: now, plans: map[planKey]Plan{}}
}

// Lookup 取某序列的计划；第二个返回值表示「是否配置了快照」（未配置 = 零写入）。
func (s *Store) Lookup(ctx context.Context, typeID int64, itemCode string) (Plan, bool, error) {
	key := planKey{typeID: typeID, itemCode: itemCode}
	s.mu.RLock()
	fresh := s.valid && s.now().Before(s.loadedAt.Add(s.ttl))
	plan, ok := s.plans[key]
	s.mu.RUnlock()
	if fresh {
		return plan, ok, nil
	}
	if err := s.reload(ctx); err != nil {
		return Plan{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok = s.plans[key]
	return plan, ok, nil
}

// Invalidate 让下一次读取重新加载（管理 API 写完计划后调用）。
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.valid = false
	s.mu.Unlock()
}

// reload 全量重载。双检只挡住「已经加载完」的后来者：TTL 到期的瞬间若有多个 Lookup 并发，
// 它们可能同时看到 valid=false 而各自加载一次（没有 singleflight 合并"飞行中"的加载）。
// 计划表很小、TTL 30s，重复加载的代价可接受；读库在锁外进行，不阻塞其它 Lookup。
func (s *Store) reload(ctx context.Context) error {
	s.mu.Lock()
	if s.valid && s.now().Before(s.loadedAt.Add(s.ttl)) {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	plans, err := s.loader.LoadPlans(ctx)
	if err != nil {
		return err
	}
	next := make(map[planKey]Plan, len(plans))
	for _, p := range plans {
		next[planKey{typeID: p.TypeID, itemCode: p.ItemCode}] = p
	}

	s.mu.Lock()
	s.plans = next
	s.loadedAt = s.now()
	s.valid = true
	s.mu.Unlock()
	return nil
}
