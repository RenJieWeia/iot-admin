-- 边缘网关注册表：只有注册且启用的网关才能在接入面发布消息。
--
-- 语义：接入面（HTTP X-Gateway-Id / MQTT iot/ingest/{gateway_id}）先查这张表，
-- 未注册返回 403 gateway_unknown，已注册但停用返回 403 gateway_disabled；
-- 平台自身向 Broker 发布实时消息不经过此表（发布是平台身份）。
--
-- 注意：Broker 侧的连接账号与 ACL 仍由 Broker 管理，本表只做平台侧接入鉴权，
-- 两处名单需要同步维护（见 README「边缘网关」）。
create table if not exists gateway (
    gateway_id text primary key,
    name       text not null,
    enabled    boolean not null default true,
    remark     text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
