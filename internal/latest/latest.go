// Package latest 负责最新值缓存（主键 terminal_id + item_code）。
package latest

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/RenJieWeia/iot-admin/internal/store"
)

// Upsert 批量写入一条消息的全部数据项：value_time 更大才覆盖（NTP 回拨的数据不覆盖）。
// 返回真正写入的数据项集合；不在集合里的表示已有更新的值。
// 一条消息一个语句，避免逐项往返（每条消息 15 项时可省下 14 次 round trip）。
func Upsert(ctx context.Context, tx pgx.Tx, vs []store.Value) (map[string]struct{}, error) {
	written := make(map[string]struct{}, len(vs))
	if len(vs) == 0 {
		return written, nil
	}
	placeholders := make([]string, len(vs))
	args := make([]any, 0, len(vs)*8)
	for i, v := range vs {
		n := i * 8
		placeholders[i] = fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, now())",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8)
		args = append(args, v.TerminalID, v.ItemCode, v.ValueNum, v.ValueText, v.Unit, v.ValueTime,
			v.Quality, v.QualityReason)
	}
	// 质量标记跟随"胜出"的那一行：value_time 更新时才覆盖，避免旧数据的质量位盖掉新数据。
	rows, err := tx.Query(ctx, `
		insert into latest_value
		    (terminal_id, item_code, value_num, value_text, unit, value_time, quality, quality_reason, updated_at)
		values `+strings.Join(placeholders, ", ")+`
		on conflict (terminal_id, item_code) do update
		set value_num = excluded.value_num, value_text = excluded.value_text,
		    unit = excluded.unit, value_time = excluded.value_time,
		    quality = excluded.quality, quality_reason = excluded.quality_reason, updated_at = now()
		where excluded.value_time > latest_value.value_time
		returning item_code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		written[code] = struct{}{}
	}
	return written, rows.Err()
}
