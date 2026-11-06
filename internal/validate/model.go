package validate

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTerminalUnknown 用于"终端不存在或停用"这一类拒收（记 terminal_unknown）。
var ErrTerminalUnknown = errors.New("terminal_unavailable")

// Terminal 是校验与清洗需要的终端上下文（含实时主题要用的 domain/type 编码）。
type Terminal struct {
	ID         string
	TypeID     int64
	TypeCode   string
	DomainID   int64
	DomainCode string
	GatewayID  string
	Enabled    bool
}

// Property 是物模型属性声明。
type Property struct {
	ItemCode  string
	Name      string
	ValueType string // number | string | bool
	Unit      string
	Precision *int
	// Required 是 thing_model_property.required 列的透传值：R-021 后**不参与**接入校验
	// （全部声明字段都必填），保留字段是为了元数据/前端提示使用。
	Required bool
	Min      *float64
	Max      *float64
}

// Registry 按需从 PG 读取终端与物模型。
// shortcut: 每条消息两次元数据查询，未加进程内缓存；元数据读成为吞吐上限时再加缓存。
type Registry struct {
	Pool *pgxpool.Pool
}

func NewRegistry(pool *pgxpool.Pool) *Registry { return &Registry{Pool: pool} }

// LoadTerminal 读取终端上下文；不存在返回 ErrTerminalUnknown，停用也返回 ErrTerminalUnknown。
func (r *Registry) LoadTerminal(ctx context.Context, terminalID string) (Terminal, error) {
	var t Terminal
	err := r.Pool.QueryRow(ctx, `
		select t.id, t.type_id, ty.code, d.id, d.code, t.gateway_id, t.enabled
		from terminal t
		join device_type ty on ty.id = t.type_id
		join data_domain d on d.id = ty.domain_id
		where t.id = $1`, terminalID).
		Scan(&t.ID, &t.TypeID, &t.TypeCode, &t.DomainID, &t.DomainCode, &t.GatewayID, &t.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Terminal{}, ErrTerminalUnknown
	}
	if err != nil {
		return Terminal{}, err
	}
	if !t.Enabled {
		return Terminal{}, ErrTerminalUnknown
	}
	return t, nil
}

// LoadProperties 读取终端类型的全部物模型属性。
func (r *Registry) LoadProperties(ctx context.Context, typeID int64) ([]Property, error) {
	rows, err := r.Pool.Query(ctx, `
		select item_code, name, value_type, coalesce(unit, ''), precision, required, min_value, max_value
		from thing_model_property where type_id = $1 order by item_code`, typeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Property
	for rows.Next() {
		var p Property
		if err := rows.Scan(&p.ItemCode, &p.Name, &p.ValueType, &p.Unit,
			&p.Precision, &p.Required, &p.Min, &p.Max); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MissingRequired 返回未覆盖的属性 code（最多 MaxMissingCodes 个）。
//
// R-021：物模型声明的**全部** item_code 必须全覆盖，缺任一即 msg_incomplete；
// thing_model_property.required 列不再参与校验（退化为前端提示），Property.Required
// 因此只作为元数据透传，这里刻意不读它。
func MissingRequired(props []Property, items []RawItem) []string {
	have := make(map[string]struct{}, len(items))
	for _, it := range items {
		have[it.Code] = struct{}{}
	}
	var missing []string
	for _, p := range props {
		if _, ok := have[p.ItemCode]; !ok {
			missing = append(missing, p.ItemCode)
			if len(missing) == MaxMissingCodes {
				break
			}
		}
	}
	return missing
}

// MaxClockSkew 采集时间允许的超前量。
const MaxClockSkew = 5 * time.Minute

// FindFuture 返回第一个超前平台时间 5 分钟以上的数据项；ok=false 表示无超前。
func FindFuture(items []RawItem, now time.Time) (RawItem, bool) {
	limit := now.Add(MaxClockSkew)
	for _, it := range items {
		if it.ParsedTime.After(limit) {
			return it, true
		}
	}
	return RawItem{}, false
}
