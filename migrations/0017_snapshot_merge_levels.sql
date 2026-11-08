-- 快照配置收敛：时/日/月/年合并为「快照」一个开关，秒档下线。
--
-- 现状（0013）：snapshot_slot_config 六行（second/minute/hour/day/month/year），
-- 每档各自 enabled/step。新口径：
--   * 时/日/月/年合并为一行 granularity='snapshot'，一个开关统管四档；默认关闭；
--   * 分钟档保留独立开关与倍数步长；
--   * 秒档不再写入，snapshot_second 删除；配置行删除。
--
-- 存量数据：snapshot_coarse / snapshot_minute 保持不变（关闭开关只停写，不清历史）。
--
-- 不可逆：drop table snapshot_second（按产品要求不备份）。升级后
-- /admin/v1/readonly/snapshot-second 返回 404。

-- 1) 先摘掉旧约束（granularity 闭集、步长白名单、enabled 需要 step），稍后按新口径重建。
do $$
declare c record;
begin
    for c in
        select conname
        from pg_constraint
        where conrelid = 'snapshot_slot_config'::regclass and contype = 'c'
    loop
        execute format('alter table snapshot_slot_config drop constraint %I', c.conname);
    end loop;
end $$;

-- 2) 写入合并后的快照行（默认关闭），保留分钟档原有配置。
insert into snapshot_slot_config (granularity, enabled, step)
values ('snapshot', false, 1)
on conflict (granularity) do update set enabled = false, step = 1;

delete from snapshot_slot_config
where granularity in ('second', 'hour', 'day', 'month', 'year');

-- 3) 新约束：只有 snapshot / minute 两行；步长按档位收敛；分钟档启用时必须给步长。
alter table snapshot_slot_config
    add constraint snapshot_slot_config_granularity
        check (granularity in ('snapshot', 'minute')),
    add constraint snapshot_slot_config_step
        check (
            (granularity = 'snapshot' and (step is null or step = 1))
            or (granularity = 'minute' and (step is null or step in (1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30)))
        ),
    add constraint snapshot_slot_config_enabled_step
        check (granularity <> 'minute' or not enabled or step is not null);

-- 4) 秒档下线：表与保留期配置一并移除（分区随父表删除）。
drop table if exists snapshot_second;
delete from snapshot_settings where table_name = 'snapshot_second';
