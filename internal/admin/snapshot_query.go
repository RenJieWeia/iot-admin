package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// 快照查询：按模板（终端类型 + 字段）+ 档位 + 时间范围，透视出「终端 × 字段」的最新值。
//
// 口径：
//   - 只查粗表 snapshot_coarse（时/日/月/年），分钟/秒不在此功能内；
//   - 行 = 终端实例（模板类型下所有终端），列 = 字段（typeID:item_code，避免不同类型同名字段冲突）；
//   - 单元格 = 该档位、时间范围内该终端该字段的最新一条（segment_start 倒序、revision 倒序）；
//   - 限制时间跨度与终端行数，超出直接 400，让调用方收窄条件。

const (
	// snapshotQueryMaxRows 是一次查询最多展开的终端行数。
	snapshotQueryMaxRows = 1000
	// snapshotQueryMaxSpan 是各档位允许的最大时间跨度。
	snapshotQueryMaxSpanHour  = 7 * 24 * time.Hour
	snapshotQueryMaxSpanDay   = 31 * 24 * time.Hour
	snapshotQueryMaxSpanMonth = 366 * 24 * time.Hour
	snapshotQueryMaxSpanYear  = 3650 * 24 * time.Hour
)

type snapshotQueryCell struct {
	Num     *float64 `json:"num,omitempty"`
	Text    *string  `json:"text,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Time    string   `json:"time,omitempty"`
	Quality *int16   `json:"quality,omitempty"`
}

type snapshotQueryColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit,omitempty"`
}

type snapshotQueryRow struct {
	TerminalID   string                       `json:"terminalId"`
	TerminalName string                       `json:"terminalName"`
	TypeID       int64                        `json:"typeId"`
	Values       map[string]snapshotQueryCell `json:"values"`
}

type snapshotQueryPayload struct {
	Template    map[string]any        `json:"template"`
	Granularity string                `json:"granularity"`
	From        string                `json:"from"`
	To          string                `json:"to"`
	Columns     []snapshotQueryColumn `json:"columns"`
	Rows        []snapshotQueryRow    `json:"rows"`
}

// snapshotQuery 处理 GET /admin/v1/snapshot-query。
func (a *API) snapshotQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawTemplate := q.Get("template")
	if rawTemplate == "" {
		writeError(w, http.StatusBadRequest, "invalid_query: 缺少 template")
		return
	}
	templateID, err := strconv.ParseInt(rawTemplate, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query: template 不是整数")
		return
	}
	granularity := q.Get("granularity")
	span, ok := snapshotQuerySpan(granularity)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query: granularity 只能是 hour/day/month/year")
		return
	}
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query: from 需要 RFC3339")
		return
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query: to 需要 RFC3339")
		return
	}
	if !to.After(from) {
		writeError(w, http.StatusBadRequest, "invalid_query: to 必须晚于 from")
		return
	}
	if to.Sub(from) > span {
		writeError(w, http.StatusBadRequest, "invalid_query: 时间跨度超出该档位上限（"+granularity+"）")
		return
	}

	var (
		name        string
		description string
		typesRaw    []byte
		codesRaw    []byte
	)
	err = a.Pool.QueryRow(r.Context(),
		`select name, description, device_type_ids, item_codes from snapshot_query_template where id = $1`,
		templateID).Scan(&name, &description, &typesRaw, &codesRaw)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "template_not_found")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	var typeIDs []int64
	var itemCodes []string
	if err := json.Unmarshal(typesRaw, &typeIDs); err != nil {
		writeDBError(w, err)
		return
	}
	if err := json.Unmarshal(codesRaw, &itemCodes); err != nil {
		writeDBError(w, err)
		return
	}

	// 终端行：只看模板选中的类型；超过上限直接拒绝，不做静默截断。
	var rows []snapshotQueryRow
	termRows, err := a.Pool.Query(r.Context(),
		`select id, coalesce(name, ''), type_id from terminal
		 where type_id = any($1::bigint[]) order by id limit $2`,
		typeIDs, snapshotQueryMaxRows+1)
	if err != nil {
		writeDBError(w, err)
		return
	}
	termIndex := map[string]int{}
	for termRows.Next() {
		var row snapshotQueryRow
		if err := termRows.Scan(&row.TerminalID, &row.TerminalName, &row.TypeID); err != nil {
			termRows.Close()
			writeDBError(w, err)
			return
		}
		row.Values = map[string]snapshotQueryCell{}
		termIndex[row.TerminalID] = len(rows)
		rows = append(rows, row)
	}
	termRows.Close()
	if err := termRows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	if len(rows) > snapshotQueryMaxRows {
		writeError(w, http.StatusBadRequest, "invalid_query: 终端数量超过 1000，请收窄终端类型")
		return
	}

	// 列：模板字段 × 选中类型；只保留类型里真实声明过的字段。
	propRows, err := a.Pool.Query(r.Context(),
		`select p.type_id, p.item_code, p.name, coalesce(p.unit, ''), coalesce(t.name, '')
		 from thing_model_property p join device_type t on t.id = p.type_id
		 where p.type_id = any($1::bigint[]) and p.item_code = any($2::text[])
		 order by p.type_id, p.item_code`,
		typeIDs, itemCodes)
	if err != nil {
		writeDBError(w, err)
		return
	}
	columns := make([]snapshotQueryColumn, 0, len(itemCodes))
	for propRows.Next() {
		var (
			typeID   int64
			code     string
			label    string
			unit     string
			typeName string
		)
		if err := propRows.Scan(&typeID, &code, &label, &unit, &typeName); err != nil {
			propRows.Close()
			writeDBError(w, err)
			return
		}
		key := strconv.FormatInt(typeID, 10) + ":" + code
		columns = append(columns, snapshotQueryColumn{
			Key:   key,
			Label: typeName + " · " + label,
			Unit:  unit,
		})
	}
	propRows.Close()
	if err := propRows.Err(); err != nil {
		writeDBError(w, err)
		return
	}

	if len(rows) > 0 && len(columns) > 0 {
		terminalIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			terminalIDs = append(terminalIDs, row.TerminalID)
		}
		// 每格只取一条：同键按段起点、revision 倒序取第一条。
		cellRows, err := a.Pool.Query(r.Context(),
			`select distinct on (s.terminal_id, s.item_code)
			         s.terminal_id, s.item_code, s.value_num, s.value_text, s.unit, s.value_time, s.quality
			 from snapshot_coarse s
			 where s.granularity = $1 and s.segment_start >= $2 and s.segment_start <= $3
			   and s.terminal_id = any($4::text[])
			 order by s.terminal_id, s.item_code, s.segment_start desc, s.revision desc`,
			granularity, from, to, terminalIDs)
		if err != nil {
			writeDBError(w, err)
			return
		}
		for cellRows.Next() {
			var (
				terminalID string
				itemCode   string
				cell       snapshotQueryCell
				valueTime  time.Time
			)
			if err := cellRows.Scan(&terminalID, &itemCode, &cell.Num, &cell.Text, &cell.Unit, &valueTime, &cell.Quality); err != nil {
				cellRows.Close()
				writeDBError(w, err)
				return
			}
			cell.Time = valueTime.UTC().Format(time.RFC3339)
			if index, ok := termIndex[terminalID]; ok {
				// 列 key = 类型:字段；同 item_code 在不同类型下是不同列。
				rows[index].Values[strconv.FormatInt(rows[index].TypeID, 10)+":"+itemCode] = cell
			}
		}
		cellRows.Close()
		if err := cellRows.Err(); err != nil {
			writeDBError(w, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: snapshotQueryPayload{
		Template:    map[string]any{"id": templateID, "name": name, "description": description},
		Granularity: granularity,
		From:        from.UTC().Format(time.RFC3339),
		To:          to.UTC().Format(time.RFC3339),
		Columns:     columns,
		Rows:        rows,
	}})
}

func snapshotQuerySpan(granularity string) (time.Duration, bool) {
	switch granularity {
	case "hour":
		return snapshotQueryMaxSpanHour, true
	case "day":
		return snapshotQueryMaxSpanDay, true
	case "month":
		return snapshotQueryMaxSpanMonth, true
	case "year":
		return snapshotQueryMaxSpanYear, true
	default:
		return 0, false
	}
}
