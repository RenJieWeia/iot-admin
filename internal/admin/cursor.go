package admin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 快照游标分页的限额与游标编解码：从已删除的 changes.go 迁入，仅服务只读页的游标分页。
//
// 快照变更查询端点已下线（本文件不含任何 handler）；只读快照页仍在用同一套
// (settled_at, terminal_id, item_code, hour_start) 游标，因此把共用部分原样搬到这里。
// 迁移是零行为变化：编码格式、base64 变体与错误语义逐字未改。
// 符号名保留 Changes* 前缀，避免无意义重命名扩散到 readonly.go 与其测试。

const (
	// ChangesDefaultLimit 是游标页的默认条数。
	ChangesDefaultLimit = 200
	// ChangesMaxLimit 是游标页的上限（超出按上限截断）。
	ChangesMaxLimit = 1000
)

// changesCursor 是游标解码后的位置（settled_at + 主键三列）。
type changesCursor struct {
	SettledAt  time.Time
	TerminalID string
	ItemCode   string
	HourStart  time.Time
}

// changesCursorLayout 是游标里时间的编码格式（固定纳秒精度，避免精度丢失导致跳行）。
const changesCursorLayout = time.RFC3339Nano

func encodeChangesCursor(c changesCursor) string {
	raw := strings.Join([]string{
		c.SettledAt.UTC().Format(changesCursorLayout),
		c.TerminalID,
		c.ItemCode,
		c.HourStart.UTC().Format(changesCursorLayout),
	}, "\x1f")
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeChangesCursor(s string) (changesCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return changesCursor{}, errors.New("不是合法的 base64url")
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 4 {
		return changesCursor{}, errors.New("字段数不符")
	}
	settled, err := time.Parse(changesCursorLayout, parts[0])
	if err != nil {
		return changesCursor{}, fmt.Errorf("settled_at: %w", err)
	}
	hour, err := time.Parse(changesCursorLayout, parts[3])
	if err != nil {
		return changesCursor{}, fmt.Errorf("hour_start: %w", err)
	}
	if parts[1] == "" || parts[2] == "" {
		return changesCursor{}, errors.New("主键为空")
	}
	return changesCursor{SettledAt: settled, TerminalID: parts[1], ItemCode: parts[2], HourStart: hour}, nil
}
