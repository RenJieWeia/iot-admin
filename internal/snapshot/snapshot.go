// Package snapshot 提供六档自然时间快照的白名单、段起点对齐与写入。
//
// 口径（与 migrations/0010 一致）：
//   - 段起点一律按 Asia/Shanghai 本地自然时间对齐；秒/分/时按 step 对齐到本地时间轴，
//     日 = 本地 0 点，月 = 1 日 0 点（step 按自然季度/半年/年分组），年 = 1 月 1 日；
//   - 每档独立判迟到：段结束（段起点 + step×单位）+ 15 分钟宽限；
//   - 覆盖写只认「采集时间更新」：excluded.value_time > 目标行 value_time 才覆盖并 revision+1。
package snapshot

import (
	"fmt"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/staging"
)

// Granularity 是六档粒度；值直接落库（snapshot_coarse.granularity 的 CHECK 闭集）。
type Granularity string

const (
	Second Granularity = "second"
	Minute Granularity = "minute"
	Hour   Granularity = "hour"
	Day    Granularity = "day"
	Month  Granularity = "month"
	Year   Granularity = "year"
)

// AllGranularities 是固定顺序（写入与测试都按它遍历，避免 map 随机序）。
var AllGranularities = []Granularity{Second, Minute, Hour, Day, Month, Year}

// GracePeriod 是每档的迟到宽限：段结束 + 15 分钟内仍可写。
const GracePeriod = 15 * time.Minute

// 步长白名单：必须与 migrations/0010 的 CHECK 约束逐项一致（DB 是最后一道，服务层先返回 400）。
var stepWhitelist = map[Granularity][]int{
	Second: {1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30},
	Minute: {1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30},
	Hour:   {1, 2, 3, 4, 6, 8, 12},
	Day:    {1},
	Month:  {1, 2, 3, 4, 6, 12},
	Year:   {1},
}

// Validate 校验某档的步长是否在白名单内。管理 API 写入前调用（DB CHECK 兜底）。
// 参数顺序与任务书一致：Validate(step, granularity)。
func Validate(step int, g Granularity) error {
	allowed, ok := stepWhitelist[g]
	if !ok {
		return fmt.Errorf("未知粒度 %q", g)
	}
	for _, v := range allowed {
		if v == step {
			return nil
		}
	}
	return fmt.Errorf("%s 档的步长 %d 不在白名单 %v 内", g, step, allowed)
}

// Slot 是某一档的配置：是否启用 + 步长（步长的单位随档位：秒/分/时/日/月/年）。
type Slot struct {
	Enabled bool
	Step    int
}

// Plan 是一个序列（设备类型 + 数据项）的六档配置。
type Plan struct {
	TypeID   int64
	ItemCode string
	Slots    map[Granularity]Slot
}

// Slot 取某档配置；未配置视为未启用。
func (p Plan) Slot(g Granularity) Slot { return p.Slots[g] }

// Configured 表示该序列至少启用了一档（未配置的序列零写入）。
func (p Plan) Configured() bool {
	for _, g := range AllGranularities {
		if s := p.Slots[g]; s.Enabled && s.Step > 0 {
			return true
		}
	}
	return false
}

// Value 是一个待写快照的清洗结果（与 store.Value 同形，避免包间耦合）。
type Value struct {
	TerminalID    string
	ItemCode      string
	Num           *float64
	Text          *string
	Unit          string
	Time          time.Time
	Quality       int16
	QualityReason string
}

// Row 是一行快照写入（Granularity 仅粗表使用，分表/秒表各自一档）。
type Row struct {
	TerminalID    string
	ItemCode      string
	Granularity   Granularity
	SegmentStart  time.Time
	ValueNum      *float64
	ValueText     *string
	Unit          string
	ValueTime     time.Time
	Quality       int16
	QualityReason string
}

// SegmentStart 计算 t 所属段的起点（本地时区对齐）。
//
// 秒/分/时：按「本地当天已过的秒/分/时」向下取整到 step 的倍数（step 均整除 60/24，
// 因此 2 小时档落在偶数小时、2 分钟档落在偶数分钟，不会因为 UTC 对齐而整体偏 8 小时）。
// 月：按「本年第几个月」0-based 向下取整到 step 的倍数 —— step=3 落在 1/4/7/10 月。
func SegmentStart(t time.Time, g Granularity, step int) time.Time {
	if step < 1 {
		step = 1
	}
	lt := t.In(staging.Zone)
	switch g {
	case Second:
		secOfDay := lt.Hour()*3600 + lt.Minute()*60 + lt.Second()
		base := secOfDay - secOfDay%step
		return time.Date(lt.Year(), lt.Month(), lt.Day(), base/3600, base%3600/60, base%60, 0, staging.Zone)
	case Minute:
		minOfDay := lt.Hour()*60 + lt.Minute()
		base := minOfDay - minOfDay%step
		return time.Date(lt.Year(), lt.Month(), lt.Day(), base/60, base%60, 0, 0, staging.Zone)
	case Hour:
		base := lt.Hour() - lt.Hour()%step
		return time.Date(lt.Year(), lt.Month(), lt.Day(), base, 0, 0, 0, staging.Zone)
	case Day:
		return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, staging.Zone)
	case Month:
		idx := int(lt.Month()) - 1
		start := idx - idx%step
		return time.Date(lt.Year(), time.Month(start+1), 1, 0, 0, 0, 0, staging.Zone)
	case Year:
		return time.Date(lt.Year(), 1, 1, 0, 0, 0, 0, staging.Zone)
	default:
		return lt
	}
}

// SegmentEnd 计算段结束（= 段起点 + step×单位）；月/年按日历加法。
func SegmentEnd(start time.Time, g Granularity, step int) time.Time {
	if step < 1 {
		step = 1
	}
	switch g {
	case Second:
		return start.Add(time.Duration(step) * time.Second)
	case Minute:
		return start.Add(time.Duration(step) * time.Minute)
	case Hour:
		return start.Add(time.Duration(step) * time.Hour)
	case Day:
		return start.AddDate(0, 0, step)
	case Month:
		return start.AddDate(0, step, 0)
	case Year:
		return start.AddDate(step, 0, 0)
	default:
		return start
	}
}

// Writable 判断某档在 now 时刻是否还能写：now < 段结束 + 15 分钟。
// 每档独立判断：同一条数据可能进粗档、进不了秒档（已接受的档间不一致）。
func Writable(now, start time.Time, g Granularity, step int) bool {
	return now.Before(SegmentEnd(start, g, step).Add(GracePeriod))
}

// Rows 按本序列的六档配置，把一条清洗结果展开成可写快照行：
// 未启用的档不出行，迟到（段结束 + 15 分钟已过）的档单独跳过，其余档照写。
func (p Plan) Rows(now time.Time, v Value) []Row {
	var out []Row
	for _, g := range AllGranularities {
		slot := p.Slots[g]
		if !slot.Enabled || slot.Step < 1 {
			continue
		}
		start := SegmentStart(v.Time, g, slot.Step)
		if !Writable(now, start, g, slot.Step) {
			continue
		}
		out = append(out, Row{
			TerminalID: v.TerminalID, ItemCode: v.ItemCode,
			Granularity: g, SegmentStart: start,
			ValueNum: v.Num, ValueText: v.Text, Unit: v.Unit, ValueTime: v.Time,
			Quality: v.Quality, QualityReason: v.QualityReason,
		})
	}
	return out
}
