package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// 分区续建的前瞻窗口（与迁移 0010 的初始窗口衔接；重复创建是幂等的）。
const (
	partitionLeadMonths = 3
	partitionLeadDays   = 8
)

// EnsurePartitions 给三张快照表续建未来分区：粗表/分表按月、秒表按日。
// 开机跑一次 + 每日 ticker；失败由调用方只记日志告警（缺分区时插入失败 = 平台故障，可重传）。
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	monthStart := SegmentStart(now, Month, 1)
	for i := 0; i <= partitionLeadMonths; i++ {
		start := SegmentEnd(monthStart, Month, i)
		end := SegmentEnd(start, Month, 1)
		for _, parent := range []string{"snapshot_coarse", "snapshot_minute"} {
			if err := createPartition(ctx, pool, parent, parent+"_"+start.Format("200601"), start, end); err != nil {
				return err
			}
		}
	}
	dayStart := SegmentStart(now, Day, 1)
	for i := 0; i <= partitionLeadDays; i++ {
		start := dayStart.AddDate(0, 0, i)
		end := start.AddDate(0, 0, 1)
		if err := createPartition(ctx, pool, "snapshot_second", "snapshot_second_"+start.Format("20060102"), start, end); err != nil {
			return err
		}
	}
	return nil
}

// createPartition 建一个分区；名字与边界都由代码生成（无外部输入拼接）。
func createPartition(ctx context.Context, pool *pgxpool.Pool, parent, name string, start, end time.Time) error {
	stmt := fmt.Sprintf(
		"create table if not exists %s partition of %s for values from ('%s') to ('%s')",
		name, parent, start.Format(time.RFC3339), end.Format(time.RFC3339))
	_, err := pool.Exec(ctx, stmt)
	return err
}

// PurgeExpired 按 snapshot_settings.retention_days 清理过期分区：空值 = 永久保留。
// 粗/分表整月 DROP、秒表整日 DROP；cutoff 之前的整段分区才删。
func PurgeExpired(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	rows, err := pool.Query(ctx, `select table_name, coalesce(retention_days, 0) from snapshot_settings`)
	if err != nil {
		return err
	}
	type setting struct {
		table string
		days  int
	}
	var settings []setting
	for rows.Next() {
		var s setting
		if err := rows.Scan(&s.table, &s.days); err != nil {
			rows.Close()
			return err
		}
		settings = append(settings, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	today := SegmentStart(now, Day, 1)
	for _, s := range settings {
		if s.days <= 0 {
			continue // 空 = 永久保留
		}
		if err := dropExpired(ctx, pool, s.table, today.AddDate(0, 0, -s.days)); err != nil {
			return err
		}
	}
	return nil
}

// dropExpired 删除右边界 <= cutoff 的分区。
func dropExpired(ctx context.Context, pool *pgxpool.Pool, parent string, cutoff time.Time) error {
	names, err := listPartitions(ctx, pool, parent)
	if err != nil {
		return err
	}
	for _, name := range names {
		upper, ok := partitionUpper(name)
		if !ok || upper.After(cutoff) {
			continue
		}
		if _, err := pool.Exec(ctx, "drop table if exists "+name); err != nil {
			return err
		}
	}
	return nil
}

// listPartitions 列出某父表的所有分区名。
func listPartitions(ctx context.Context, pool *pgxpool.Pool, parent string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		select c.relname
		from pg_inherits i
		join pg_class p on p.oid = i.inhparent
		join pg_class c on c.oid = i.inhrelid
		where p.relname = $1`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// partitionUpper 解析分区名的日期后缀（YYYYMM 或 YYYYMMDD），返回右边界（本地时间）。
func partitionUpper(name string) (time.Time, bool) {
	parts := strings.Split(name, "_")
	suffix := parts[len(parts)-1]
	switch len(suffix) {
	case 6:
		t, err := time.ParseInLocation("200601", suffix, staging.Zone)
		if err != nil {
			return time.Time{}, false
		}
		return t.AddDate(0, 1, 0), true
	case 8:
		t, err := time.ParseInLocation("20060102", suffix, staging.Zone)
		if err != nil {
			return time.Time{}, false
		}
		return t.AddDate(0, 0, 1), true
	}
	return time.Time{}, false
}
