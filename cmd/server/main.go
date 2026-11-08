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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/admin"
	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/snapshot"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/ui"
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

	client, err := connectMQTT(log)
	if err != nil {
		return err
	}
	defer client.Disconnect(250)

	snapStore := snapshot.NewStore(snapshot.NewPGLoader(pool), snapshot.DefaultTTL, now)
	proc := &ingest.Processor{
		DB:    pool,
		Meta:  meta.NewStore(meta.NewPGLoader(pool), meta.DefaultTTL, now),
		Pub:   publish.NewMQTT(client),
		Exc:   rec,
		Locks: store.NewShardedLock(64),
		Snap:  snapStore,
		Now:   now,
		Log:   log,
	}
	if err := ingest.Subscribe(client, proc, log); err != nil {
		return err
	}

	adminAPI := admin.New(pool)
	// 管理面写入成功后失效进程内缓存：元数据与快照计划都清，下一条消息立即使用新定义。
	adminAPI.Invalidate = func() {
		proc.Meta.Flush()
		snapStore.Invalidate()
	}

	// 快照分区维护：开机先跑一次，之后每日一次；失败只告警，不阻塞上报。
	go maintainSnapshots(ctx, pool, now, log)

	mux := newMux(proc, admin.WithAuth(nil, adminAPI.Handler()))

	addr := env("IOT_LISTEN", "127.0.0.1:8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           withSecurityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
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

// maintainSnapshots 续建快照分区并按保留期清理过期分区。
// 开机立刻跑一次，之后每 24 小时一次；任何失败只记告警（缺分区导致的写失败属平台故障，网关可重传）。
func maintainSnapshots(ctx context.Context, pool *pgxpool.Pool, now func() time.Time, log *slog.Logger) {
	run := func() {
		if err := snapshot.EnsurePartitions(ctx, pool, now()); err != nil {
			log.Warn("快照分区续建失败", "err", err)
		}
		if err := snapshot.PurgeExpired(ctx, pool, now()); err != nil {
			log.Warn("快照保留期清理失败", "err", err)
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// withSecurityHeaders 给所有响应加安全头：管理面同源部署，CSP 不允许任何外源脚本/样式/连接。
// style-src 放开 'unsafe-inline' 是因为 Radix 组件会注入内联 style；脚本仍只允许 'self'。
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// newMux 汇总全部对外路由；抽出来便于用例断言路径。
// 单二进制交付：前端产物由 embed 从 /ui/ 提供，根路径统一跳过去。
func newMux(proc *ingest.Processor, adminHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /ingest/v1/messages", &ingest.HTTPHandler{P: proc})
	mux.Handle("/admin/v1/", adminHandler)
	mux.Handle("GET /{$}", http.RedirectHandler("/ui/", http.StatusFound))
	mux.Handle("/ui/", http.StripPrefix("/ui/", ui.Handler()))
	return mux
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
