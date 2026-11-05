-- 运行时表：最新值、小时暂存、小时快照（月度分区）。

create table if not exists latest_value (
    terminal_id text not null,
    item_code   text not null,
    value_num   double precision,
    value_text  text,
    unit        text,
    value_time  timestamptz not null,
    updated_at  timestamptz not null default now(),
    primary key (terminal_id, item_code)
);

create table if not exists hourly_staging (
    terminal_id text not null,
    item_code   text not null,
    hour_start  timestamptz not null,
    value_num   double precision,
    value_text  text,
    unit        text,
    value_time  timestamptz not null,
    updated_at  timestamptz not null default now(),
    primary key (terminal_id, item_code, hour_start)
);

create index if not exists hourly_staging_hour_idx on hourly_staging (hour_start);

-- 分区表：PG 要求唯一约束包含分区键，主键即 (terminal_id, item_code, hour_start)。
-- 不另加自增 id；冻结语义 = 写入后不再 UPDATE；过期 = 分区 DROP（物理消失）。
create table if not exists hourly_snapshot (
    terminal_id text not null,
    item_code   text not null,
    hour_start  timestamptz not null,
    value_num   double precision,
    value_text  text,
    unit        text,
    value_time  timestamptz not null,
    created_at  timestamptz not null default now(),
    primary key (terminal_id, item_code, hour_start)
) partition by range (hour_start);
