// Package meta 提供接入管道所需元数据（终端、物模型、质量规则）的进程内缓存。
//
// 缓存只保存成功结果：终端不存在 / 已停用 / 数据库错误一律不写入，下次读取继续穿透 loader。
// 失效为全量 Flush（元数据写入是低频人工操作），另以 TTL 兜底绕过管理 API 的直接改库。
package meta

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// DefaultTTL 是兜底过期时间：不走管理 API 直接改库时，元数据最多脏 30 秒。
const DefaultTTL = 30 * time.Second

// Loader 是缓存背后的真实读取来源，独立成接口便于单测替身。
type Loader interface {
	LoadTerminal(ctx context.Context, terminalID string) (validate.Terminal, error)
	LoadProperties(ctx context.Context, typeID int64) ([]validate.Property, error)
	LoadRules(ctx context.Context, terminalID string) (map[string][]clean.Rule, error)
}

// Store 是三类元数据的进程内缓存。零值不可用，必须经 NewStore 构造。
type Store struct {
	loader     Loader
	terminals  cache[validate.Terminal]
	properties cache[[]validate.Property]
	rules      cache[map[string][]clean.Rule]
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
		loader:     l,
		terminals:  newCache[validate.Terminal](ttl, now),
		properties: newCache[[]validate.Property](ttl, now),
		rules:      newCache[map[string][]clean.Rule](ttl, now),
	}
}

// LoadTerminal 读终端上下文；错误结果不缓存。并发 miss 允许多查一次（不做 singleflight）。
func (s *Store) LoadTerminal(ctx context.Context, id string) (validate.Terminal, error) {
	return s.terminals.get(id, func() (validate.Terminal, error) {
		return s.loader.LoadTerminal(ctx, id)
	})
}

// LoadProperties 按终端类型读物模型属性。
func (s *Store) LoadProperties(ctx context.Context, typeID int64) ([]validate.Property, error) {
	return s.properties.get(strconv.FormatInt(typeID, 10), func() ([]validate.Property, error) {
		return s.loader.LoadProperties(ctx, typeID)
	})
}

// LoadRules 读终端绑定的质量规则。
func (s *Store) LoadRules(ctx context.Context, id string) (map[string][]clean.Rule, error) {
	return s.rules.get(id, func() (map[string][]clean.Rule, error) {
		return s.loader.LoadRules(ctx, id)
	})
}

// Flush 清空全部缓存，供管理 API 写成功后调用。
func (s *Store) Flush() {
	s.terminals.flush()
	s.properties.flush()
	s.rules.flush()
}

type cacheItem[T any] struct {
	val T
	exp time.Time
}

type cache[T any] struct {
	mu    sync.RWMutex
	items map[string]cacheItem[T]
	ttl   time.Duration
	now   func() time.Time
}

func newCache[T any](ttl time.Duration, now func() time.Time) cache[T] {
	return cache[T]{items: map[string]cacheItem[T]{}, ttl: ttl, now: now}
}

// get 命中即返回；未命中则穿透 fetch，只有成功结果写回缓存。
func (c *cache[T]) get(key string, fetch func() (T, error)) (T, error) {
	if v, ok := c.hit(key); ok {
		return v, nil
	}
	// 锁外穿透：并发 miss 各自查一次，可接受（R-006）。
	v, err := fetch()
	if err != nil {
		var zero T
		return zero, err
	}
	c.mu.Lock()
	c.items[key] = cacheItem[T]{val: v, exp: c.now().Add(c.ttl)}
	c.mu.Unlock()
	return v, nil
}

func (c *cache[T]) hit(key string) (T, bool) {
	c.mu.RLock()
	it, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || !c.now().Before(it.exp) {
		var zero T
		return zero, false
	}
	return it.val, true
}

func (c *cache[T]) flush() {
	c.mu.Lock()
	c.items = map[string]cacheItem[T]{}
	c.mu.Unlock()
}

// PGLoader 是基于 PG 的默认 Loader：终端与物模型走 validate.Registry，规则走 clean.LoadRules。
type PGLoader struct {
	Pool *pgxpool.Pool
	Reg  *validate.Registry
}

func NewPGLoader(pool *pgxpool.Pool) *PGLoader {
	return &PGLoader{Pool: pool, Reg: validate.NewRegistry(pool)}
}

func (l *PGLoader) LoadTerminal(ctx context.Context, terminalID string) (validate.Terminal, error) {
	return l.Reg.LoadTerminal(ctx, terminalID)
}

func (l *PGLoader) LoadProperties(ctx context.Context, typeID int64) ([]validate.Property, error) {
	return l.Reg.LoadProperties(ctx, typeID)
}

func (l *PGLoader) LoadRules(ctx context.Context, terminalID string) (map[string][]clean.Rule, error) {
	return clean.LoadRules(ctx, l.Pool, terminalID)
}
