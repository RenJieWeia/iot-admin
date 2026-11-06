package ingest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/admin"
	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// countingLoader 统计缓存背后真正穿透到 PG 的次数（四类载荷分别计数）。
type countingLoader struct {
	inner    *meta.PGLoader
	terminal atomic.Int64
	props    atomic.Int64
	rules    atomic.Int64
	window   atomic.Int64
}

func (c *countingLoader) LoadTerminal(ctx context.Context, id string) (validate.Terminal, error) {
	c.terminal.Add(1)
	return c.inner.LoadTerminal(ctx, id)
}

func (c *countingLoader) LoadProperties(ctx context.Context, typeID int64) ([]validate.Property, error) {
	c.props.Add(1)
	return c.inner.LoadProperties(ctx, typeID)
}

func (c *countingLoader) LoadRules(ctx context.Context, id string) (map[string][]clean.Rule, error) {
	c.rules.Add(1)
	return c.inner.LoadRules(ctx, id)
}

func (c *countingLoader) LoadWindowConfig(ctx context.Context) (settle.WindowConfig, error) {
	c.window.Add(1)
	return c.inner.LoadWindowConfig(ctx)
}

func (c *countingLoader) total() int64 {
	return c.terminal.Load() + c.props.Load() + c.rules.Load() + c.window.Load()
}

// adminPost 走管理 API 的 POST，成功返回。
func adminPost(t *testing.T, handler http.Handler, path, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s 返回 %d: %s", path, rec.Code, rec.Body.String())
	}
}

// adminPut 走管理 API 的 PUT，成功返回。
func adminPut(t *testing.T, handler http.Handler, path, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s 返回 %d: %s", path, rec.Code, rec.Body.String())
	}
}

// 验收 §8：同一终端两条消息，第二条不新增元数据查询（首条 4 次、第二条 0 次）。
func TestCacheSkipsMetadataQueriesOnSecondMessage(t *testing.T) {
	e := newEnv(t)
	cl := &countingLoader{inner: meta.NewPGLoader(e.pool)}
	e.proc.Meta = meta.NewStore(cl, meta.DefaultTTL, e.clock.Now)

	at := baseTime.Add(5 * time.Minute)
	msg := single(testTerminalID, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at))

	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("首条消息应被接收: %+v", r)
	}
	first := cl.total()
	if first != 4 {
		t.Fatalf("首条消息元数据查询 = %d 次（终端/物模型/规则/窗口各 1），期望 4: %+v", first, cl)
	}

	e.clock.Add(10 * time.Second) // 仍在 30s TTL 内
	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("第二条消息应被接收: %+v", r)
	}
	if got := cl.total(); got != first {
		t.Fatalf("第二条消息新增元数据查询 %d 次，期望 0（终端=%d 物模型=%d 规则=%d 窗口=%d）",
			got-first, cl.terminal.Load(), cl.props.Load(), cl.rules.Load(), cl.window.Load())
	}
}

// 验收 §8：新增物模型声明后，下一条消息立即按新定义判 msg_incomplete。
// R-021 后 required 列不再参与校验，所以用"新增一个声明"来验证管理写路径的即时失效。
func TestAdminPropertyUpdateTakesEffectImmediately(t *testing.T) {
	e := newEnv(t)
	handler := admin.New(e.pool)
	handler.Invalidate = e.proc.Meta.Flush

	at := baseTime.Add(5 * time.Minute)
	msg := single(testTerminalID, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at))
	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("新增 hum 声明前应接收: %+v", r)
	}

	adminPost(t, handler.Handler(), "/admin/v1/thing-model-properties",
		`{"type_id":`+strconv.FormatInt(e.typeID, 10)+`,"item_code":"hum","name":"湿度","value_type":"number"}`)
	e.clock.Add(5 * time.Second) // 远小于 TTL，只能靠 Flush 生效

	r := resultOf(t, e.handle(msg), testTerminalID)
	if r.Accepted || r.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("新增声明后应判 msg_incomplete，实际 %+v", r)
	}
	if len(r.MissingCodes) != 1 || r.MissingCodes[0] != "hum" {
		t.Fatalf("缺失项 = %v，期望 [hum]", r.MissingCodes)
	}
}

// 验收 §8：停用终端后 TTL 内仍判 terminal_unknown（负结果不缓存），重新启用后立即恢复。
func TestAdminTerminalToggleTakesEffectImmediately(t *testing.T) {
	e := newEnv(t)
	handler := admin.New(e.pool)
	handler.Invalidate = e.proc.Meta.Flush

	at := baseTime.Add(5 * time.Minute)
	msg := single(testTerminalID, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at))
	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("停用前应接收: %+v", r)
	}

	adminPut(t, handler.Handler(), "/admin/v1/terminals/"+testTerminalID, `{"enabled":false}`)
	e.clock.Add(5 * time.Second)
	for i := 0; i < 2; i++ {
		r := resultOf(t, e.handle(msg), testTerminalID)
		if r.Accepted || r.Reason != validate.ReasonTerminalUnknown {
			t.Fatalf("第 %d 次停用后应判 terminal_unknown，实际 %+v", i+1, r)
		}
	}

	adminPut(t, handler.Handler(), "/admin/v1/terminals/"+testTerminalID, `{"enabled":true}`)
	e.clock.Add(5 * time.Second)
	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("重新启用后应恢复接收: %+v", r)
	}
}

// R-004 兜底：绕过管理 API 直接改库时，缓存最长脏 TTL，过期后自动穿透。
func TestTTLFallsBackForDirectDBEdit(t *testing.T) {
	e := newEnv(t)
	handler := admin.New(e.pool)
	handler.Invalidate = e.proc.Meta.Flush

	at := baseTime.Add(5 * time.Minute)
	msg := single(testTerminalID, it("temp", 20.0, "C", at), it("power", 1.0, "kW", at))
	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("改库前应接收: %+v", r)
	}

	// 直接改库（新增 hum 声明），不走管理 API，因此不会 Flush。
	if _, err := e.pool.Exec(context.Background(),
		`insert into thing_model_property (type_id, item_code, name, value_type)
		 values ($1, 'hum', '湿度', 'number')`, e.typeID); err != nil {
		t.Fatalf("直接改物模型: %v", err)
	}

	e.clock.Add(10 * time.Second)
	if r := resultOf(t, e.handle(msg), testTerminalID); !r.Accepted {
		t.Fatalf("TTL 内应仍用缓存旧定义（接收），实际 %+v", r)
	}

	e.clock.Add(meta.DefaultTTL) // 累计超过 TTL
	if r := resultOf(t, e.handle(msg), testTerminalID); r.Accepted || r.Reason != validate.ReasonMsgIncomplete {
		t.Fatalf("TTL 过期后应按新定义判 msg_incomplete，实际 %+v", r)
	}
}
