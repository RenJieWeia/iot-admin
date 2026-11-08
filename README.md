# iot-admin

IoT 采集与传输服务的后端。对外提供接入面（`POST /ingest/v1/messages`）、管理面（`/admin/v1/*`，JSON API）与内嵌的管理后台前端（`/ui/`），
外加 MQTT 发布、数据清洗/校验/快照与异常记录等业务逻辑。前端产物随 `go build` 打进同一个二进制，不再单独部署静态站点。

## 目录

| 路径 | 内容 |
| --- | --- |
| `cmd/server` | 进程入口：路由、MQTT 连接、快照分区维护 |
| `internal/ingest` | 接入处理：HTTP/MQTT 入口、流水线、批量与容量 |
| `internal/validate` `internal/clean` | 报文校验与清洗（单位、质量位） |
| `internal/staging` `internal/latest` `internal/publish` | 落库前暂存、最新值、MQTT 发布 |
| `internal/snapshot` | 快照存储、分区续建与保留期清理 |
| `internal/exception` | 异常与断连区间记录 |
| `internal/admin` | 管理面 API：元数据、查询、枚举、只读页、游标分页 |
| `internal/ui` | 前端产物 embed 与 SPA handler（`/ui/`、深链 fallback、资源长缓存） |
| `internal/meta` `internal/store` | 元数据缓存、连接池与分片锁 |
| `web` | React + TypeScript + Radix + Tailwind 管理后台前端（构建产物写进 `internal/ui/dist`） |
| `migrations` | SQL 迁移，进程启动时按序执行 |
| `configs/server.env.example` | 本地开发环境变量样例 |

## 开发环境

- Go 1.27+（见 `go.mod`）
- Node.js 22+（只在前端开发/构建时需要）
- PostgreSQL（迁移由服务启动时自动执行）
- MQTT Broker（可选，Broker 侧管理网关凭证）

```bash
cp configs/server.env.example .env      # 按需修改 DSN 与 Broker 地址
export $(grep -v '^#' .env | xargs)     # 或用 direnv / systemd 注入
go run ./cmd/server
```

### 前端（web/）

```bash
cd web
npm install        # 只装已声明的依赖，不新增包
npm run dev        # http://127.0.0.1:5173，/admin 由 vite 代理到 127.0.0.1:8080
npm run build      # 类型检查 + 构建，产物直接写进 internal/ui/dist
```

- 计划中的页面与组件契约见 `web/CONTRACT.md`。
- 构建产物在 `internal/ui/dist`，由 `//go:embed all:dist` 编进二进制；服务启动后访问 `http://127.0.0.1:8080/ui/`，`/` 会 302 到 `/ui/`。
- 没构建前端时 `internal/ui/dist` 里保留占位 `index.html`，保证 `go build ./...` 可用；发布前用 `make web-build` 覆盖成真实产物。
- 页面使用前端路由（`/ui/e/:name`、`/ui/r/:slot`、`/ui/x/:slot`），服务端对未命中的非资源路径回退 `index.html`，`/ui/assets/` 下的哈希资源按 immutable 长缓存。

### 环境变量

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `IOT_PG_DSN` | 是 | PostgreSQL 连接串 |
| `IOT_LISTEN` | 否 | HTTP 监听地址，默认 `127.0.0.1:8080` |
| `IOT_MQTT_URL` | 否 | Broker 地址，默认 `tcp://127.0.0.1:1883` |
| `IOT_MQTT_CLIENT_ID` | 否 | 客户端 ID，默认 `iot-admin` |
| `IOT_MQTT_USERNAME` / `IOT_MQTT_PASSWORD` | 否 | Broker 凭证，留空即匿名 |

测试用（不设置则跳过对应用例）：`IOT_TEST_DSN`（PG 用例，按包名建独立测试库）、`MQTT_BROKER_URL`（Broker 用例）、
`IOT_CAPACITY=1`（容量实测）。

## 校验

```bash
make go-check    # go build ./... && go vet ./... && go test ./...
make web-check   # npm install + tsc --noEmit（前端类型检查）
make web-build   # 前端生产构建，产物入 internal/ui/dist
make dist-check  # 确认 embed 目录有 index.html
make check       # 提交前全量校验（web-check + go-check）
```
