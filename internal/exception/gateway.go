package exception

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// GatewayQuerier 只取读取网关清单需要的 PG 能力。
type GatewayQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// RegisteredGateways 读取注册表中出现过的网关标识（去重、忽略空值）。
func RegisteredGateways(ctx context.Context, q GatewayQuerier) ([]string, error) {
	rows, err := q.Query(ctx, `select distinct gateway_id from terminal where gateway_id <> '' order by gateway_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var gw string
		if err := rows.Scan(&gw); err != nil {
			return nil, err
		}
		out = append(out, gw)
	}
	return out, rows.Err()
}

// MarkGatewaysDisconnected 记录网关断连（状态型异常）。
// 对象是网关而不是终端：terminal_id 与 gateway_id 都存网关标识，item_code 存空串，
// 这样 EXCLUDE 的冲突域恰好是"同一网关不能同时存在两条未结束断连"。
func (r *Recorder) MarkGatewaysDisconnected(ctx context.Context, q GatewayQuerier) error {
	gateways, err := RegisteredGateways(ctx, q)
	if err != nil {
		return err
	}
	empty := ""
	for _, gw := range gateways {
		if err := r.OpenInterval(ctx, GatewayDisconnected, gw, gw, &empty); err != nil {
			return err
		}
	}
	return nil
}

// MarkGatewaysConnected 关闭所有未结束的断连区间（重连成功即结束，不留 upper_inf）。
func (r *Recorder) MarkGatewaysConnected(ctx context.Context) error {
	_, err := r.Pool.Exec(ctx, `
		update exception_interval
		set period = tstzrange(lower(period), $1)
		where error_type = $2 and upper_inf(period)`, r.Now(), string(GatewayDisconnected))
	return err
}
