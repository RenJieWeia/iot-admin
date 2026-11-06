package settle

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// RetentionMonths 快照保留 24 个月：切到新月份后 DROP 最老分区（过期 = 物理消失）。
const RetentionMonths = 24

const partitionPrefix = "hourly_snapshot_"

func monthStart(t time.Time) time.Time {
	t = t.In(staging.Zone)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, staging.Zone)
}

func partitionName(m time.Time) string {
	return partitionPrefix + m.Format("200601")
}

func tsLiteral(t time.Time) string {
	return t.Format("2006-01-02 15:04:05.000000-07:00")
}

// EnsurePartitions 预建 now 前 1 个月到后 2 个月的分区，并 DROP 超出保留期的老分区。
// 幂等：可重复执行，不依赖外部调度器状态。
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	base := monthStart(now)
	for i := -1; i <= 2; i++ {
		start := base.AddDate(0, i, 0)
		end := start.AddDate(0, 1, 0)
		_, err := pool.Exec(ctx, fmt.Sprintf(
			`create table if not exists %s partition of hourly_snapshot for values from ('%s') to ('%s')`,
			partitionName(start), tsLiteral(start), tsLiteral(end)))
		if err != nil {
			return fmt.Errorf("建分区 %s: %w", partitionName(start), err)
		}
	}
	return dropExpired(ctx, pool, now)
}

func dropExpired(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	cutoff := monthStart(now).AddDate(0, -RetentionMonths, 0)
	// 过期分区直接 DROP（物理消失），没有独立的行级清理步骤。
	rows, err := pool.Query(ctx, `
		select c.relname from pg_inherits i
		join pg_class c on c.oid = i.inhrelid
		where i.inhparent = 'hourly_snapshot'::regclass`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		m, ok := parsePartitionMonth(name)
		if !ok {
			continue // 非本代码创建的分区不碰
		}
		if !m.AddDate(0, 1, 0).After(cutoff) { // 分区上界 <= 保留截止月
			if _, err := pool.Exec(ctx, "drop table if exists "+name); err != nil {
				return fmt.Errorf("DROP 过期分区 %s: %w", name, err)
			}
		}
	}
	return nil
}

func parsePartitionMonth(name string) (time.Time, bool) {
	suffix, ok := strings.CutPrefix(name, partitionPrefix)
	if !ok || len(suffix) != 6 {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(suffix[:4])
	if err != nil {
		return time.Time{}, false
	}
	month, err := strconv.Atoi(suffix[4:])
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, staging.Zone), true
}
