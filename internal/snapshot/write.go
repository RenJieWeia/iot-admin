package snapshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Write 把快照行写入三张表（粗表承接时/日/月/年，分表与秒表各一档）。
//
// 事务边界由调用方控制（pipeline 在 T1 提交 latest_value 之后再开 T2）。
// 覆盖语义：只有 excluded.value_time 更大才覆盖并 revision+1 —— 同值重发与更早的回拨都不动行，
// 与 latest_value 的 value_time 守卫同一口径。
func Write(ctx context.Context, tx pgx.Tx, rows []Row) error {
	var coarse, minute, second []Row
	for _, r := range rows {
		switch r.Granularity {
		case Hour, Day, Month, Year:
			coarse = append(coarse, r)
		case Minute:
			minute = append(minute, r)
		case Second:
			second = append(second, r)
		}
	}
	if err := writeCoarse(ctx, tx, coarse); err != nil {
		return err
	}
	if err := writeSimple(ctx, tx, "snapshot_minute", minute); err != nil {
		return err
	}
	return writeSimple(ctx, tx, "snapshot_second", second)
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
