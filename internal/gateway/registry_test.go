package gateway

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeLoader struct {
	states map[string]bool
	calls  int
	err    error
}

func (f *fakeLoader) Load(_ context.Context, id string) (bool, bool, error) {
	f.calls++
	if f.err != nil {
		return false, false, f.err
	}
	enabled, ok := f.states[id]
	return enabled, ok, nil
}

func TestLookupCachesPositiveAndNegative(t *testing.T) {
	loader := &fakeLoader{states: map[string]bool{"GW-A": true}}
	now := time.Unix(1000, 0)
	reg := New(loader, time.Minute, func() time.Time { return now })
	ctx := context.Background()

	enabled, found, err := reg.Lookup(ctx, "GW-A")
	if err != nil || !found || !enabled {
		t.Fatalf("GW-A = (%v,%v,%v)，期望 (true,true,nil)", enabled, found, err)
	}
	if _, _, err := reg.Lookup(ctx, "GW-A"); err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 {
		t.Fatalf("命中缓存后 loader 调用 %d 次，期望 1", loader.calls)
	}

	// 未注册也要负缓存：恶意/误配网关反复打不会每次都查库。
	if _, found, _ := reg.Lookup(ctx, "GW-X"); found {
		t.Fatal("GW-X 不应已注册")
	}
	_, _, _ = reg.Lookup(ctx, "GW-X")
	if loader.calls != 2 {
		t.Fatalf("负缓存后 loader 调用 %d 次，期望 2", loader.calls)
	}

	// TTL 过期后穿透重查。
	now = now.Add(2 * time.Minute)
	_, _, _ = reg.Lookup(ctx, "GW-A")
	if loader.calls != 3 {
		t.Fatalf("TTL 过期后 loader 调用 %d 次，期望 3", loader.calls)
	}

	// Flush 后立刻穿透。
	loader.states["GW-A"] = false
	reg.Flush()
	enabled, found, _ = reg.Lookup(ctx, "GW-A")
	if !found || enabled {
		t.Fatalf("Flush 后 GW-A = (%v,%v)，期望停用", enabled, found)
	}
}

func TestLookupDoesNotCacheErrors(t *testing.T) {
	loader := &fakeLoader{err: errors.New("db down")}
	reg := New(loader, time.Minute, nil)
	if _, _, err := reg.Lookup(context.Background(), "GW-A"); err == nil {
		t.Fatal("数据库错误应原样返回")
	}
	loader.err = nil
	loader.states = map[string]bool{"GW-A": true}
	if enabled, found, err := reg.Lookup(context.Background(), "GW-A"); err != nil || !found || !enabled {
		t.Fatalf("错误不缓存，下一次应查库成功: (%v,%v,%v)", enabled, found, err)
	}
}
