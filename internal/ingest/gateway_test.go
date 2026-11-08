package ingest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/gateway"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

type stubLoader struct {
	enabled bool
	found   bool
	err     error
}

func (s stubLoader) Load(context.Context, string) (bool, bool, error) {
	return s.enabled, s.found, s.err
}

func newGatewayProcessor(loader gateway.Loader) *Processor {
	return &Processor{
		Gateways: gateway.New(loader, time.Minute, nil),
		Now:      time.Now,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

const oneTerminalEnvelope = `{"ver":1,"messageId":"m-1","gatewayId":"GW-X",
 "terminals":[{"terminalId":"T-1","items":[]}]}`

// 未注册的网关整包拒收：reason=gateway_unknown，且不会进入终端处理。
func TestGatewayMustBeRegistered(t *testing.T) {
	proc := newGatewayProcessor(stubLoader{found: false})
	_, err := proc.Handle(context.Background(), "GW-X", []byte(oneTerminalEnvelope))
	var envelopeErr *validate.EnvelopeError
	if !errors.As(err, &envelopeErr) || envelopeErr.Reason != validate.ReasonGatewayUnknown {
		t.Fatalf("未注册网关应返回 gateway_unknown，实际 %v", err)
	}
}

// 注册了但停用：reason=gateway_disabled。
func TestDisabledGatewayRejected(t *testing.T) {
	proc := newGatewayProcessor(stubLoader{found: true, enabled: false})
	_, err := proc.Handle(context.Background(), "GW-X", []byte(oneTerminalEnvelope))
	var envelopeErr *validate.EnvelopeError
	if !errors.As(err, &envelopeErr) || envelopeErr.Reason != validate.ReasonGatewayDisabled {
		t.Fatalf("停用网关应返回 gateway_disabled，实际 %v", err)
	}
}

// 注册表读失败是平台故障，必须原样返回（上层转 500/503），不能当成鉴权失败。
func TestGatewayRegistryFailureIsPlatformError(t *testing.T) {
	proc := newGatewayProcessor(stubLoader{err: errors.New("db down")})
	_, err := proc.Handle(context.Background(), "GW-X", []byte(oneTerminalEnvelope))
	var envelopeErr *validate.EnvelopeError
	if err == nil || errors.As(err, &envelopeErr) {
		t.Fatalf("DB 故障不应变成整包校验错误，实际 %v", err)
	}
}

// HTTP 接入：鉴权失败返回 403（而非数据问题的 400），网关侧可据此停止重传。
func TestHTTPGatewayRejectedIsForbidden(t *testing.T) {
	handler := &HTTPHandler{P: newGatewayProcessor(stubLoader{found: false})}
	req := httptest.NewRequest(http.MethodPost, "/ingest/v1/messages", strings.NewReader(oneTerminalEnvelope))
	req.Header.Set(GatewayHeader, "GW-X")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("未注册网关 HTTP 状态 = %d，期望 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gateway_unknown") {
		t.Fatalf("响应体应包含 gateway_unknown: %s", rec.Body.String())
	}
}

// 传输层没带身份（HTTP 缺头）→ 400 gateway_id_missing。
func TestMissingIngressIdentity(t *testing.T) {
	proc := newGatewayProcessor(stubLoader{found: true, enabled: true})
	_, err := proc.Handle(context.Background(), "", []byte(oneTerminalEnvelope))
	var envelopeErr *validate.EnvelopeError
	if !errors.As(err, &envelopeErr) || envelopeErr.Reason != validate.ReasonGatewayMissing {
		t.Fatalf("缺接入身份应返回 gateway_id_missing，实际 %v", err)
	}
}
