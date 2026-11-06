# 测试环境清单

> 只记录环境事实与探测结论，不含口令。口令一律放 `.devcache/test-env.env`（已被 `.gitignore` 忽略）。
> 最近核实：2026-11-05 21:10（本沙箱实测）。

## 1. 本地开发环境（当前沙箱）

| 组件 | 地址 / 路径 | 凭据 | 说明 |
| --- | --- | --- | --- |
| PostgreSQL 16 | `127.0.0.1:55432`，库 `iot` | `postgres` / `pg` | Podman 容器 `iot-pg`（`--network host`）；DSN `postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable` |
| BroMQ（本仓库构建） | MQTT `127.0.0.1:1883`、WS `127.0.0.1:1884`、HTTP API `127.0.0.1:18080` | 匿名开；控制台 `admin` / `admin` | 二进制 `.devcache/bin/bromq`，配置 `.devcache/bromq.conf`，预置 `.devcache/bromq-provision.yml`（`anonymous` 用户 + `#` 全通 ACL），数据 `.devcache/bromq-data/`，日志 `.devcache/log/bromq.log` |
| iot-admin | `127.0.0.1:8080`（`IOT_LISTEN`） | 无鉴权（`AllowAll`，仅监听回环） | 接入 `POST /ingest/v1/messages`、管理 API `/admin/v1/*`、管理后台 `/ui/*` |
| 启动方式 | `.devcache/bin/bromqup.sh`（幂等）· `pgup.sh` | — | 沙箱每次 bash 调用在 bwrap 内（`--die-with-parent`、fresh `/tmp`）：broker 只活在启动它的那次调用里，或改成后台任务 |

平台侧默认配置（`configs/server.env.example`）：`IOT_MQTT_URL=tcp://127.0.0.1:1883`、`IOT_MQTT_CLIENT_ID=iot-admin`、`IOT_MQTT_USERNAME/PASSWORD` 留空。

## 2. 测试实例（现场 BroMQ）

| 项 | 值 |
| --- | --- |
| 控制台 | http://192.168.100.1:8080/dashboard （`GET /dashboard` → 200，`GET /health` → 200） |
| 控制台账号 | `renjie`（**dashboard 用户**，`POST /api/auth/login` 实测 200 并返回 JWT；口令见 `.devcache/test-env.env`） |
| MQTT TCP | **`192.168.100.1:1884`**（非默认 1883） |
| MQTT WebSocket | 存在（控制台在线客户端 `3zhang-jingsanlu` 的 `listener` 字段为 `ws`），具体端口未探明 |
| 匿名接入 | **不开放**：1884 回 `CONNACK 20 02 00 05`（not authorized） |
| 平台 MQTT 用户 | **`iot-admin`（id 3）**：2026-11-06 通过 dashboard API 创建，口令见 `.devcache/test-env.env`（`BROMQ_TEST_MQTT_PASS`） |
| 该用户 ACL | `iot/ingest/+` → `sub`，`iot/realtime/#` → `pub`（`POST /api/acl`，均 201；permission 只接受 `pub`/`sub`/`pubsub`） |
| 既有 MQTT 用户 | `3zhang`（id 2）、`testdev`（id 1） |
| 在线客户端 | `3zhang-jingsanlu`：MQTT v5、`keepalive=20`、`clean=false`、1 个订阅、来源为 `ws` |

### 实测命令（可复现）

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://192.168.100.1:8080/health          # 200
curl -s -o /dev/null -w '%{http_code}\n' http://192.168.100.1:8080/dashboard       # 200
curl -s -H 'Content-Type: application/json' \
     -d '{"username":"renjie","password":"<见 .devcache/test-env.env>"}' \
     http://192.168.100.1:8080/api/auth/login                                      # 200 + token
curl -s -H "Authorization: Bearer <token>" http://192.168.100.1:8080/api/clients    # 2 个在线客户端
curl -s -H "Authorization: Bearer <token>" http://192.168.100.1:8080/api/mqtt/users # 3zhang / testdev

# 端口连通性：8080 ✓、1884 ✓、1883 ✗（connection refused）、8883 ✗、443/80 ✗
# 1884 是 MQTT TCP（发 CONNECT 得到 CONNACK 0x05 not authorized，而非 WS 握手）
```

### 对接平台时的直接含义

1. `IOT_MQTT_URL` 必须写 **`tcp://192.168.100.1:1884`**，沿用默认的 `tcp://...:1883` 连不上。
2. 平台必须带 **MQTT 用户凭据**（`IOT_MQTT_USERNAME` / `IOT_MQTT_PASSWORD`）：匿名被 `CONNACK 0x05` 拒绝。控制台的 `renjie` 只用于 dashboard/HTTP API，**不是** MQTT 账号。
3. 平台订阅/发布所需 ACL 已就绪并实测：`SUBACK 90 03 00 01 00`（`iot/ingest/+` 授权）、`PUBACK 40 02 00 01`（`iot/realtime/#` 发布成功）；以 `IOT_MQTT_URL=tcp://192.168.100.1:1884` + `iot-admin` 凭据启动 iot-admin，日志出现 `MQTT 已连接` 且订阅成功、HTTP 正常监听（端口 8082 实测）。

## 3. 对 UNKNOWN 的影响（待 reviewer 落台账）

| 编号 | 原问题 | 本次事实 | 建议状态 |
| --- | --- | --- | --- |
| U-012 | 给定账号口令属于 dashboard 用户还是 MQTT 用户 | `renjie/…` 对 `/api/auth/login` 成功 → **dashboard 管理员**；平台所需 MQTT 用户已用该 dashboard 账号创建（`iot-admin`） | 可结 |
| U-013 | BroMQ 是否在 1883 监听、是否有 TLS/WS 端口 | 1883 **不可达**；**1884 是 MQTT TCP**；另有 WS 监听（端口未明）；未见 TLS | 部分消解，WS 端口与 TLS 仍待确认 |
| U-014 | 在线客户端 `3zhang-jingsanlu` 是否为待接入网关 | 仍在线，`listener=ws`、v5、`clean=false`、1 个订阅 | 维持待确认 |
| U-015 | 现场 BroMQ 版本是否与本地一致 | 控制台 API 未暴露版本端点（`/api/config` 404）；`/health` 不含版本 | 仍待确认 |

## 4. 平台接入测试实例的启动方式

```bash
cd /home/renjie/codespace/iot-admin
. .devcache/test-env.env          # 取 BROMQ_TEST_* （不入库的口令在这里）
GOCACHE=$PWD/.devcache/gocache go build -o .devcache/bin/iot-admin ./cmd/server
IOT_PG_DSN='postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable' \
IOT_MQTT_URL="$BROMQ_TEST_MQTT_URL" \
IOT_MQTT_USERNAME="$BROMQ_TEST_MQTT_USER" \
IOT_MQTT_PASSWORD="$BROMQ_TEST_MQTT_PASS" \
IOT_LISTEN=127.0.0.1:8082 .devcache/bin/iot-admin
```

实测（2026-11-06 08:34）：`MQTT 已连接` + `HTTP 监听 127.0.0.1:8082`，`GET /ui/` 200。

**仍未验证**：网关侧（`3zhang-jingsanlu`）向 `iot/ingest/+` 实际上报并在平台侧落库的端到端链路——需要一条真实网关报文或临时给测试用户加 `iot/ingest/+` 的 `pub` 权限；本期不做，等联调窗口。

## 5. 常驻 UI 实例（给用户看的那个）

| 项 | 值 |
| --- | --- |
| 地址 | **http://127.0.0.1:8084/ui/**（8 个页面：`/ui/{6 实体}` + `/ui/exceptions/{events,intervals}`） |
| 版本 | `rv-08` 构建，`HEAD=191c1df`（净含 skip 链接布局修复、CSP 放开样式内联、favicon、焦点归还修复） |
| 后端 | 本机 PG（`127.0.0.1:55432`），MQTT 用默认 `tcp://127.0.0.1:1883` |
| 产物校验 | 服务端 index 引用 `assets/index-L8FPHqSc.js` = 仓库 `internal/ui/dist/assets/index-L8FPHqSc.js`（即“能访问到最新版本”的判据） |
| 端口说明 | `8080` 被早期 SSR 版残留进程占据（在别的 PID 命名空间里，沙箱内杀不掉，重启沙箱自然消失），不要用 8080 判断版本 |

**刷新到最新版本的标准动作**（每次前端提交后由 Lead 执行）：

```bash
cd /home/renjie/codespace/iot-admin
git log --oneline -1                                   # 确认提交
GOCACHE=$PWD/.devcache/gocache go build -o .devcache/bin/iot-admin ./cmd/server
# 停掉旧实例后台任务后重启（8084）
IOT_PG_DSN='postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable' \
IOT_LISTEN=127.0.0.1:8084 .devcache/bin/iot-admin
# 校验：服务端引用的 asset 哈希 == 仓库 dist 里的哈希
```

> `internal/ui/dist` 入仓 + `go:embed`，所以“重建二进制”就等于“切到最新前端产物”；不需要装 node 跑 Vite。

## 6. 测试库隔离（task-006 / R3）

带 `IOT_TEST_DSN` 跑 `go test ./...` 时，Go 默认按包并行。`internal/ingest` 的测试夹具会
`truncate` 全部 11 张业务表（`internal/ingest/helper_test.go` 的 `newEnv`），
`internal/admin` 的 DB 用例也会删自己的 `qtest-*` 行——两者同一个库就会互踩（现象是随机红，
`-p 1` 才稳定）。现在改成**每个测试包一个库**，`go test ./...` 默认并行即可全绿。

### 6.1 规则

| 包 | 实际使用的库 | 派生方式 |
| --- | --- | --- |
| `internal/admin` | `iot_admin` | `IOT_TEST_DSN` 的库名 + `_admin` |
| `internal/ingest` | `iot_ingest` | `IOT_TEST_DSN` 的库名 + `_ingest` |

- 运维只维护**一条** `IOT_TEST_DSN`（如 `postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable`），
  host/端口/凭据/参数原样沿用，只替换库名。
- 派生与建库逻辑各包内各一份（`internal/admin/dsnhelper_test.go`、`internal/ingest/dsnhelper_test.go`），
  不新增共享包、不改 `internal/store`。
- `IOT_TEST_DSN` 未设置时，所有 DB 用例照旧 `t.Skip`。

### 6.2 建库幂等

每个 DB 用例先 `ensureDatabase(dsn)`：

1. 用同一凭据连**维护库** `postgres`，查 `pg_database` 是否已有目标库；
2. 已存在 → 直接返回（重复运行、并行运行都不会报错）；
3. 不存在 → `create database <name>`（标识符经 `pgx.Identifier` 转义）；并发建库撞上
   `42P04 duplicate_database` 视为已存在。
4. 之后照常 `store.Connect` + `store.Migrate`，迁移在各自库里跑（`iot_admin`、`iot_ingest`
   各有自己的 `schema_migrations` 与 9 张表，互不影响）。

### 6.3 失败模式（无建库权限）

测试用户没有 `createdb` 权限时，`_meta` 这类不碰库的用例照常跑，DB 用例
`t.Skipf("跳过需要 PostgreSQL 的用例：测试用户没有建库权限…")`——**不会**退回去用基础库，
避免"看着绿其实跑错了库"。连接/建库的其他失败（连不上、SQL 错）一律 `t.Fatalf`，不吞。

### 6.4 验收命令

```bash
# 1) 默认并行（新口径，重复 3 次都应全绿）
IOT_TEST_DSN='postgres://postgres:pg@127.0.0.1:55432/iot?sslmode=disable' \
  GOCACHE=$PWD/.devcache/gocache go test ./... -count=1
# 2) 串行（兼容路径仍在）
IOT_TEST_DSN='…' GOCACHE=$PWD/.devcache/gocache go test -p 1 ./... -count=1
# 3) 无 DSN（DB 用例 skip）
GOCACHE=$PWD/.devcache/gocache go test ./... -count=1
```

> 首次跑会多建两个库；想清理可 `drop database iot_admin; drop database iot_ingest;`，
> 下次运行会自动重建。基础库 `iot` 不再被测试写入。
