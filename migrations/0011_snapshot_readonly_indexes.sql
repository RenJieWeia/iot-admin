-- 快照只读页的索引：游标分页按 (segment_start desc, terminal_id, item_code[, granularity]) 翻页。
--
-- 三张表的 PK 都是 (terminal_id, item_code, [granularity,] segment_start)，最左前缀是"序列"，
-- 支撑不了"按段起点倒序跨序列翻页"——没有这条索引，每翻一页都要扫全部存活分区。
--
-- 分区表上建索引会在每个分区上各建一份，随分区续建自动生效（PG 的分区索引机制）。

-- 粗表：既支持"跨档按段起点翻页"，也支持"先按档位过滤再翻页"。
create index if not exists snapshot_coarse_segment_idx
    on snapshot_coarse (segment_start desc, terminal_id, item_code, granularity);

create index if not exists snapshot_minute_segment_idx
    on snapshot_minute (segment_start desc, terminal_id, item_code);

create index if not exists snapshot_second_segment_idx
    on snapshot_second (segment_start desc, terminal_id, item_code);
