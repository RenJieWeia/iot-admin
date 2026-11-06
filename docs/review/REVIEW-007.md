# REVIEW-007 · task-005 复核（订阅失败可见化 + 接入护栏）

- `basis:` `rv-10..b3ce559`（代码）+ `241bef8`（report-005）
- `verdict:` **放行**，同意打 `rv-11`
- `method:` 命令复跑 + **端到端 fail-fast 实测**（自建 deny-all broker）

## 1. 范围

`git show --name-only b3ce559` 恰为书里四项：`internal/ingest/mqtt.go`、`internal/ingest/mqtt_test.go`、`docs/design.md`、`docs/test-env.md`。`go.mod`/`go.sum`/`migrations`/`nongoal_test.go`/`web`/`internal/ui`/`cmd/server` 零改动。

## 2. 代码复核

- `CheckGranted(map[string]byte) error`：任一项 `>= 0x80` 返回错误，文案含主题、`0x%02X`、十进制值，**无凭据**。
- `grantedOf`：`*mqtt.SubscribeToken` 断言取 `Result()`，非订阅 token / 未回填 → `nil`（按通过）。
- `Subscribe`：`tok.Error()` 之后接 `CheckGranted`，失败 **return err**（不是 warning），`run()` 随即退出；成功打印 `MQTT 订阅成功 … granted=…`。
- Assumption（已登记）：`CheckGranted` 用导出名，因既有 `mqtt_test.go` 在 `package ingest_test`；internal 包内纯函数，不构成对外 API 扩张。**接受**，不要求改用 `export_test.go`。

## 3. 命令复核

```
$ go build ./... && go vet ./...                 → 干净
$ go test ./internal/ingest -run TestCheckGranted -v → PASS（表驱动 9 项）
$ go test ./... -count=1（无 DSN）                → 8 包全绿
$ IOT_TEST_DSN=… go test -p 1 ./... -count=1     → 8 包全绿
$ grep -c "网关掉线对平台不可观测" docs/design.md   → 0
```

## 4. 端到端 fail-fast（补做对方未验证项）

对方报告 §3.2 标注"`subscribe → 0x80 → 退出` 未实测（本机无拒绝型 ACL broker）。我用仓库内 BroMQ 起了一个**不加载 provisioning** 的实例（匿名可建连、ACL 全拒）：

```
$ MQTT_TCP_ADDR=127.0.0.1:1886 MQTT_ALLOW_ANONYMOUS=true (无 CONFIG_FILE) .devcache/bin/bromq
$ IOT_PG_DSN=… IOT_MQTT_URL=tcp://127.0.0.1:1886 IOT_LISTEN=127.0.0.1:8090 .devcache/bin/iot-admin
{"level":"INFO","msg":"MQTT 已连接"}
{"level":"ERROR","msg":"服务退出","err":"订阅被拒绝: topic=\"iot/ingest/+\" granted=0x80（128），请检查 Broker ACL 与配置"}
exit=1
```

结论：**fail-fast 生效**，日志含主题与 granted 值，不再出现"已连接但零数据零报错"；该未验证项从"假设"升级为"已实测"。

## 5. 遗留

- 现场 broker 的 LWT（`U-017`）与真实网关侧 ACL 仍未验证，属网关接入任务范围。
- `docs/test-env.md` 新增 §7（匿名只放行建连、ACL 缓存 5 分钟实测 `CONNACK 0x05`、SUBACK 护栏、三步排障顺序），与我的实测一致。
