// Package gateway 是边缘网关注册表的进程内缓存：接入面按 gateway_id 判定能否收发。
//
// 语义：只有注册（gateway 表有行）且启用（enabled=true）的网关才允许接入；
// 管理面写入后 Flush，直接改库最多脏 30 秒（TTL 兜底）。
package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultTTL 是兜底过期时间：绕过管理 API 直接改库时，注册表最多脏 30 秒。
const DefaultTTL = 30 * time.Second

// Loader 是缓存背后的真实读取来源，独立成接口便于单测替身。
type Loader interface {
	// Load 返回该网关是否启用、是否已注册；未注册不是错误。
	Load(ctx context.Context, gatewayID string) (enabled bool, found bool, err error)
}

// Registry 是网关鉴权的进程内缓存。零值不可用，必须经 New 构造。
type Registry struct {
	loader Loader
	ttl    time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	enabled bool
	found   bool
	at      time.Time
}

// New 构造缓存；ttl<=0 取 DefaultTTL，now 为 nil 取 time.Now。
func New(loader Loader, ttl time.Duration, now func() time.Time) *Registry {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Registry{loader: loader, ttl: ttl, now: now, entries: make(map[string]entry)}
}

// Lookup 返回网关是否启用、是否已注册。命中缓存直接返回（含未注册的负缓存）。
func (r *Registry) Lookup(ctx context.Context, gatewayID string) (enabled bool, found bool, err error) {
	r.mu.Lock()
	if hit, ok := r.entries[gatewayID]; ok && r.now().Sub(hit.at) < r.ttl {
		r.mu.Unlock()
		return hit.enabled, hit.found, nil
	}
	r.mu.Unlock()

	enabled, found, err = r.loader.Load(ctx, gatewayID)
	if err != nil {
		// 数据库故障不写缓存：下一条消息重新尝试，避免把平台故障固化 30 秒。
		return false, false, err
	}
	r.mu.Lock()
	r.entries[gatewayID] = entry{enabled: enabled, found: found, at: r.now()}
	r.mu.Unlock()
	return enabled, found, nil
}

// Flush 清空缓存；管理面 create/update/delete 成功后调用。
func (r *Registry) Flush() {
	r.mu.Lock()
	r.entries = make(map[string]entry)
	r.mu.Unlock()
}

// PGLoader 是注册表的 PostgreSQL 读取实现。
type PGLoader struct {
	Pool *pgxpool.Pool
}

// Load 查 gateway 表；无行表示未注册（found=false），不是错误。
func (l PGLoader) Load(ctx context.Context, gatewayID string) (bool, bool, error) {
	var enabled bool
	err := l.Pool.QueryRow(ctx, `select enabled from gateway where gateway_id = $1`, gatewayID).Scan(&enabled)
	if err == pgx.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return enabled, true, nil
}
