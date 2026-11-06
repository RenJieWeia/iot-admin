# 项目扩展规范（admin-ui-spec 附录 A）

本文件承接「只在 iot-admin 成立」的界面约定：业务词、状态枚举与颜色映射、操作名、页面清单，
以及明确不做的条款。共通的交互/布局条款以 admin-ui-spec 为准，本文件只写**项目特有**部分，
冲突时业务词与枚举以本文件为准。

> 维护约定：新增实体、只读页、状态枚举或操作名时**同一次提交内**更新本文件；
> 文案以 `internal/admin/*.go` 的 `entityLabels` 与各 `*Options` 为准，前端不另起名字。

---

## 1. 术语表

界面用词与代码标识的对照。**一个概念只有一个中文名**，不在不同页面换说法。

| 中文名 | 代码/接口标识 | 含义 |
|---|---|---|
| 数据域 | `data-domains` / `data_domain` | 业务域（当前只有 `energy`） |
| 终端类型 | `device-types` / `device_type` | 一类终端的物模型载体（当前只有 `meter`） |
| 物模型属性 | `thing-model-properties` / `thing_model_property` | 终端类型下声明的数据项定义 |
| 终端 | `terminals` / `terminal` | 一台被采集的设备，归属于某网关 |
| 质量规则 | `quality-rules` / `quality_rule` | 行级质量判定规则 |
| 结算窗口 | `settle-configs` / `settle_config` / `settle_config` 表 | 按域/类型配置的小时关闭窗口（默认 900 秒） |
| 网关 | `gateway_id` | 上报来源标识；**不要**与「终端」混用 |
| 采集时间 | `value_time` / `collectedAt` | 设备侧采样时刻 |
| 归属小时 | `hour_start` | 数据落库所归属的整点小时 |
| 落库时间 | `settled_at` | 快照写入/修正时刻 |
| 修正次数 | `revision` | 快照格子被覆盖的次数（>0 即被改写过的格子） |
| 最新值 | `latest-values` | 每（终端, 数据项）一行，最近一次写入 |
| 小时暂存 | `hourly-staging` | 未结算小时的中间态 |
| 小时快照 | `hourly-snapshot` | 结算后的定稿数据 |
| 异常事件 | `exception-events` | 事件型异常（计数表，按小时聚合） |
| 数据项 | `item_code` | 物模型里的一个属性编码 |
| 补录 | `msgType=2` | 补传历史数据（只作来源标记，不影响落库路径） |

**已下线、不要再用**：`异常区间` / `exception-intervals` / `gateway_disconnected` / `item_silent` /
`auth_failed`（状态型异常整套已移除）、`快照变更查询` 的**写入侧留痕**（`snapshot_cell_history`）。

---

## 2. 状态枚举与颜色映射

### 2.1 颜色通道

状态色只有五档语义 `tone`，取值与令牌一一对应（颜色只来自 `web/src/styles/tokens.css`，
组件里**不得**出现颜色字面量）：

| tone | 背景令牌 | 文字令牌 | 语义 |
|---|---|---|---|
| `success` / `positive` | `--color-positive-bg` | `--color-positive-text` | 正常、启用、成功 |
| `warning` | `--color-warning-bg` | `--color-warning-text` | 需关注但不阻断 |
| `danger` / `negative` | `--color-negative-bg` | `--color-negative-text` | 失败、停用、拒收 |
| `info` | `--color-info-weak` | `--color-info` | 中性说明/链接型状态 |
| `neutral` | `--color-neutral-bg` | `--color-neutral-text` | 无状态、空值 |

规则：**颜色只是辅助**，状态标签必须同时有文字（§17）；不得用颜色单独表达状态；
不得为状态新增第六档 tone。

### 2.2 枚举清单（权威来源：`internal/admin/fields.go` 的 `*Options`）

| 枚举 | 取值 → 文案（tone） |
|---|---|
| 启用状态 `enabled` | `true`→启用(success)、`false`→停用(danger) |
| 布尔值 `boolean` | `true`→是(success)、`false`→否(neutral) |
| 值类型 `value_type` | `number`→数值、`string`→文本、`bool`→布尔（均 neutral） |
| 规则类型 `rule_type` | `range`→范围、`enum`→枚举、`precision`→精度、`unit_whitelist`→单位白名单、`not_null`→非空（均 neutral） |
| 质量位 `quality` | `0`→正常(neutral)；`1`越界 / `2`规则命中 / `4`单位闭集外 / `3,5,6,7` 为组合（均 warning） |
| 事件型异常 `error_type` | `upload_timeout`→上报超时(warning)、`protocol_error`→协议错误(danger)、`msg_incomplete`→消息不完整(warning)、`clean_failed`→清洗失败(warning)、`cache_write_failed`→写入失败(danger)、`mqtt_publish_failed`→发布失败(danger)、`snapshot_failed`→快照失败(danger)、`late_data`→迟到数据(warning)、`quality_violation`→质量违规(warning) |

> 注：`terminal_unknown`（终端未注册）在闭集里有定义，但后台筛选下拉当前**未**提供该项
> ——留作已知缺口，补齐时同步本表。

### 2.3 上报应答的逐终端状态（对外契约，见 `internal/ingest/pipeline.go`）

| 情形 | `accepted` | `reason` | 其他 |
|---|---|---|---|
| 全部项成功 | `true` | — | `written` > 0 |
| 部分项失败 | `true` | — | `failedCount` + `items[]` |
| 全部项失败 | `false` | `all_items_failed` | `items[]` 列全部失败项 |
| 结构性拒收 | `false` | `msg_incomplete` / `protocol_error` / `terminal_unknown` 等 | `missingCodes` 可能非空 |
| 平台故障 | `false` | `internal_error` | HTTP 整包 503 |

---

## 3. 操作名表

按钮与菜单文案固定用右列，不用「确定/提交/是」这类手势词（§18）。

| 场景 | 文案 | 备注 |
|---|---|---|
| 新建实体 | 新建{实体名}，如「新建终端」 | 顶栏主按钮 |
| 编辑 | 编辑 | 行内次级按钮 |
| 删除 | 删除 | 需二次确认，确认按钮文案为「删除 {对象名}」 |
| 保存 | 保存 | 请求中保持文案不变 + `disabled` + `aria-busy` |
| 取消 | 取消 | 只关闭浮层/抽屉，不撤销已生效的筛选 |
| 关闭浮层 | 完成 | 「更多筛选」底部为「取消 / 完成」，并标注「修改即生效」 |
| 重置查询 | 重置查询 | 仅在后端 400 时出现，清空 q/filters/sort/order/page |
| 清除筛选 | 清除筛选 | 只清 keyword + 筛选条件，不动排序 |
| 刷新 | 刷新 | 空态/失败态恢复入口 |
| 重试 | 重试 | 失败态恢复入口，不叫「再试一次」 |
| 加载更多 | 加载更多 | 游标分页唯一前进方式 |
| 查看更多 | 更多筛选 | 打开次级筛选浮层 |

---

## 4. 页面清单

导航由 `GET /admin/v1/_meta` 驱动，前端不硬编码实体名。当前共 **6 个实体页 + 4 个只读页**。

### 4.1 实体页（可增删改查）

| 页面 | 路由 | 主要字段 |
|---|---|---|
| 数据域 | `/ui/data-domains` | 编码、名称 |
| 终端类型 | `/ui/device-types` | 编码、名称、数据域（关联） |
| 物模型属性 | `/ui/thing-model-properties` | 终端类型（关联）、数据项编码、名称、值类型、单位、必填、小数位、最小值、最大值 |
| 终端 | `/ui/terminals` | 终端 ID、终端类型（关联）、网关 ID、名称、状态 |
| 质量规则 | `/ui/quality-rules` | 终端、数据项、规则类型、阈值等 |
| 结算窗口 | `/ui/settle-configs` | 数据域/终端类型、窗口秒数 |

### 4.2 只读页（仅查询，无写入口）

| 页面 | 路由 | 分页方式 | 说明 |
|---|---|---|---|
| 最新值 | `/ui/readonly/latest-values` | offset | 每（终端, 数据项）一行 |
| 小时暂存 | `/ui/readonly/hourly-staging` | offset | 未结算小时 |
| 小时快照 | `/ui/readonly/hourly-snapshot` | cursor | 结算后数据，含修正次数 |
| 异常事件 | `/ui/exceptions/events` | offset | 事件型异常计数 |

> 游标分页页必须显式声明「只能继续加载，不能回到指定页」（table-query §6）。
> 已不存在「异常区间」页——状态型异常整套下线（迁移 `0008`）。

---

## 5. 已知豁免（明确不做）

以下条款在本项目**不适用或不实施**，理由如下；如需改变，走新的需求，不在整改里顺手加。

| 条款 | 不做的内容 | 理由 |
|---|---|---|
| §5.2 | 列可见性与导出 | 列由 `_meta` 的 `visible` 固定下发，用户级列偏好需要持久化存储与后端支持；导出需要服务端异步任务与结果保留期，属独立能力。当前页面只有 5–10 列，横向滚动已可覆盖。 |
| §9 | 批量选择与批量操作（L1/L2/L3 全豁免） | 后台 6 个治理对象的操作是低频单条编辑；批量删除/批量启用属高风险动作，需要服务端按条件执行 + 二次确认 + 部分失败重试，成本与收益不匹配。**注意**：本次豁免的是「批量」，不是「范围声明」——列表的分页/筛选范围文案仍须准确。 |
| §3.1 顶栏四项 | 全局搜索、主题切换、用户菜单 | 项目无鉴权（`AllowAll`，仅监听 127.0.0.1），没有「用户」概念；不做 dark mode 是既定决策（`tokens.css` 只有一套浅色令牌）。顶栏只保留页面标题与主操作。 |
| §3.2 二级导航 | 侧栏多级展开 | 导航按「实体 / 数据 / 异常」三组扁平排列，层级不超过一层。 |

---

## 6. 已知行为（不是缺陷，不要"修"）

| 行为 | 触发条件 | 为什么这样是对的 |
|---|---|---|
| 首屏骨架行数可能与回包行数不一致，页面会长高一次 | 首次进入某列表页，且「每页条数」> 10，且回包行数 > 10 | 首屏之前没有任何可参考的行数，「猜 10 行」是唯一合理默认。进入过一次后，骨架已改为 `min(每页条数, 上一次实际行数)`，后续翻页不再跳变。**不要把它改成按每页条数画 50 行**——那会把「数据多时一次长高」换成「数据少时一次塌陷」，后者更刺眼（页脚上移、点击落空）。 |
| 分页区同时出现「第 1–4 条，共 4 条」与「第 1 页」 | 所有 offset 分页页 | 前者是**范围**，后者是**页码控件**，不是同一信息重复。§16.1 要去重的是「表格说明 + 分页区都报总数」，已去掉表格说明里的数字。 |

## 7. 待补项（下次评审）

1. `terminal_unknown` 在事件型异常筛选下拉里缺失（闭集有、选项没有），用户只能看到原始 code。
2. 只读页未接滚动位置记忆（实体页有 `lib/scroll.ts`），两页体验不一致。
3. 移动端筛选抽屉与表格横向滚动的组合尚未在真机验证（只有 headless Chromium）。
