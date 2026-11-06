-- R-030 两段写入模型：快照不再"只插不改"，而是可修正 + 带版本号。
--
-- revision   : 该格被改写的次数（0 = 首次落库后未被修正过）。只在 value_time 更大时 +1，
--              相等/更小的写入被 WHERE 守卫挡掉，revision 保持不变（幂等）。
-- settled_at : 该格最近一次落库/修正的时刻；snapshot/changes 以它为游标。
--
-- 索引服务于 GET /admin/v1/snapshot/changes 的 (settled_at > since) 扫描与稳定排序。
alter table hourly_snapshot
    add column if not exists revision int not null default 0,
    add column if not exists settled_at timestamptz not null default now();

create index if not exists hourly_snapshot_settled_idx
    on hourly_snapshot (settled_at, terminal_id, item_code, hour_start);
