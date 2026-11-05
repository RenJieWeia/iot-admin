-- 新增事件型异常 terminal_unknown（终端未注册或已停用）。
-- 先 drop 旧约束再 add 新约束，保证可重复执行；migrations 0001-0003 保持不动。
alter table exception_count
    drop constraint if exists exception_count_error_type_check;

alter table exception_count
    add constraint exception_count_error_type_check check (error_type in (
        'upload_timeout', 'protocol_error', 'msg_incomplete', 'clean_failed',
        'cache_write_failed', 'mqtt_publish_failed', 'snapshot_failed',
        'late_data', 'quality_violation', 'terminal_unknown'
    ));
