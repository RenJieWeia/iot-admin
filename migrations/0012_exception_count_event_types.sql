-- 事件型异常闭集：把 0004 之后就再没更新过的 CHECK 补到当前枚举。
--
-- 背景：0004 之后 Go 侧 EventTypes 多次变更（0009 删了 upload_timeout / snapshot_failed /
-- late_data；此后又新增 snapshot_write_failed / snapshot_late_dropped），但 exception_count
-- 的 CHECK 一直停在 0004 的十项。后果是：新类型插入时违反 CHECK，Recorder.Event 只打一条
-- error 日志、事件永远落不了库——"快照写失败/迟到丢弃"的可观测性从落地起就是哑的。
--
-- 为什么 CHECK 里保留三个已下线的类型（upload_timeout / snapshot_failed / late_data）：
--   1) 库里已有这些类型的历史行（它们记录的是当时真实发生过的观测），加严格 CHECK 会因
--      存量行校验失败而整条迁移失败；
--   2) 直接删历史行属于数据删除，不在「迁移只新增结构」的口径内，且会抹掉审计线索。
--   因此 CHECK 取「当前 9 类 + 3 类历史值」的并集：**写侧以 Go 的 EventTypes 为准**
--   （Recorder 不会产生这三个值），CHECK 只保证"历史行仍合法、新类型能写进去"。
--   回归见 internal/admin 的 TestEventTypeCheckCoversEventTypes（EventTypes ⊆ CHECK）。

alter table exception_count
    drop constraint if exists exception_count_error_type_check;

alter table exception_count
    add constraint exception_count_error_type_check check (error_type in (
        -- 当前事件型（与 internal/exception/errors.go 的 EventTypes 一致）
        'protocol_error', 'msg_incomplete', 'clean_failed',
        'cache_write_failed', 'mqtt_publish_failed', 'quality_violation',
        'terminal_unknown',
        'snapshot_write_failed', 'snapshot_late_dropped',
        -- 历史值：能力已下线，仅保留兼容存量行，Go 侧不再产生
        'upload_timeout', 'snapshot_failed', 'late_data'
    ));
