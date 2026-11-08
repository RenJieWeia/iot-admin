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

// MaxBodyBytes 传输层请求体上限（R12）：超限返回 400 + body_too_large，不做截断。
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

// envelopeErrorBody 是整包级错误体（body 超限、ver 不支持、terminals 非法等共用）。
type envelopeErrorBody struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, Response{Code: http.StatusMethodNotAllowed, Message: "method_not_allowed"})
		return
	}
	// 用 MaxBytesReader 而不是 LimitReader：超限必须显式报错，不能静默截断。
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusBadRequest, Response{
				Code: http.StatusBadRequest, Message: validate.ReasonBodyTooLarge,
				Data: envelopeErrorBody{Reason: validate.ReasonBodyTooLarge, Detail: "请求体超过 1MB"},
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, Response{Code: http.StatusBadRequest, Message: "body_read_error"})
		return
	}

	res, err := h.P.Handle(r.Context(), r.Header.Get(GatewayHeader), body)
	if err != nil {
		var envelopeErr *validate.EnvelopeError
		if errors.As(err, &envelopeErr) {
			// 接入鉴权失败（未注册/停用）是 403：与数据问题（400）区分，网关侧可据此停止重传。
			status := http.StatusBadRequest
			switch envelopeErr.Reason {
			case validate.ReasonGatewayUnknown, validate.ReasonGatewayDisabled:
				status = http.StatusForbidden
			}
			writeJSON(w, status, Response{
				Code: status, Message: envelopeErr.Reason,
				Data: envelopeErrorBody{Reason: envelopeErr.Reason, Detail: envelopeErr.Detail},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, Response{Code: http.StatusInternalServerError, Message: ReasonInternal})
		return
	}
	// 平台侧故障（DB 不可用等）与数据问题必须区分：网关把 accepted:false 当成数据坏，
	// 不会重传；这里整包给 503，body 仍带完整 results，调用方可据此判断哪些终端已成功。
	// 整包重试是安全的：不做幂等，但 value_time 相等时不覆盖；重复发布实时消息属已允许行为。
	for _, r := range res.Results {
		if r.Reason == ReasonInternal {
			writeJSON(w, http.StatusServiceUnavailable, Response{
				Code: http.StatusServiceUnavailable, Message: ReasonInternal, Data: res,
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: res})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
