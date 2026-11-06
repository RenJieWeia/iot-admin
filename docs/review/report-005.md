# report-005 · 订阅失败可见化 + 网关接入环境护栏（task-005）

- `task:` task-005（v1，`status: READY`；baseline 写 `rv-08`，实际以 `rv-09` 为 base）
- `baseline:` `rv-09`（`284a463`）
- `commits:`
  | 内容 | hash |
  |---|---|
  | 代码 + 两处文档 | `b3ce559` |
  | 本报告 | 见 `git log -- docs/review/report-005.md` |
- `source:` `RESEARCH-003` F6/F7；`OPEN-ISSUES` OP-30/OP-31
- `red-lines:` `go.mod`/`go.sum`/`migrations`/`nongoal_test.go` 零改动；未改 `publish`、重连策略、`Processor`、`http.go`、`admin`、`web/`、`internal/ui`、`cmd/server`

## 1. 变更说明

### 1.1 `internal/ingest/mqtt.go`

| 位置 | 改动 |
| --- | --- |
| `const grantedFailure byte = 0x80` | SUBACK 失败码下界（`0x80` 起都是错误码） |
| `CheckGranted(map[string]byte) error` | 纯函数：任一项 `>= 0x80` 返回错误，消息含主题与 `0x%02X`/十进制值，不含任何凭据 |
| `grantedOf(mqtt.Token) map[string]byte` | 断言 `*mqtt.SubscribeToken` 并取 `Result()`；非订阅 token 或未回填 → `nil`（按通过处理） |
| `Subscribe` | `tok.Wait()` → `tok.Error()` 不变，其后新增 `CheckGranted` 校验，失败**直接返回 error**；成功 `log.Info("MQTT 订阅成功", "topic", filter, "granted", granted)` |

根因（`RESEARCH-003` F6）：paho v1.5.1 的 `Token.Error()` 对 SUBACK `0x80` 返回 `nil`，
被拒信息只在 `SubscribeToken.Result()` 里。原实现只 `tok.Wait(); return tok.Error()`，
于是 ACL 配错时日志只有 `MQTT 已连接`，之后零数据零报错。

`run()` 侧无需改动：`Subscribe` 返回 error 即导致进程退出（fail fast），这是任务书 §5 的硬约束。

### 1.2 `internal/ingest/mqtt_test.go`

新增 `TestCheckGranted` 表驱动，9 个表项：QoS `0/1/2`、多主题全成功、`0x80`、`0x87`、
多主题里任一被拒、`nil` map、空 map。另断言错误信息同时包含 `iot/ingest/+`、`128`、`0x80`
（任务书 §7 的三条验收直接对应前三行）。

### 1.3 `docs/design.md`

| 位置 | 改动 |
| --- | --- |
| §二.11 | 标题改「本期区间表无生产写入路径（能力具备、平台侧未实现）」，正文写明 BroMQ 的 `mqtt_clients` 客户端跟踪（`/api/mqtt/clients`、`/metrics`）与 MQTT 遗嘱（LWT，需网关自己声明 `WillTopic`，正常 DISCONNECT 不触发）已具备，**平台侧写入路径仍未实现**，随网关离线观测任务排期，佐证 `RESEARCH-002` §5/§6 |
| §五.9 | 同上口径；把「网关掉线对平台不可观测」改为「Broker 侧可观测、缺平台侧写入路径」 |

验收：`grep -c "网关掉线对平台不可观测" docs/design.md` = **0**。

### 1.4 `docs/test-env.md`

1. §1 BroMQ 行补「启动必须带 `CONFIG_FILE=.devcache/bromq-provision.yml`，否则 ACL 全拒」。
2. §4 把「端到端未验证」改成「已在隔离 broker 验证（`RESEARCH-003` §2）」，并把仍未验证项
   收窄为「现场 broker 上的链路」与「网关是否声明 LWT」。
3. 新增 §7「Broker 启动护栏与排障顺序」：7.1 `MQTT_ALLOW_ANONYMOUS` 只放行建连、必须
   `CONFIG_FILE` 启动；7.2 ACL/用户缓存 TTL 默认 5 分钟（新建账号立即连接会 `not authorized`，
   实测 `CONNACK 0x05`）；7.3 平台侧护栏（SUBACK 校验 + granted 日志）；7.4 三步排障顺序。

`docs/test-env.md` 是在 Lead 的 `acaaf0a` 最新版上改的，未覆盖其它小节。

## 2. 测试结果

命令统一前置 `GOCACHE=$PWD/.devcache/gocache`。

```
$ go build ./... && go vet ./...
（无输出）

$ go test ./... -count=1                       # 无 DSN
ok  cmd/server 0.009s | internal/admin 0.013s | internal/clean 0.005s | internal/ingest 0.011s
ok  internal/meta 0.004s | internal/staging 0.005s | internal/ui 0.010s | internal/validate 0.005s

$ IOT_TEST_DSN=… go test -p 1 ./... -count=1   # 有 DSN
ok  cmd/server 0.008s | internal/admin 0.135s | internal/clean 0.003s | internal/ingest 2.626s
ok  internal/meta 0.003s | internal/staging 0.002s | internal/ui 0.009s | internal/validate 0.003s

$ go test ./internal/ingest -run TestCheckGranted -v -count=1
--- PASS: TestCheckGranted (0.00s)
```

验收对照（task-005 §7）：

| 验收项 | 结果 |
| --- | --- |
| `CheckGranted({"iot/ingest/+": 0x80})` 非 nil，含主题与 `128`/`0x80` | ✅ 表项「0x80 订阅被拒」+ 末尾错误信息断言 |
| `CheckGranted({"iot/ingest/+": 1})` → nil | ✅ 表项「granted QoS1」 |
| `CheckGranted(nil)` → nil | ✅ 表项「nil map 视为通过」 |
| 无 DSN 全绿、`go vet` 无输出 | ✅ |
| `docs/design.md` 不再出现绝对表述 | ✅ `grep -c` = 0 |
| `grep -n "granted" internal/ingest/mqtt.go` 有输出 | ✅ 5 处 |

## 3. 风险

1. **行为变化是"启动失败"**：ACL 配错现在会让进程起不来（预期 fail fast）。部署侧要把这条错误
   当成配置问题处理（日志会指出被拒主题与 granted 值），而不是当崩溃重试。
2. **未做 broker 级实测**：本地没有"拒绝型 ACL"的 broker 可复现 0x80（`RESEARCH-003` 里是
   另一条路径复现的），因此 `CheckGranted` 由表驱动单测覆盖，`subscribe → 0x80 → 退出` 的
   端到端链路**未在本机跑过**，属未验证假设。
3. **多 filter 时日志是 map 形态**：`granted` 以 map 打印，顺序不定；当前只有一个 filter，
   可读性够用；将来多订阅时建议改成逐主题一行。
4. **QoS 语义未变**：仍订阅 QoS 1，不改连接参数与重连策略（任务书非目标）。

## 4. 未闭环

- 无新增 `Q-NNN`。
- `docs/test-env.md` 里「网关是否声明 LWT」仍是未知事实，影响的是后续离线观测任务（不在本任务范围）。
