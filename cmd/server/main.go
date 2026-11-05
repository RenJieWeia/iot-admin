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
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/settle"
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
		Meta:  meta.NewStore(meta.NewPGLoader(pool), meta.DefaultTTL, now),
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

	adminAPI := admin.New(pool)
	// 元数据写入成功后失效进程内缓存，下一条消息立即使用新定义。
	adminAPI.Invalidate = proc.Meta.Flush

	// 管理后台 UI：React SPA 产物走 embed，单二进制交付（R-015/R-016），不再渲染 HTML。
	mux := newMux(proc, admin.WithAuth(nil, adminAPI.Handler()), ui.Handler())

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

// withSecurityHeaders 给所有响应加安全头：管理面同源部署，CSP 不允许任何外源脚本/样式/连接。
// 前端产物没有内联脚本与内联样式，因此 default-src 'self' 即可生效。
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// newMux 汇总全部对外路由；抽出来便于用例断言路径与重定向。
func newMux(proc *ingest.Processor, adminHandler, uiHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /ingest/v1/messages", &ingest.HTTPHandler{P: proc})
	mux.Handle("/admin/v1/", adminHandler)
	// SPA 处理器只看 /ui 之后的路径，前缀在这里剥掉，处理器本身与挂载点解耦。
	mux.Handle("/ui/", http.StripPrefix("/ui", uiHandler))
	// 根路径直接进管理后台；接入与管理 API 仍走各自的明确路径。
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
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
