-- 网关最近上报：批次到达时间（平台视角），用于网关列表的按时间排序/筛选。
alter table gateway add column if not exists last_seen_at timestamptz;

create index if not exists gateway_last_seen_idx on gateway (last_seen_at desc);
