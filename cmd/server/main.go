// Command server 启动单实例 IoT 采集与传输服务。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/RenJieWeia/iot-admin/internal/admin"
	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/settle"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
	"github.com/RenJieWeia/iot-admin/migrations"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("服务退出", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn := env("IOT_PG_DSN", "")
	if dsn == "" {
		return errors.New("缺少 IOT_PG_DSN")
	}
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	now := time.Now
	rec := exception.New(pool, now, log)
	if err := settle.EnsurePartitions(ctx, pool, now()); err != nil {
		return err
	}

	client, err := connectMQTT(log)
	if err != nil {
		return err
	}
	defer client.Disconnect(250)

	proc := &ingest.Processor{
		DB:    pool,
		Reg:   validate.NewRegistry(pool),
		Pub:   publish.NewMQTT(client),
		Exc:   rec,
		Locks: store.NewShardedLock(64),
		Now:   now,
		Log:   log,
	}
	if err := ingest.Subscribe(client, proc, log); err != nil {
		return err
	}

	settler := settle.New(pool, rec, now, log)
	go settler.Loop(ctx, time.Minute)

	mux := http.NewServeMux()
	mux.Handle("POST /ingest/v1/messages", &ingest.HTTPHandler{P: proc})
	mux.Handle("/admin/v1/", admin.WithAuth(nil, admin.New(pool).Handler()))

	addr := env("IOT_LISTEN", "127.0.0.1:8080")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("HTTP 监听", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP 服务异常退出", "err", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// connectMQTT 连接 BroMQ（独立进程）。接入凭证由 Broker 管理，平台只透传。
// 注意：本连接是"平台 -> Broker"的客户端连接，它与"网关是否在线"无关。
// 平台侧连接抖动不得记为网关断连（会为所有网关产生假区间），故两个回调只打日志；
// 真正的网关断连需要 Broker 侧事件（client tracking / 遗嘱消息），随 Broker 集成排期。
func connectMQTT(log *slog.Logger) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(env("IOT_MQTT_URL", "tcp://127.0.0.1:1883")).
		SetClientID(env("IOT_MQTT_CLIENT_ID", "iot-admin")).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second)
	if u := env("IOT_MQTT_USERNAME", ""); u != "" {
		opts.SetUsername(u)
	}
	if p := env("IOT_MQTT_PASSWORD", ""); p != "" {
		opts.SetPassword(p)
	}
	opts.SetOnConnectHandler(func(mqtt.Client) { log.Info("MQTT 已连接") })
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) { log.Warn("MQTT 连接断开", "err", err) })

	client := mqtt.NewClient(opts)
	tok := client.Connect()
	tok.Wait()
	return client, tok.Error()
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
