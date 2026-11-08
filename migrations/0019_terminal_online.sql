-- 终端在线状态：默认离线；收到该终端报文置在线；同网关批次未带该终端则置离线。
--
-- 网关整机离线暂不处理（平台尚未接入 Broker 的客户端追踪/遗嘱事件）。
alter table terminal add column if not exists online boolean not null default false;
alter table terminal add column if not exists last_seen_at timestamptz;

create index if not exists terminal_gateway_online_idx
    on terminal (gateway_id) where online;
