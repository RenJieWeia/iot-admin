package admin

import (
	"net/http"
	"time"
)

// rawItems 返回某终端在时间窗内的接入流水（异常现场回放）。
// 只读、限窗（≤2 小时）限量（≤1000 行），避免误查全表。
func (a *API) rawItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	terminalID := q.Get("terminal_id")
	if terminalID == "" {
		writeError(w, http.StatusBadRequest, "missing_terminal_id")
		return
	}
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query: from 必须是 RFC3339")
		return
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query: to 必须是 RFC3339")
		return
	}
	if to.Before(from) || to.Sub(from) > 2*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid_query: 时间窗必须在 0~2 小时之间")
		return
	}

	rows, err := a.Pool.Query(r.Context(), `
		select id, message_id, received_at, item_code, value_num, value_text, unit, value_time,
		       quality, quality_reason, status, reason
		from raw_item
		where terminal_id = $1 and received_at >= $2 and received_at < $3
		order by received_at
		limit 1000`, terminalID, from, to)
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0, 64)
	for rows.Next() {
		var (
			id, messageID                                  int64
			receivedAt                                     time.Time
			itemCode                                       string
			valueNum                                       *float64
			valueText, unit, qualityReason, status, reason *string
			valueTime                                      *time.Time
			quality                                        int16
		)
		if err := rows.Scan(&id, &messageID, &receivedAt, &itemCode, &valueNum, &valueText,
			&unit, &valueTime, &quality, &qualityReason, &status, &reason); err != nil {
			writeDBError(w, err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "message_id": messageID, "received_at": receivedAt,
			"item_code": itemCode, "value_num": valueNum, "value_text": valueText,
			"unit": unit, "value_time": valueTime, "quality": quality,
			"quality_reason": qualityReason, "status": status, "reason": reason,
		})
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: out, Meta: nil})
}

// rawMessage 返回单条流水的整包原文。
func (a *API) rawMessage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id")
		return
	}
	var payload []byte
	if err := a.Pool.QueryRow(r.Context(),
		`select payload from raw_message where id = $1::bigint`, id).Scan(&payload); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: map[string]any{"payload": string(payload)}})
}
