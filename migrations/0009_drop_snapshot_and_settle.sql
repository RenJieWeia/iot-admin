-- 下线快照能力：小时快照 / 小时暂存 / 结算配置。
--
-- 系统从「采集 + 历史」收缩为「采集 + 最新值 + 异常观测」：
--   上报 → 校验 → 清洗 → 写 latest_value → 发布实时
-- 「迟到」这个概念随之消失——没有窗口，就没有迟到。
--
-- 不可逆：执行前已导出存档（.devcache/archive/snapshot-20261106/，gitignore）：
--   hourly_snapshot.sql  37 行 + 8 个月度分区，hour_start 覆盖 2026-10-07 ~ 2026-11-06
--   hourly_staging.sql    0 行（结算后为空）
--   settle_config.sql     1 行（窗口 900s）
-- 恢复需先有 0001–0008 的完整 schema 与被引用的 data_domain / device_type 行。
--
-- 保留：latest_value（最新值）、exception_count（事件型异常）、质量位、实时发布。
-- hourly_snapshot 是分区表，drop 主表会连带删除全部分区，无需 CASCADE。

drop table if exists hourly_snapshot;
drop table if exists hourly_staging;
drop table if exists settle_config;
