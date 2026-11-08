package ingest

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/clean"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

type disabledLoader struct{}

func (disabledLoader) LoadTerminal(context.Context, string) (validate.Terminal, error) {
	return validate.Terminal{ID: "T-1", TypeID: 1, Enabled: false}, nil
}

func (disabledLoader) LoadProperties(context.Context, int64) ([]validate.Property, error) {
	return nil, nil
}

func (disabledLoader) LoadRules(context.Context, string) (map[string][]clean.Rule, error) {
	return nil, nil
}

// 停用终端：静默忽略（accepted=true、written=0、无异常），不碰库也不写异常表。
func TestDisabledTerminalSilentlyIgnored(t *testing.T) {
	proc := &Processor{
		Meta: meta.NewStore(disabledLoader{}, time.Minute, func() time.Time { return time.Unix(1000, 0) }),
		Now:  func() time.Time { return time.Unix(1000, 0) },
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	env := `{"ver":1,"messageId":"m","gatewayId":"GW-X","terminals":[
		{"terminalId":"T-1","items":[{"code":"temp","value":20,"unit":"C","collectedAt":"2026-11-08T18:00:00+08:00"}]}]}`
	res, err := proc.Handle(context.Background(), "GW-X", []byte(env))
	if err != nil {
		t.Fatalf("停用终端不应报错: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("结果数 = %d，期望 1", len(res.Results))
	}
	r := res.Results[0]
	if !r.Accepted || r.Written != 0 || r.Reason != "" {
		t.Fatalf("停用终端应静默忽略（accepted/written=0/reason 空），实际 %+v", r)
	}
}
