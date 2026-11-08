-- 网关 MQTT 凭据：密码以 AES-GCM 密文保存，便于管理员随时查看/复制。
-- 明文只在注册/重置的响应里出现一次；读取需走 /admin/v1/gateways/{id}/credential。
create table if not exists gateway_credential (
    gateway_id  text primary key references gateway (gateway_id) on delete cascade,
    password_enc text not null,
    updated_at  timestamptz not null default now()
);
