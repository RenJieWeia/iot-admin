// Package staging 负责小时归属计算与 hourly_staging 写入。
package staging

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/RenJieWeia/iot-admin/internal/store"
)

// Zone 固定 Asia/Shanghai（无夏令时），不依赖运行环境是否装了 tzdata。
var Zone = time.FixedZone("Asia/Shanghai", 8*60*60)

// DefaultWindowSeconds 默认结算窗口：小时结束后 15 分钟。
const DefaultWindowSeconds = 900

// HourStart 把采集时间取整到所属自然小时（Asia/Shanghai）。
func HourStart(t time.Time) time.Time {
	t = t.In(Zone)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, Zone)
}

// CloseAt 返回小时 H 的结算关闭时刻：H + 1h + window。闭区间语义：
// 平台接收时刻 >= 关闭时刻即"窗口外"（判为迟到），结算扫描用同一比较（<= now 可结算）。
func CloseAt(hourStart time.Time, windowSeconds int) time.Time {
	if windowSeconds <= 0 {
		windowSeconds = DefaultWindowSeconds
	}
	return hourStart.Add(time.Hour + time.Duration(windowSeconds)*time.Second)
}

// InWindow 判定平台接收时刻现在是否仍在窗口内。
func InWindow(hourStart time.Time, windowSeconds int, now time.Time) bool {
	return now.Before(CloseAt(hourStart, windowSeconds))
}

// Row 是暂存行：携带归属小时。
type Row struct {
	store.Value
	HourStart time.Time
}

// Upsert 批量写入暂存：同 (terminal,item,hour) 按 value_time 大者覆盖，否则忽略。
func Upsert(ctx context.Context, tx pgx.Tx, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	placeholders := make([]string, len(rows))
	args := make([]any, 0, len(rows)*9)
	for i, r := range rows {
		n := i * 9
		placeholders[i] = fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, now())",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9)
		args = append(args, r.TerminalID, r.ItemCode, r.HourStart,
			r.ValueNum, r.ValueText, r.Unit, r.ValueTime, r.Quality, r.QualityReason)
	}
	// 质量标记与 value_time 同步覆盖（R-022）：同一小时同一项以最后到达的为准。
	_, err := tx.Exec(ctx, `
		insert into hourly_staging
		    (terminal_id, item_code, hour_start, value_num, value_text, unit, value_time,
		     quality, quality_reason, updated_at)
		values `+strings.Join(placeholders, ", ")+`
		on conflict (terminal_id, item_code, hour_start) do update
		set value_num = excluded.value_num, value_text = excluded.value_text,
		    unit = excluded.unit, value_time = excluded.value_time,
		    quality = excluded.quality, quality_reason = excluded.quality_reason, updated_at = now()
		where excluded.value_time > hourly_staging.value_time`, args...)
	return err
}
