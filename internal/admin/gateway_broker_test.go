package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/broker"
)

type fakeBroker struct {
	registerErr   error
	unregisterErr error
	registered    []broker.Gateway
	unregistered  []string
}

func (f *fakeBroker) Register(_ context.Context, gw broker.Gateway) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.registered = append(f.registered, gw)
	return nil
}

func (f *fakeBroker) Unregister(_ context.Context, id string) error {
	if f.unregisterErr != nil {
		return f.unregisterErr
	}
	f.unregistered = append(f.unregistered, id)
	return nil
}

// 网关新增先在 Broker 开通；Broker 失败时不写本地行，返回 502。
func TestCreateGatewayRegistersBrokerFirst(t *testing.T) {
	api := New(nil)
	api.Broker = &fakeBroker{registerErr: errors.New("broker 500")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/gateways",
		strings.NewReader(`{"gateway_id":"GW-A","name":"一号网关"}`))
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "broker_register_failed") {
		t.Fatalf("Broker 注册失败应 502，实际 %d %s", rec.Code, rec.Body.String())
	}
}

// 网关删除先回收 Broker 账号；失败时本地行保留，返回 502。
func TestDeleteGatewayUnregistersBrokerFirst(t *testing.T) {
	api := New(nil)
	api.Broker = &fakeBroker{unregisterErr: errors.New("broker 500")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/gateways/GW-A", nil)
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "broker_unregister_failed") {
		t.Fatalf("Broker 注销失败应 502，实际 %d %s", rec.Code, rec.Body.String())
	}
}
