package ingest

import (
	"encoding/json"
	"io"
	"net/http"
)

// GatewayHeader 是 HTTP 通道的网关标识头。
const GatewayHeader = "X-Gateway-Id"

// MaxBodyBytes 单条消息上限，防止意外超大报文占用内存。
const MaxBodyBytes = 1 << 20

// HTTPHandler 处理 POST /ingest/v1/messages。
type HTTPHandler struct {
	P *Processor
}

// Response 与数据源管理 API 同一信封格式。
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, Response{Code: http.StatusMethodNotAllowed, Message: "method_not_allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Code: http.StatusBadRequest, Message: "body_read_error"})
		return
	}
	res := h.P.Handle(r.Context(), r.Header.Get(GatewayHeader), body)

	switch {
	case res.Reason == ReasonInternal:
		writeJSON(w, http.StatusInternalServerError, Response{Code: http.StatusInternalServerError, Message: res.Reason, Data: res})
	case !res.Accepted:
		writeJSON(w, http.StatusBadRequest, Response{Code: http.StatusBadRequest, Message: res.Reason, Data: res})
	default:
		writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: res})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
