-- R-022 行级质量标记：latest_value / hourly_staging / hourly_snapshot 各加两列。
-- 只加列，不建表（nongoal_test 的表名白名单不受影响）。
--
-- quality 是位掩码，可叠加：
--   1 = 物模型 min/max 越界
--   2 = 终端质量规则命中
--   4 = 单位不在换算闭集内（原单位透传）
--   0 = 正常；未使用的高位留给后续扩展
-- quality_reason 存可读摘要（如 source=thing_model item=power 低于下限），多来源用 "; " 连接。
--
-- 原值照常入库：质量异常只做标记，不改变 value_num/value_text/unit，也不影响
-- exception_count.quality_violation 的计数口径。

alter table latest_value
    add column if not exists quality smallint not null default 0,
    add column if not exists quality_reason text;

alter table hourly_staging
    add column if not exists quality smallint not null default 0,
    add column if not exists quality_reason text;

-- 分区父表加列，PG 会逐分区递归（PG16 带常量 default 的 add column 是元数据操作，不重写数据）。
alter table hourly_snapshot
    add column if not exists quality smallint not null default 0,
    add column if not exists quality_reason text;
