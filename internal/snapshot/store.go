package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Loader 是全量配置的读取来源（独立成接口便于单测替身）。
type Loader interface {
	Load(ctx context.Context) (Config, Bindings, error)
}

// PGLoader 从 snapshot_slot_config（snapshot 一行）与 snapshot_binding 全量读取。
// 两张表都很小，整体缓存是划算的。
type PGLoader struct {
	Pool *pgxpool.Pool
}

// NewPGLoader 构造基于 PG 的加载器。
func NewPGLoader(pool *pgxpool.Pool) *PGLoader { return &PGLoader{Pool: pool} }

// Load 读一次全量配置：快照总开关 + 两级绑定。
func (l *PGLoader) Load(ctx context.Context) (Config, Bindings, error) {
	cfg := Config{}
	rows, err := l.Pool.Query(ctx, `select granularity, enabled, coalesce(step, 0) from snapshot_slot_config`)
	if err != nil {
		return Config{}, Bindings{}, err
	}
	for rows.Next() {
		var g string
		var slot Slot
		if err := rows.Scan(&g, &slot.Enabled, &slot.Step); err != nil {
			rows.Close()
			return Config{}, Bindings{}, err
		}
		if g == SlotSnapshotName {
			cfg.Snapshot = slot
		}
		// 其它档位（回滚残留）忽略，避免把整条写入链路打挂。
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Config{}, Bindings{}, err
	}

	// 一条绑定 = 一个目标 + 一个 item_codes 数组：加载时把数组展开进 set，
	// 缓存对外形状（Types[typeID]→set、Terminals[terminalID]→set）与并集判定都不变。
	binds := Bindings{
		Types:     map[int64]map[string]struct{}{},
		Terminals: map[string]map[string]struct{}{},
	}
	rows, err = l.Pool.Query(ctx, `select device_type_id, terminal_id, item_codes from snapshot_binding`)
	if err != nil {
		return Config{}, Bindings{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var typeID *int64
		var terminalID *string
		var raw []byte
		if err := rows.Scan(&typeID, &terminalID, &raw); err != nil {
			return Config{}, Bindings{}, err
		}
		var codes []string
		if err := json.Unmarshal(raw, &codes); err != nil {
			// 数据库 CHECK 保证是数组；解不出来说明数据被外部改坏了 → 按平台故障上报。
			return Config{}, Bindings{}, fmt.Errorf("解析 snapshot_binding.item_codes: %w", err)
		}
		for _, code := range codes {
			if code == "" {
				continue
			}
			switch {
			case typeID != nil:
				set := binds.Types[*typeID]
				if set == nil {
					set = map[string]struct{}{}
					binds.Types[*typeID] = set
				}
				set[code] = struct{}{}
			case terminalID != nil:
				set := binds.Terminals[*terminalID]
				if set == nil {
					set = map[string]struct{}{}
					binds.Terminals[*terminalID] = set
				}
				set[code] = struct{}{}
			}
		}
	}
	return cfg, binds, rows.Err()
}

// DefaultTTL 是配置缓存的兜底过期时间：绕过管理 API 直接改库时，配置最多脏 30 秒。
const DefaultTTL = 30 * time.Second

// Store 是「全局六档 + 两级绑定」的进程内缓存（TTL + 显式失效）。
// 读失败直接返回错误，由调用方按平台故障处理（internal_error → 503），不拿旧值硬撑。
type Store struct {
	loader   Loader
	ttl      time.Duration
	now      func() time.Time
	mu       sync.RWMutex
	cfg      Config
	binds    Bindings
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
	return &Store{
		loader: l, ttl: ttl, now: now,
		cfg:   Config{},
		binds: Bindings{Types: map[int64]map[string]struct{}{}, Terminals: map[string]map[string]struct{}{}},
	}
}

// Config 返回快照配置（结构体副本，调用方可以安全持有）。
func (s *Store) Config(ctx context.Context) (Config, error) {
	if err := s.ensure(ctx); err != nil {
		return Config{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, nil
}

// Lookup 判定某序列是否被纳入快照：类型级 OR 实例级（并集）。
// 返回 false 时即使六档全开也零写入（未绑定 = 不纳入）。
func (s *Store) Lookup(ctx context.Context, terminalID string, typeID int64, itemCode string) (bool, error) {
	if err := s.ensure(ctx); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.binds.Binds(terminalID, typeID, itemCode), nil
}

// Invalidate 让下一次读取重新加载（管理面写完 slot config / binding 后调用，验收 1 靠它）。
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.valid = false
	s.mu.Unlock()
}

// ensure 在 TTL 内直接返回；过期则重载。
func (s *Store) ensure(ctx context.Context) error {
	s.mu.RLock()
	fresh := s.valid && s.now().Before(s.loadedAt.Add(s.ttl))
	s.mu.RUnlock()
	if fresh {
		return nil
	}
	return s.reload(ctx)
}

// reload 全量重载。双检只挡住「已经加载完」的后来者：TTL 到期的瞬间若有多个调用并发，
// 它们可能同时看到 valid=false 而各自加载一次（没有 singleflight 合并"飞行中"的加载）。
// 两张表都很小、TTL 30s，重复加载的代价可接受；读库在锁外进行，不阻塞其它读。
func (s *Store) reload(ctx context.Context) error {
	s.mu.Lock()
	if s.valid && s.now().Before(s.loadedAt.Add(s.ttl)) {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	cfg, binds, err := s.loader.Load(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.cfg = cfg
	s.binds = binds
	s.loadedAt = s.now()
	s.valid = true
	s.mu.Unlock()
	return nil
}
