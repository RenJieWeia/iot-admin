-- 粗档（小时/日/月/年）只按自然边界出快照：步长固定为 1 个自然单位。
--
-- 产品口径：快照计划不再支持「每 2 小时 / 每 3 个月 / 每半年」这类倍数聚合，
-- 秒/分档保持原有多倍数步长不变（见 0013 的 CHECK）。
-- 服务层 internal/snapshot.Validate 先给出可读的 400，本约束是最后一道。
--
-- 存量行：六档初始 step 均为 null（未启用），不会违反约束；已启用的旧倍数配置
-- 需要先改为 1（否则本迁移失败，这是有意的——不静默改写业务配置）。
alter table snapshot_slot_config
    add constraint snapshot_slot_config_coarse_natural_step check (
        granularity not in ('hour', 'day', 'month', 'year')
        or step is null
        or step = 1
    );
