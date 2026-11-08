package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Write 把快照行写入两张表（粗表承接时/日/月/年，分表一档；秒表已随 0017 下线）。
//
// 事务边界由调用方控制（pipeline 在 T1 提交 latest_value 之后再开 T2）。
// 覆盖语义：只有 excluded.value_time 更大才覆盖并 revision+1 —— 同值重发与更早的回拨都不动行，
// 与 latest_value 的 value_time 守卫同一口径。
func Write(ctx context.Context, tx pgx.Tx, rows []Row) error {
	rows = collapseRows(rows)
	var coarse, minute []Row
	for _, r := range rows {
		if r.Granularity == Minute {
			minute = append(minute, r)
			continue
		}
		coarse = append(coarse, r)
	}
	if err := writeCoarse(ctx, tx, coarse); err != nil {
		return err
	}
	return writeSimple(ctx, tx, "snapshot_minute", minute)
}

// rowKey 是快照行的自然键（粗表含 granularity，分/秒表同形）。
type rowKey struct {
	terminalID   string
	itemCode     string
	granularity  Granularity
	segmentStart time.Time
}

// collapseRows 按自然键把同一格的多行收敛成一行：保留 value_time 最大的那一行。
//
// 为什么必须有：类型级与实例级绑定同时命中同一序列时（并集 = 真），一旦调用方按
// "类型级一轮 + 实例级一轮"展开就会生成两组同主键行，PG 会在同一条 INSERT 里报
// `cannot affect row a second time`。在这里收敛既保命又保语义：
// 只写一次，revision 也不会因为"并集命中"多 +1。
// 注意：这是"同一格只写一行"的主键收敛，与上报消息的静默合并无关。
func collapseRows(rows []Row) []Row {
	if len(rows) < 2 {
		return rows
	}
	seen := make(map[rowKey]int, len(rows))
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		k := rowKey{r.TerminalID, r.ItemCode, r.Granularity, r.SegmentStart}
		if i, ok := seen[k]; ok {
			if r.ValueTime.After(out[i].ValueTime) {
				out[i] = r
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, r)
	}
	return out
}

func writeCoarse(ctx context.Context, tx pgx.Tx, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	placeholders := make([]string, len(rows))
	args := make([]any, 0, len(rows)*10)
	for i, r := range rows {
		n := i * 10
		placeholders[i] = fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9, n+10)
		args = append(args, r.TerminalID, r.ItemCode, string(r.Granularity), r.SegmentStart,
			r.ValueNum, r.ValueText, r.Unit, r.ValueTime, r.Quality, r.QualityReason)
	}
	_, err := tx.Exec(ctx, `
		insert into snapshot_coarse
		    (terminal_id, item_code, granularity, segment_start, value_num, value_text, unit,
		     value_time, quality, quality_reason)
		values `+strings.Join(placeholders, ", ")+`
		on conflict (terminal_id, item_code, granularity, segment_start) do update
		set value_num = excluded.value_num, value_text = excluded.value_text, unit = excluded.unit,
		    value_time = excluded.value_time, quality = excluded.quality,
		    quality_reason = excluded.quality_reason, updated_at = now(),
		    revision = snapshot_coarse.revision + 1
		where excluded.value_time > snapshot_coarse.value_time`, args...)
	return err
}

// writeSimple 写分表/秒表（表名是包内常量，不接受外部输入）。
func writeSimple(ctx context.Context, tx pgx.Tx, table string, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	placeholders := make([]string, len(rows))
	args := make([]any, 0, len(rows)*9)
	for i, r := range rows {
		n := i * 9
		placeholders[i] = fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9)
		args = append(args, r.TerminalID, r.ItemCode, r.SegmentStart,
			r.ValueNum, r.ValueText, r.Unit, r.ValueTime, r.Quality, r.QualityReason)
	}
	_, err := tx.Exec(ctx, `
		insert into `+table+`
		    (terminal_id, item_code, segment_start, value_num, value_text, unit,
		     value_time, quality, quality_reason)
		values `+strings.Join(placeholders, ", ")+`
		on conflict (terminal_id, item_code, segment_start) do update
		set value_num = excluded.value_num, value_text = excluded.value_text, unit = excluded.unit,
		    value_time = excluded.value_time, quality = excluded.quality,
		    quality_reason = excluded.quality_reason, updated_at = now(),
		    revision = `+table+`.revision + 1
		where excluded.value_time > `+table+`.value_time`, args...)
	return err
}
