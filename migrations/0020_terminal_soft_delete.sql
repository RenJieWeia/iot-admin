-- 终端软删除：不再物理删除带历史数据的终端，避免大批量行删造成的膨胀与 vacuum 压力。
--
-- 语义：删除 = 标记 deleted_at + 停用 + 离线；列表默认不再返回；接入面按停用终端静默忽略；
-- 最新值/快照/绑定等历史数据保留，由分区保留期自然回收。
alter table terminal add column if not exists deleted_at timestamptz;

create index if not exists terminal_alive_idx on terminal (id) where deleted_at is null;
