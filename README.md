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

## 边缘网关（接入鉴权）

只有登记在 `gateway` 表、且 `enabled = true` 的网关才能在接入面发布消息：

- HTTP：`X-Gateway-Id` 未注册 → `403 gateway_unknown`；已注册但停用 → `403 gateway_disabled`
- MQTT：`iot/ingest/{gateway_id}` 主题做同一判定，失败只记日志丢弃（MQTT 没有应答码）
- 平台自身向 Broker 发布实时消息使用平台连接，不经过该表

管理面「边缘网关」页维护 `网关 ID / 名称 / 备注 / 启用`，并展示 `最近上报 / 在线状态 /
MQTT 主题 / MQTT 密码（掩码）`；写入后注册表缓存立即失效，
绕过管理面直接改库最多脏 `gateway.DefaultTTL`（30s）。
平台与 Broker 之间**只走开放接口**（HTTP 管理 API），不直接改 Broker 的库或配置文件。
网关在 Broker 侧就是一个 MQTT 用户 + 一条发布 ACL：

```
POST   {IOT_BROKER_ADMIN_URL}/api/auth/login   {"username","password"} → {"token"}
POST   /api/mqtt/users                         {"username": 网关ID, "password": 一次性密码, "description"}
POST   /api/acl                                {"mqtt_user_id", "topic": "iot/ingest/{网关ID}", "permission": "pub"}
GET    /api/mqtt/users                         找 username=网关ID 的 id
DELETE /api/mqtt/users/{id}                    删除用户（ACL 级联删除）
Authorization: Bearer {token}
```

- 新增网关：只有勾选「启用」时才在 Broker 建号 + 授权，成功后再写本地表；未启用只写本地
- 启用/停用：列表操作栏的开关切换启用状态——`false→true` 建号并回一次性密码，`true→false` 注销账号
  - 平台生成 43 位随机密码，AES-256-GCM 加密存 `gateway_credential`；列表行右键可查看/复制/重置
  - 加密密钥来自 `IOT_CREDENTIAL_KEY`（32 字节 base64/hex）或 `IOT_CREDENTIAL_KEY_FILE`
- 删除网关：先销号，成功后再删本地行；Broker 侧用户不存在视为已注销（幂等）
- Broker 调用失败返回 `502 broker_register_failed / broker_unregister_failed`，本地不做半套变更
- 未配置 `IOT_BROKER_ADMIN_URL` 时只写本地注册表并打告警（本地开发/离线环境）

## 上报契约

一条消息承载同一网关下的多个终端；网关身份只来自接入层（MQTT topic / `X-Gateway-Id`），
payload 里除下列字段外的内容一律忽略。

```json
{
  "terminals": [
    {
      "terminalId": "T-1001",
      "items": [
        { "code": "temp", "value": 23.5, "collectedAt": "2026-11-10T17:20:00+08:00" }
      ]
    }
  ]
}
```

| 层级 | 字段 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| 信封 | `terminals` | 是 | 非空数组 |
| 终端 | `terminalId` | 是 | 必须已注册、启用且未删除 |
| 终端 | `items` | 是 | 非空数组；同一终端内 `collectedAt` 跨度 ≤ 1 小时 |
| 数据项 | `code` | 是 | 须在所属物模型中声明；同一终端内不可重复 |
| 数据项 | `value` | 是 | number/string/bool，须与物模型 `value_type` 一致 |
| 数据项 | `collectedAt` | 是 | RFC3339（带偏移或 Z）、省略偏移按 +08:00、秒/毫秒 epoch；不得超前平台 5 分钟 |

单位只取物模型声明，payload 里的 `unit` 会被忽略；`gatewayId`、`msgType`、`messageId`、`ver`
等历史字段已删除，带上也不会报错。

处理口径：

- 未声明的 `code`：静默丢弃，不报警
- 必填字段（`required=true`）缺失：该项 `value_missing`，记 `msg_incomplete`
- `value` 类型不符：该项剔除，记 `clean_failed`
- `collectedAt` 不可解析：该项剔除，记 `msg_incomplete`
- 数值越界：**不拒收**，入库并标质量位，记 `quality_violation`
- 终端停用/已删除：静默忽略（`accepted=true`、`written=0`）
- 终端不存在：拒收 `terminal_unknown`
- 平台写库故障：整包 503，网关应重试

HTTP 应答（MQTT 没有业务应答，只有 PUBACK）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "results": [
      {
        "terminalId": "T-1001",
        "accepted": true,
        "written": 1,
        "published": true,
        "failedCount": 1,
        "missingCodes": ["pressure"],
        "items": [{ "code": "pressure", "reason": "value_missing", "detail": "required 属性缺失" }]
      }
    ]
  }
}
```

`items` 最多 20 条、`missingCodes` 最多 10 个；`failedCount` 始终是真实总数。

平台向 Broker 发布实时消息：主题 `iot/realtime/{domain_code}/{type_code}/{terminal_id}`，
QoS 1、retain=false，只包含实际写入成功的项：

```json
{
  "terminalId": "T-1001",
  "publishedAt": "2026-11-10T17:20:00.123+08:00",
  "items": [{ "code": "temp", "value": 23.5, "unit": "C", "collectedAt": "2026-11-10T17:20:00+08:00" }]
}
```

网关在线状态：Broker 脚本（`on_connect` / `on_disconnect`）向 `iot/gateway/{gateway_id}/status`
发布 retained `{"online":true|false}`；平台订阅后更新网关与终端的在线状态。

## 校验

```bash
make go-check    # go build ./... && go vet ./... && go test ./...
make web-check   # npm install + tsc --noEmit（前端类型检查）
make web-build   # 前端生产构建，产物入 internal/ui/dist
make dist-check  # 确认 embed 目录有 index.html
make check       # 提交前全量校验（web-check + go-check）
```
