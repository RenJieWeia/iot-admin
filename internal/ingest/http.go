package ingest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// GatewayHeader 是 HTTP 通道的网关标识头。
const GatewayHeader = "X-Gateway-Id"

// MaxBodyBytes 传输层请求体上限（R12）。
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

// bodyTooLargeError 是整包级错误体。
type bodyTooLargeError struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
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

	res, err := h.P.Handle(r.Context(), r.Header.Get(GatewayHeader), body)
	if err != nil {
		var envelopeErr *validate.EnvelopeError
		if errors.As(err, &envelopeErr) {
			writeJSON(w, http.StatusBadRequest, Response{
				Code: http.StatusBadRequest, Message: envelopeErr.Reason,
				Data: bodyTooLargeError{Reason: envelopeErr.Reason, Detail: envelopeErr.Detail},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, Response{Code: http.StatusInternalServerError, Message: ReasonInternal})
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: res})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
