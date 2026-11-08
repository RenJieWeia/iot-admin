-- 网关在线状态：由 Broker 的上下线事件（脚本/LWT）驱动，默认离线。
alter table gateway add column if not exists online boolean not null default false;
alter table gateway add column if not exists online_at timestamptz;
