package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RenJieWeia/iot-admin/internal/admin"
)

// ErrUnknownEntity 表示 URL 段不是已知实体，调用方据此返回 404。
var ErrUnknownEntity = errors.New("unknown_entity")

// ErrNotFound 表示按 id 取行时不存在。
var ErrNotFound = errors.New("not_found")

// Source 是管理后台的读取入口。页面只依赖这个接口，测试用假实现，无需数据库。
type Source interface {
	List(ctx context.Context, entity string, qs admin.QueryState) (admin.ListResult, error)
	Get(ctx context.Context, entity, id string) (map[string]any, error)
}

// 写入能力不放进 Source：页面读列表/单行用 Source，写入由默认实现提供（假 Source 不实现即为只读）。
type mutator interface {
	Create(ctx context.Context, entity string, form url.Values) (string, error)
	Update(ctx context.Context, entity, id string, form url.Values) error
	Delete(ctx context.Context, entity, id string) error
}

// PoolSource 是 Source 的默认实现：列表与总数复用 admin 的 QueryState 口径，写入走同一白名单。
type PoolSource struct {
	Pool *pgxpool.Pool
}

func NewPoolSource(pool *pgxpool.Pool) *PoolSource { return &PoolSource{Pool: pool} }

func (s *PoolSource) List(ctx context.Context, entity string, qs admin.QueryState) (admin.ListResult, error) {
	e, ok := entityByName(entity)
	if !ok {
		return admin.ListResult{}, ErrUnknownEntity
	}
	return admin.ListRows(ctx, s.Pool, e, qs)
}

func (s *PoolSource) Get(ctx context.Context, entity, id string) (map[string]any, error) {
	e, ok := entityByName(entity)
	if !ok {
		return nil, ErrUnknownEntity
	}
	// row_to_json 让 pgx 的类型（timestamptz / jsonb / range）统一按 JSON 语义读出，与列表保持一致。
	var raw string
	err := s.Pool.QueryRow(ctx,
		fmt.Sprintf("select row_to_json(t)::text from (select * from %s where id::text = $1) t", e.Table), id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return nil, err
	}
	return row, nil
}

// Create 按实体列白名单插入一行并返回新行 id（非自增主键时返回入参 id）。
func (s *PoolSource) Create(ctx context.Context, entity string, form url.Values) (string, error) {
	e, ok := entityByName(entity)
	if !ok {
		return "", ErrUnknownEntity
	}
	cols, args, casts, err := writeValues(entity, form)
	if err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", errors.New("empty_body")
	}
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = fmt.Sprintf("$%d%s", i+1, casts[i])
	}
	// INSERT 不能放进子查询，直接 RETURNING id；bigint 与 text 主键统一转成文本。
	var id string
	if err := s.Pool.QueryRow(ctx, fmt.Sprintf("insert into %s (%s) values (%s) returning id::text",
		e.Table, strings.Join(cols, ", "), strings.Join(placeholders, ", ")), args...).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

// Update 更新白名单列；受影响行数为 0 视为不存在。
func (s *PoolSource) Update(ctx context.Context, entity, id string, form url.Values) error {
	e, ok := entityByName(entity)
	if !ok {
		return ErrUnknownEntity
	}
	cols, args, casts, err := writeValues(entity, form)
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return errors.New("empty_body")
	}
	sets := make([]string, len(cols))
	for i, c := range cols {
		sets[i] = fmt.Sprintf("%s = $%d%s", c, i+1, casts[i])
	}
	args = append(args, id)
	tag, err := s.Pool.Exec(ctx,
		fmt.Sprintf("update %s set %s where id::text = $%d", e.Table, strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 删除一行；受影响行数为 0 视为不存在。
func (s *PoolSource) Delete(ctx context.Context, entity, id string) error {
	e, ok := entityByName(entity)
	if !ok {
		return ErrUnknownEntity
	}
	tag, err := s.Pool.Exec(ctx, fmt.Sprintf("delete from %s where id::text = $1", e.Table), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// columnKinds 声明每个实体可写列的 Go 取值类型，写入时据此转换并显式 cast。
// 取值与 migrations/0001 的列类型一一对应，不靠运行时反射猜。
var columnKinds = map[string]map[string]string{
	"data-domains": {"code": "text", "name": "text"},
	"device-types": {"code": "text", "name": "text", "domain_id": "int"},
	"thing-model-properties": {
		"type_id": "int", "item_code": "text", "name": "text", "value_type": "text",
		"unit": "text", "precision": "int", "required": "bool", "min_value": "float", "max_value": "float",
	},
	"terminals": {"id": "text", "type_id": "int", "gateway_id": "text", "name": "text", "enabled": "bool"},
	"quality-rules": {
		"terminal_id": "text", "item_code": "text", "rule_type": "text", "params": "json", "enabled": "bool",
	},
	"settle-configs": {"domain_id": "int", "type_id": "int", "window_seconds": "int"},
}

// writeValues 把表单按列白名单转成 (列, 参数, 显式 cast)；空字符串表示"不设置该列"。
func writeValues(entity string, form url.Values) ([]string, []any, []string, error) {
	e, ok := entityByName(entity)
	if !ok {
		return nil, nil, nil, ErrUnknownEntity
	}
	kinds := columnKinds[entity]
	var (
		cols  []string
		args  []any
		casts []string
	)
	for _, c := range e.Cols {
		kind, ok := kinds[c]
		if !ok {
			continue // 自增主键等不可写列
		}
		raw := strings.TrimSpace(form.Get(c))
		if raw == "" {
			continue
		}
		switch kind {
		case "int":
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("字段 %s 需要整数: %q", c, raw)
			}
			cols, args, casts = append(cols, c), append(args, n), append(casts, "")
		case "float":
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("字段 %s 需要数值: %q", c, raw)
			}
			cols, args, casts = append(cols, c), append(args, f), append(casts, "")
		case "bool":
			if raw != "true" && raw != "false" {
				return nil, nil, nil, fmt.Errorf("字段 %s 需要 true/false: %q", c, raw)
			}
			cols, args, casts = append(cols, c), append(args, raw == "true"), append(casts, "")
		case "json":
			var probe any
			if err := json.Unmarshal([]byte(raw), &probe); err != nil {
				return nil, nil, nil, fmt.Errorf("字段 %s 不是合法 JSON: %v", c, err)
			}
			cols, args, casts = append(cols, c), append(args, raw), append(casts, "::jsonb")
		default:
			cols, args, casts = append(cols, c), append(args, raw), append(casts, "")
		}
	}
	return cols, args, casts, nil
}
