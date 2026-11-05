package clean

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// RuleType 是质量规则的闭集，与 migrations/0001 的 check 约束一致，禁止扩展。
type RuleType string

const (
	RuleRange         RuleType = "range"
	RuleEnum          RuleType = "enum"
	RulePrecision     RuleType = "precision"
	RuleUnitWhitelist RuleType = "unit_whitelist"
	RuleNotNull       RuleType = "not_null"
)

// RuleTypes 闭集清单（管理 API 校验用）。
var RuleTypes = []RuleType{RuleRange, RuleEnum, RulePrecision, RuleUnitWhitelist, RuleNotNull}

func ValidRuleType(s string) bool {
	for _, r := range RuleTypes {
		if string(r) == s {
			return true
		}
	}
	return false
}

// Rule 是绑定到终端实例的质量规则。
type Rule struct {
	ItemCode string
	Type     RuleType
	Params   json.RawMessage
}

// Violation 是一条质量违规，只用于计数，不影响入库。
type Violation struct {
	RuleType RuleType
	Detail   string
}

type ruleParams struct {
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	Scale  *int     `json:"scale"`
	Values []any    `json:"values"`
	Units  []string `json:"units"`
}

// Querier 只取 rules 需要的 PG 能力，便于测试替身。
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// LoadRules 读取终端启用的质量规则，按 item_code 分组。
func LoadRules(ctx context.Context, q Querier, terminalID string) (map[string][]Rule, error) {
	rows, err := q.Query(ctx, `
		select item_code, rule_type, params from quality_rule
		where terminal_id = $1 and enabled order by item_code`, terminalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ItemCode, &r.Type, &r.Params); err != nil {
			return nil, err
		}
		out[r.ItemCode] = append(out[r.ItemCode], r)
	}
	return out, rows.Err()
}

// Verify 对清洗后的值执行规则，返回命中项（命中不拒收）。
func Verify(rules []Rule, out Out) []Violation {
	var vs []Violation
	for _, r := range rules {
		var p ruleParams
		if len(r.Params) > 0 {
			if err := json.Unmarshal(r.Params, &p); err != nil {
				vs = append(vs, Violation{RuleType: r.Type, Detail: fmt.Sprintf("params 不可解析: %v", err)})
				continue
			}
		}
		switch r.Type {
		case RuleRange:
			if out.Number == nil {
				continue
			}
			if p.Min != nil && *out.Number < *p.Min {
				vs = append(vs, Violation{RuleType: r.Type, Detail: "低于下限"})
			} else if p.Max != nil && *out.Number > *p.Max {
				vs = append(vs, Violation{RuleType: r.Type, Detail: "高于上限"})
			}
		case RuleEnum:
			if len(p.Values) == 0 {
				continue
			}
			if !enumMatch(out.Value, p.Values) {
				vs = append(vs, Violation{RuleType: r.Type, Detail: "取值不在允许集合"})
			}
		case RulePrecision:
			if out.Number == nil || p.Scale == nil {
				continue
			}
			scale := pow10(*p.Scale)
			if v := *out.Number * scale; abs(v-round(v)) > 1e-9 {
				vs = append(vs, Violation{RuleType: r.Type, Detail: fmt.Sprintf("小数位超过 %d", *p.Scale)})
			}
		case RuleUnitWhitelist:
			if out.Unit == "" {
				continue
			}
			ok := false
			for _, u := range p.Units {
				if u == out.Unit {
					ok = true
					break
				}
			}
			if !ok {
				vs = append(vs, Violation{RuleType: r.Type, Detail: "单位不在白名单"})
			}
		case RuleNotNull:
			if out.Number == nil && (out.Text == nil || *out.Text == "") {
				vs = append(vs, Violation{RuleType: r.Type, Detail: "空值"})
			}
		}
	}
	return vs
}

// enumMatch 按字面量比较（数字/字符串/布尔都归一成字符串）。
func enumMatch(v any, allowed []any) bool {
	got := literal(v)
	for _, a := range allowed {
		if literal(a) == got {
			return true
		}
	}
	return false
}

func literal(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
