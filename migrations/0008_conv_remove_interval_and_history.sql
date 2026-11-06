-- 收敛：下线两处半截能力（不改任何保留表的结构）。
--
-- A. 状态型异常区间 exception_interval
--    零生产写入路径：internal/exception 有开启/抖动合并/闭合三处写入方法，但主链路从不调用，
--    后台「异常区间」页因此永远为空。随表删除 3 个状态型枚举（gateway_disconnected /
--    auth_failed / item_silent）与 btree_gist 扩展。
--
-- B. 补录留痕 snapshot_cell_history
--    只在「补录写快照」时追加版本行，从来没有读取入口。**仅写入侧下线**：
--    快照变更查询接口（GET /admin/v1/snapshot/changes）暂留，待外部调用方确认后另行撤除；
--    该接口只读 hourly_snapshot，不依赖本表，故删表不影响它。
--    补录本身的行为不变：数据仍进 hourly_snapshot、revision 仍自增（revision 不是留痕）。
--
-- 扩展下线前的确认：btree_gist 仅被 0003 的 exception_interval_no_overlap 排除约束使用
-- （grep "btree_gist\|using gist" migrations/ 仅 0003 两处），该约束随表一并消失，故扩展可删；
-- 其余表（含 hourly_snapshot 及其分区）不使用任何 gist 索引。
--
-- 不可逆：删除前须确认 exception_interval 为空（本仓库环境实测 0 行；
-- snapshot_cell_history 的历史行按需求「追溯放弃」一并删除）。

drop table if exists exception_interval;
drop table if exists snapshot_cell_history;

drop extension if exists btree_gist;
