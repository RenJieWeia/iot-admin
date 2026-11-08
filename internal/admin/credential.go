package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/RenJieWeia/iot-admin/internal/broker"
	"github.com/jackc/pgx/v5"
)

// rememberCredential 把 Broker 返回的密码加密后写入 gateway_credential；
// 未配置密钥时只记日志（密码仍可用，但之后无法查看）。
func (a *API) rememberCredential(ctx context.Context, gatewayID, password string) {
	if a.Pool == nil || password == "" {
		return
	}
	if a.Creds == nil {
		return
	}
	enc, err := a.Creds.Encrypt(password)
	if err != nil {
		return
	}
	_, _ = a.Pool.Exec(ctx,
		`insert into gateway_credential (gateway_id, password_enc)
		 values ($1, $2)
		 on conflict (gateway_id) do update set password_enc = excluded.password_enc, updated_at = now()`,
		gatewayID, enc)
}

type credentialPayload struct {
	GatewayID string `json:"gateway_id"`
	Topic     string `json:"mqtt_topic"`
	Username  string `json:"mqtt_username"`
	Password  string `json:"mqtt_password,omitempty"`
	Saved     bool   `json:"password_saved"`
}

// credential 返回网关的 MQTT 主题、用户名与解密后的密码。
func (a *API) credential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a.Creds == nil {
		writeError(w, http.StatusBadRequest, "credential_key_missing: 未配置 IOT_CREDENTIAL_KEY，无法解密密码")
		return
	}
	var enc string
	err := a.Pool.QueryRow(r.Context(), `select password_enc from gateway_credential where gateway_id = $1`, id).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "credential_not_saved: 该网关没有保存密码，请重置后查看")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	password, err := a.Creds.Decrypt(enc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "credential_decrypt_failed: 密码解密失败（密钥可能已更换）")
		return
	}
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: credentialPayload{
		GatewayID: id,
		Topic:     "iot/ingest/" + id,
		Username:  id,
		Password:  password,
		Saved:     true,
	}})
}

// resetCredential 在 Broker 重建账号（旧密码立即失效），并把新密码加密入库。
func (a *API) resetCredential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a.Broker == nil {
		writeError(w, http.StatusBadRequest, "broker_unavailable: 未配置 Broker，无法重置 MQTT 密码")
		return
	}
	if a.Creds == nil {
		writeError(w, http.StatusBadRequest, "credential_key_missing: 未配置 IOT_CREDENTIAL_KEY，无法保存新密码")
		return
	}
	var name string
	if err := a.Pool.QueryRow(r.Context(), `select name from gateway where gateway_id = $1`, id).Scan(&name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeDBError(w, err)
		return
	}
	if err := a.Broker.Unregister(r.Context(), id); err != nil {
		writeBrokerError(w, "broker_unregister_failed", err)
		return
	}
	password, err := a.Broker.Register(r.Context(), broker.Gateway{ID: id, Name: name})
	if err != nil {
		writeBrokerError(w, "broker_register_failed", err)
		return
	}
	a.rememberCredential(r.Context(), id, password)
	a.invalidate()
	writeJSON(w, http.StatusOK, Response{Code: 0, Message: "ok", Data: credentialPayload{
		GatewayID: id,
		Topic:     "iot/ingest/" + id,
		Username:  id,
		Password:  password,
		Saved:     true,
	}})
}

// credentialTopic 是列表展示用：主题固定由 gateway_id 推导。
func credentialTopic(id string) string { return "iot/ingest/" + strings.TrimSpace(id) }
