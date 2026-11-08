// Package secret 提供网关 MQTT 密码的对称加密（AES-256-GCM）。
//
// 密钥来源（按顺序）：
//  1. IOT_CREDENTIAL_KEY：base64 或 hex 编码的 32 字节密钥；
//  2. IOT_CREDENTIAL_KEY_FILE：指向包含密钥的文件（同样编码）。
//
// 两者都没有时返回 ErrNoKey：调用方应把"无法保存/读取凭据"作为可读错误返回，
// 而不是悄悄存明文。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrNoKey 表示未配置凭据加密密钥。
var ErrNoKey = errors.New("credential_key_missing")

// Cipher 是带固定密钥的 AES-GCM 封装。
type Cipher struct {
	aead cipher.AEAD
}

// Load 按环境变量读取密钥；未配置返回 (nil, ErrNoKey)。
func Load() (*Cipher, error) {
	raw := strings.TrimSpace(os.Getenv("IOT_CREDENTIAL_KEY"))
	if raw == "" {
		if path := strings.TrimSpace(os.Getenv("IOT_CREDENTIAL_KEY_FILE")); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("读取 IOT_CREDENTIAL_KEY_FILE: %w", err)
			}
			raw = strings.TrimSpace(string(data))
		}
	}
	if raw == "" {
		return nil, ErrNoKey
	}
	key, err := decodeKey(raw)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func decodeKey(raw string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(raw); err == nil && len(key) == 32 {
		return key, nil
	}
	if key, err := hex.DecodeString(raw); err == nil && len(key) == 32 {
		return key, nil
	}
	return nil, fmt.Errorf("凭据密钥必须是 32 字节的 base64 或 hex")
}

// Encrypt 返回 base64(nonce||ciphertext)。
func (c *Cipher) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解密 Encrypt 的输出。
func (c *Cipher) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	size := c.aead.NonceSize()
	if len(raw) < size {
		return "", errors.New("密文长度不足")
	}
	plain, err := c.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
