package meta_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// fakeLoader 统计各载荷被穿透的次数，并可注入终端读取错误。
type fakeLoader struct {
	terminalCalls atomic.Int64
	propertyCalls atomic.Int64
	ruleCalls     atomic.Int64

	terminalErr error
}

func (f *fakeLoader) LoadTerminal(context.Context, string) (validate.Terminal, error) {
	f.terminalCalls.Add(1)
	if f.terminalErr != nil {
		return validate.Terminal{}, f.terminalErr
	}
	return validate.Terminal{ID: "T1", TypeID: 7, GatewayID: "G1", Enabled: true}, nil
}

func (f *fakeLoader) LoadProperties(context.Context, int64) ([]validate.Property, error) {
	f.propertyCalls.Add(1)
	return []validate.Property{{ItemCode: "p1", ValueType: "number", Required: true}}, nil
}

func (f *fakeLoader) LoadRules(context.Context, string) (map[string][]clean.Rule, error) {
	f.ruleCalls.Add(1)
	return map[string][]clean.Rule{"p1": {{ItemCode: "p1", Type: clean.RuleNotNull}}}, nil
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// 验收 §8-1：同一 key 连续 3 次读取，loader 只被调用 1 次。
func TestLoadTerminalCachesSuccess(t *testing.T) {
	ctx := context.Background()
	l := &fakeLoader{}
	s := meta.NewStore(l, 30*time.Second, newClock().now)

	var first validate.Terminal
	for i := 0; i < 3; i++ {
		got, err := s.LoadTerminal(ctx, "T1")
		if err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i+1, err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("第 %d 次读取结果与首次不一致: %+v != %+v", i+1, got, first)
		}
	}
	if n := l.terminalCalls.Load(); n != 1 {
		t.Fatalf("loader 调用次数 = %d，期望 1", n)
	}
}

// 验收 §8-2：Flush 后下一次读取重新穿透 loader。
func TestFlushForcesReload(t *testing.T) {
	ctx := context.Background()
	l := &fakeLoader{}
	s := meta.NewStore(l, 30*time.Second, newClock().now)

	if _, err := s.LoadTerminal(ctx, "T1"); err != nil {
		t.Fatalf("首次读取失败: %v", err)
	}
	s.Flush()
	if _, err := s.LoadTerminal(ctx, "T1"); err != nil {
		t.Fatalf("Flush 后读取失败: %v", err)
	}
	if n := l.terminalCalls.Load(); n != 2 {
		t.Fatalf("Flush 后 loader 调用次数 = %d，期望 2", n)
	}
}

// 验收 §8-3：loader 返回错误时不写缓存，连续 2 次错误 = 2 次调用。
func TestLoaderErrorNotCached(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("db down")
	l := &fakeLoader{terminalErr: boom}
	s := meta.NewStore(l, 30*time.Second, newClock().now)

	for i := 0; i < 2; i++ {
		if _, err := s.LoadTerminal(ctx, "T1"); !errors.Is(err, boom) {
			t.Fatalf("第 %d 次读取错误 = %v，期望 %v", i+1, err, boom)
		}
	}
	if n := l.terminalCalls.Load(); n != 2 {
		t.Fatalf("loader 调用次数 = %d，期望 2（错误不缓存）", n)
	}
}

// 验收 §8-4：ErrTerminalUnknown 不写缓存。
func TestTerminalUnknownNotCached(t *testing.T) {
	ctx := context.Background()
	l := &fakeLoader{terminalErr: validate.ErrTerminalUnknown}
	s := meta.NewStore(l, 30*time.Second, newClock().now)

	for i := 0; i < 2; i++ {
		if _, err := s.LoadTerminal(ctx, "T1"); !errors.Is(err, validate.ErrTerminalUnknown) {
			t.Fatalf("第 %d 次读取错误 = %v，期望 ErrTerminalUnknown", i+1, err)
		}
	}
	if n := l.terminalCalls.Load(); n != 2 {
		t.Fatalf("loader 调用次数 = %d，期望 2（未知终端不缓存）", n)
	}
}

// 验收 §8-5：假时钟前进超过 TTL 后自动穿透一次；未过期仍命中。ttl <= 0 取 DefaultTTL。
func TestTTLExpires(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		ttl  time.Duration
	}{
		{"显式 TTL 30s", 30 * time.Second},
		{"传入 0 走 DefaultTTL", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeLoader{}
			clk := newClock()
			s := meta.NewStore(l, tc.ttl, clk.now)

			if _, err := s.LoadTerminal(ctx, "T1"); err != nil {
				t.Fatalf("首次读取失败: %v", err)
			}
			clk.add(29 * time.Second) // 未过期，仍命中
			if _, err := s.LoadTerminal(ctx, "T1"); err != nil {
				t.Fatalf("TTL 内读取失败: %v", err)
			}
			if n := l.terminalCalls.Load(); n != 1 {
				t.Fatalf("TTL 内 loader 调用次数 = %d，期望 1", n)
			}
			clk.add(2 * time.Second) // 累计 31s > TTL，穿透一次
			if _, err := s.LoadTerminal(ctx, "T1"); err != nil {
				t.Fatalf("过期后读取失败: %v", err)
			}
			if n := l.terminalCalls.Load(); n != 2 {
				t.Fatalf("过期后 loader 调用次数 = %d，期望 2", n)
			}
			if _, err := s.LoadTerminal(ctx, "T1"); err != nil {
				t.Fatalf("过期重载后读取失败: %v", err)
			}
			if n := l.terminalCalls.Load(); n != 2 {
				t.Fatalf("重载后 loader 调用次数 = %d，期望 2（命中新值）", n)
			}
		})
	}
}

// 物模型 / 规则同样按 key 缓存，Flush 一并清空。
func TestOtherPayloadsCachedAndFlushed(t *testing.T) {
	ctx := context.Background()
	l := &fakeLoader{}
	s := meta.NewStore(l, 30*time.Second, newClock().now)

	for i := 0; i < 2; i++ {
		if _, err := s.LoadProperties(ctx, 7); err != nil {
			t.Fatalf("读取物模型失败: %v", err)
		}
		if _, err := s.LoadRules(ctx, "T1"); err != nil {
			t.Fatalf("读取质量规则失败: %v", err)
		}
	}
	if n := l.propertyCalls.Load(); n != 1 {
		t.Fatalf("物模型 loader 调用次数 = %d，期望 1", n)
	}
	if n := l.ruleCalls.Load(); n != 1 {
		t.Fatalf("规则 loader 调用次数 = %d，期望 1", n)
	}

	// 不同 typeID 是不同 key。
	if _, err := s.LoadProperties(ctx, 8); err != nil {
		t.Fatalf("读取另一类型物模型失败: %v", err)
	}
	if n := l.propertyCalls.Load(); n != 2 {
		t.Fatalf("换 typeID 后 loader 调用次数 = %d，期望 2", n)
	}

	s.Flush()
	if _, err := s.LoadProperties(ctx, 7); err != nil {
		t.Fatalf("Flush 后读取物模型失败: %v", err)
	}
	if _, err := s.LoadRules(ctx, "T1"); err != nil {
		t.Fatalf("Flush 后读取质量规则失败: %v", err)
	}
	if n := l.propertyCalls.Load(); n != 3 {
		t.Fatalf("Flush 后物模型 loader 调用次数 = %d，期望 3", n)
	}
	if n := l.ruleCalls.Load(); n != 2 {
		t.Fatalf("Flush 后规则 loader 调用次数 = %d，期望 2", n)
	}
}

// 验收 §8-6：16 goroutine 并发读同 key，无竞争，loader 调用次数不超过并发数。
func TestConcurrentReadsSameKey(t *testing.T) {
	ctx := context.Background()
	l := &fakeLoader{}
	s := meta.NewStore(l, 30*time.Second, newClock().now)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			got, err := s.LoadTerminal(ctx, "T1")
			if err != nil {
				t.Errorf("并发读取失败: %v", err)
				return
			}
			if got.ID != "T1" || got.TypeID != 7 {
				t.Errorf("并发读取结果异常: %+v", got)
			}
		}()
	}
	wg.Wait()

	if calls := l.terminalCalls.Load(); calls < 1 || calls > n {
		t.Fatalf("并发 loader 调用次数 = %d，期望 1..%d", calls, n)
	}
}
