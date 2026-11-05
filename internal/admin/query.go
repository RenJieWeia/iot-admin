package admin

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 查询参数（与服务端 SQL 白名单同源）：
//
//	q                 关键词，只在实体声明的文本列上做 ILIKE
//	sort / order      排序白名单 + asc|desc，默认 id desc
//	page / pageSize   分页，默认 1 / 10，pageSize 上限 50
//	filter.<col>           字段筛选，操作符取该字段声明的第一个（text→contains、enum/relation→in、boolean→eq）
//	filter.<col>.<op>      显式操作符：contains|eq|prefix|in|gte|lte
//	limit / offset    旧参数，仍兼容：limit 映射 pageSize，offset 映射 page
const (
	// DefaultPageSize 是列表页默认每页条数。
	DefaultPageSize = 10
	// MaxPageSize 是服务端允许的最大每页条数。
	MaxPageSize = 50
)

// Filter 是一条结构化筛选条件：操作符与字段都必须在白名单内。
type Filter struct {
	Field string
	Op    string
	Value string // in 用逗号分隔多值；gte/lte 用标量
}

// QueryState 是列表查询的唯一事实源（关键词/筛选/排序/分页全部由服务端执行）。
type QueryState struct {
	Q        string
	Sort     string
	Order    string
	Page     int
	PageSize int
	Filters  []Filter
}

// QueryError 对应 400 invalid_query，detail 说明具体拒绝原因，不静默忽略。
type QueryError struct{ Detail string }

func (e *QueryError) Error() string { return "invalid_query: " + e.Detail }

func invalidQuery(format string, args ...any) error {
	return &QueryError{Detail: fmt.Sprintf(format, args...)}
}

// ParseQueryState 解析并校验查询参数；未知字段/未知操作符/非法取值一律报错。
func ParseQueryState(e Entity, v url.Values) (QueryState, error) {
	fields, ok := FieldMetas(e.Name)
	if !ok {
		return QueryState{}, invalidQuery("unknown_entity %q", e.Name)
	}
	qs := QueryState{Q: strings.TrimSpace(v.Get("q")), Page: 1, PageSize: DefaultPageSize, Order: "desc"}
	qs.Sort = defaultSort(fields)

	if raw := strings.TrimSpace(v.Get("page")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return QueryState{}, invalidQuery("page 不是整数: %q", raw)
		}
		if n < 1 {
			n = 1
		}
		qs.Page = n
	}
	if raw := strings.TrimSpace(v.Get("pageSize")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return QueryState{}, invalidQuery("pageSize 不是整数: %q", raw)
		}
		qs.PageSize = clampPageSize(n)
	} else if raw := strings.TrimSpace(v.Get("limit")); raw != "" {
		// 旧参数兼容：limit/offset → pageSize/page。
		n, err := strconv.Atoi(raw)
		if err != nil {
			return QueryState{}, invalidQuery("limit 不是整数: %q", raw)
		}
		qs.PageSize = clampPageSize(n)
		if off, err := strconv.Atoi(strings.TrimSpace(v.Get("offset"))); err == nil && off > 0 {
			qs.Page = off/qs.PageSize + 1
		}
	} else if raw := strings.TrimSpace(v.Get("offset")); raw != "" {
		if _, err := strconv.Atoi(raw); err != nil {
			return QueryState{}, invalidQuery("offset 不是整数: %q", raw)
		}
	}

	if raw := strings.TrimSpace(v.Get("sort")); raw != "" {
		f, ok := FieldMetaOf(e.Name, raw)
		if !ok {
			return QueryState{}, invalidQuery("未知排序字段 %q", raw)
		}
		if !f.Sortable {
			return QueryState{}, invalidQuery("字段 %q 不可排序", raw)
		}
		qs.Sort = raw
	}
	if raw := strings.TrimSpace(v.Get("order")); raw != "" {
		order := strings.ToLower(raw)
		if order != "asc" && order != "desc" {
			return QueryState{}, invalidQuery("order 只能是 asc 或 desc: %q", raw)
		}
		qs.Order = order
	}

	filters, err := parseFilters(e, v)
	if err != nil {
		return QueryState{}, err
	}
	qs.Filters = filters
	return qs, nil
}

func clampPageSize(n int) int {
	if n < 1 {
		return DefaultPageSize
	}
	if n > MaxPageSize {
		return MaxPageSize
	}
	return n
}

func defaultSort(fields []FieldMeta) string {
	for _, f := range fields {
		if f.Key == "id" {
			return "id"
		}
	}
	// 没有自增主键的表（如 exception_count）默认按时间列倒序，符合"最近异常先看"。
	for _, f := range fields {
		if f.Sortable && f.Type == "datetime" {
			return f.Key
		}
	}
	for _, f := range fields {
		if f.Sortable {
			return f.Key
		}
	}
	return fields[0].Key
}

// parseFilters 只接受 filter.<col>[.<op>] 形式；未知列、未声明的操作符、非法取值都拒绝。
func parseFilters(e Entity, v url.Values) ([]Filter, error) {
	var out []Filter
	// filter.<col>[.<op>] 的确定性顺序：按 URL 键排序，避免 map 迭代造成的抖动。
	keys := make([]string, 0, len(v))
	for k := range v {
		if strings.HasPrefix(k, "filter.") {
			keys = append(keys, k)
		}
	}
	for _, key := range keys {
		rest := strings.TrimPrefix(key, "filter.")
		col, op := rest, ""
		if i := strings.LastIndex(rest, "."); i >= 0 {
			col, op = rest[:i], rest[i+1:]
		}
		f, ok := FieldMetaOf(e.Name, col)
		if !ok {
			return nil, invalidQuery("未知筛选字段 %q", col)
		}
		if !f.Filterable {
			return nil, invalidQuery("字段 %q 不可筛选", col)
		}
		if op == "" {
			if len(f.Operators) == 0 {
				return nil, invalidQuery("字段 %q 未声明可用的筛选操作符", col)
			}
			op = f.Operators[0]
		}
		if !operatorAllowed(f, op) {
			return nil, invalidQuery("字段 %q 不支持操作符 %q", col, op)
		}
		values := v[key]
		switch op {
		case "in":
			joined := make([]string, 0, len(values))
			for _, raw := range values {
				for _, part := range strings.Split(raw, ",") {
					if p := strings.TrimSpace(part); p != "" {
						joined = append(joined, p)
					}
				}
			}
			if len(joined) == 0 {
				return nil, invalidQuery("字段 %q 的 in 筛选缺少取值", col)
			}
			out = append(out, Filter{Field: col, Op: op, Value: strings.Join(joined, ",")})
		default:
			raw := strings.TrimSpace(values[len(values)-1])
			if raw == "" {
				continue
			}
			if err := validateValue(f, raw); err != nil {
				return nil, err
			}
			out = append(out, Filter{Field: col, Op: op, Value: raw})
		}
	}
	return out, nil
}

func operatorAllowed(f FieldMeta, op string) bool {
	for _, allowed := range f.Operators {
		if allowed == op {
			return true
		}
	}
	return false
}

func validateValue(f FieldMeta, raw string) error {
	switch f.Type {
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return invalidQuery("字段 %q 需要数值: %q", f.Key, raw)
		}
	case "boolean":
		if raw != "true" && raw != "false" {
			return invalidQuery("字段 %q 需要 true/false: %q", f.Key, raw)
		}
	}
	return nil
}

// BuildListQuery 生成带 where/order/limit 的列表 SQL 与参数（标识符只来自白名单）。
func BuildListQuery(e Entity, qs QueryState) (string, []any, error) {
	where, args, err := buildWhere(e, qs)
	if err != nil {
		return "", nil, err
	}
	var sb strings.Builder
	sb.WriteString("select * from ")
	sb.WriteString(e.Table)
	if where != "" {
		sb.WriteString(" where ")
		sb.WriteString(where)
	}
	sb.WriteString(" order by ")
	sb.WriteString(orderClause(e, qs))
	args = append(args, qs.PageSize, (qs.Page-1)*qs.PageSize)
	fmt.Fprintf(&sb, " limit $%d offset $%d", len(args)-1, len(args))
	return sb.String(), args, nil
}

// BuildCountQuery 生成总数 SQL（与列表共用同一 where，保证 meta.total 与行集一致）。
func BuildCountQuery(e Entity, qs QueryState) (string, []any, error) {
	where, args, err := buildWhere(e, qs)
	if err != nil {
		return "", nil, err
	}
	sql := "select count(*) from " + e.Table
	if where != "" {
		sql += " where " + where
	}
	return sql, args, nil
}

// orderClause = 白名单排序字段 + 方向 + 唯一 tie-breaker，保证分页稳定不重不漏。
func orderClause(e Entity, qs QueryState) string {
	sort := qs.Sort
	if sort == "" {
		fields, _ := FieldMetas(e.Name)
		sort = defaultSort(fields)
	}
	order := qs.Order
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	clause := sort + " " + order
	for _, tie := range tieBreakers(e, sort) {
		clause += ", " + tie + " asc"
	}
	return clause
}

func tieBreakers(e Entity, sort string) []string {
	if sort != "id" && hasColumn(e, "id") {
		return []string{"id"}
	}
	if sort != "id" && !hasColumn(e, "id") {
		var out []string
		for _, c := range e.Cols {
			if c != sort {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
}

func hasColumn(e Entity, col string) bool {
	for _, c := range e.Cols {
		if c == col {
			return true
		}
	}
	return false
}

func buildWhere(e Entity, qs QueryState) (string, []any, error) {
	var (
		clauses []string
		args    []any
	)
	add := func(format string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(format, len(args)))
	}

	if qs.Q != "" {
		var ors []string
		for _, f := range TextSearchFields(e.Name) {
			if !hasColumn(e, f.Key) {
				continue
			}
			args = append(args, "%"+escapeLike(qs.Q)+"%")
			ors = append(ors, fmt.Sprintf("%s::text ilike $%d", f.Key, len(args)))
		}
		if len(ors) > 0 {
			clauses = append(clauses, "("+strings.Join(ors, " or ")+")")
		}
	}

	for _, flt := range qs.Filters {
		f, ok := FieldMetaOf(e.Name, flt.Field)
		if !ok || !hasColumn(e, f.Key) {
			return "", nil, invalidQuery("未知筛选字段 %q", flt.Field)
		}
		switch flt.Op {
		case "contains":
			add(f.Key+"::text ilike $%d", "%"+escapeLike(flt.Value)+"%")
		case "prefix":
			add(f.Key+"::text ilike $%d", escapeLike(flt.Value)+"%")
		case "eq":
			add(f.Key+"::text = $%d", flt.Value)
		case "in":
			add(f.Key+"::text = any($%d)", strings.Split(flt.Value, ","))
		case "gte", "lte":
			op := ">="
			if flt.Op == "lte" {
				op = "<="
			}
			if f.Type == "number" {
				add(f.Key+" "+op+" $%d::numeric", flt.Value)
			} else {
				add(f.Key+" "+op+" $%d::timestamptz", flt.Value)
			}
		default:
			return "", nil, invalidQuery("字段 %q 不支持操作符 %q", flt.Field, flt.Op)
		}
	}
	return strings.Join(clauses, " and "), args, nil
}

// escapeLike 转义 LIKE 通配符，保证 q 不会被 % _ \ 放大成全表扫描或误命中。
func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// ListMeta 是列表响应的分页与排序说明；data 仍是数组，旧客户端不受影响。
type ListMeta struct {
	Total    int64  `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Sort     string `json:"sort"`
	Order    string `json:"order"`
}

// ListResult 是一次列表查询的结果（列表 API 与管理后台 UI 共用同一口径）。
type ListResult struct {
	Data []map[string]any
	Meta ListMeta
}

// ListRows 执行列表 + 总数查询；列表与总数共用同一 where，保证 meta.total 与行集一致。
func ListRows(ctx context.Context, pool *pgxpool.Pool, e Entity, qs QueryState) (ListResult, error) {
	listSQL, listArgs, err := BuildListQuery(e, qs)
	if err != nil {
		return ListResult{}, err
	}
	countSQL, countArgs, err := BuildCountQuery(e, qs)
	if err != nil {
		return ListResult{}, err
	}
	rows, err := pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return ListResult{}, err
	}
	items, err := collect(rows)
	if err != nil {
		return ListResult{}, err
	}
	if items == nil {
		items = []map[string]any{}
	}
	var total int64
	if err := pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return ListResult{}, err
	}
	return ListResult{Data: items, Meta: ListMeta{
		Total: total, Page: qs.Page, PageSize: qs.PageSize, Sort: qs.Sort, Order: qs.Order,
	}}, nil
}
