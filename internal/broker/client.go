// Package broker 是平台与 BroMQ 管理接口的客户端：网关注册/注销必须走 Broker 的
// 开放接口（HTTP），平台不直接改 Broker 的库或配置文件。
//
// 契约（BroMQ 实测）：
//
//	POST /api/auth/login  {username,password}            → {"token": "..."}（JWT，Bearer 鉴权）
//	POST /api/mqtt/users  {username,password,description} → {"id":N,...}
//	POST /api/acl         {mqtt_user_id,topic,permission} → {"id":N,...}
//	GET  /api/mqtt/users                                  → {"data":[{id,username,...}]}
//	DELETE /api/mqtt/users/{id}                           → 200（ACL 级联删除，已实测）
//
// MQTT 用户即网关身份：username = gateway_id；平台生成密码后只在创建响应里回一次。
package broker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Gateway 是注册到 Broker 的网关身份；凭证与 ACL 由 Broker 自己生成与管理。
type Gateway struct {
	ID   string `json:"gatewayId"`
	Name string `json:"name"`
}

// Error 是 Broker 返回的非 2xx 响应，保留状态码与响应体便于排查。
type Error struct {
	Op     string
	Status int
	Body   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("broker %s 失败: HTTP %d %s", e.Op, e.Status, strings.TrimSpace(e.Body))
}

// Client 用 HTTP 调 Broker 管理接口。Token 为空时每次操作自动登录换取。
type Client struct {
	BaseURL  string
	Username string
	Password string
	Token    string // 可选：直接给定管理令牌时跳过登录
	HTTP     *http.Client

	mu    sync.Mutex
	token string
}

// New 构造客户端；baseURL 为空表示未启用（调用方应跳过同步）。
func New(baseURL, username, password string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 8 * time.Second},
	}
}

// Register 在 Broker 创建 MQTT 用户并授予 iot/ingest/{id} 的发布权限，返回一次性密码。
func (c *Client) Register(ctx context.Context, gw Gateway) (string, error) {
	if gw.ID == "" {
		return "", errors.New("网关 ID 为空")
	}
	password, err := randomPassword()
	if err != nil {
		return "", err
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/mqtt/users", map[string]any{
		"username":    gw.ID,
		"password":    password,
		"description": "iot-admin 网关：" + gw.Name,
	}, &user); err != nil {
		return "", err
	}
	// 网关只要向 iot/ingest/{gateway_id} 发布报文；订阅权限由平台账号持有。
	if err := c.call(ctx, http.MethodPost, "/api/acl", map[string]any{
		"mqtt_user_id": user.ID,
		"topic":        "iot/ingest/" + gw.ID,
		"permission":   "pub",
	}, nil); err != nil {
		// 建号成功但授权失败：把没用的账号回滚掉，避免留下"能连不能发"的悬空用户。
		_ = c.deleteUser(ctx, user.ID)
		return "", err
	}
	return password, nil
}

// Unregister 注销网关对应的 MQTT 用户；用户不存在视为已注销（幂等）。
func (c *Client) Unregister(ctx context.Context, gatewayID string) error {
	var list struct {
		Data []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/mqtt/users", nil, &list); err != nil {
		return err
	}
	for _, user := range list.Data {
		if user.Username == gatewayID {
			return c.deleteUser(ctx, user.ID)
		}
	}
	return nil
}

func (c *Client) deleteUser(ctx context.Context, id int64) error {
	err := c.call(ctx, http.MethodDelete, fmt.Sprintf("/api/mqtt/users/%d", id), nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// tokenLocked 返回可用令牌；没有就登录一次并缓存（Broker 令牌有效期 24h）。
func (c *Client) tokenLocked(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Token != "" {
		return c.Token, nil
	}
	if c.token != "" {
		return c.token, nil
	}
	if c.Username == "" || c.Password == "" {
		return "", errors.New("未配置 Broker 管理账号（username/password）")
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := c.callOnce(ctx, http.MethodPost, "/api/auth/login", map[string]any{
		"username": c.Username,
		"password": c.Password,
	}, &res, ""); err != nil {
		return "", err
	}
	if res.Token == "" {
		return "", errors.New("Broker 登录未返回 token")
	}
	c.token = res.Token
	return c.token, nil
}

func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	token, err := c.tokenLocked(ctx)
	if err != nil {
		return err
	}
	err = c.callOnce(ctx, method, path, body, out, token)
	var brokerErr *Error
	if errors.As(err, &brokerErr) && brokerErr.Status == http.StatusUnauthorized {
		// 令牌失效：清缓存重登一次。
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if token, err2 := c.tokenLocked(ctx); err2 == nil {
			return c.callOnce(ctx, method, path, body, out, token)
		}
	}
	return err
}

func (c *Client) callOnce(ctx context.Context, method, path string, body any, out any, token string) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("broker %s %s 请求失败: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		raw, _ := io.ReadAll(res.Body)
		return &Error{Op: method + " " + path, Status: res.StatusCode, Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// IsNotFound 判断错误是否是 Broker 的 404（例如注销时对象已不存在）。
func IsNotFound(err error) bool {
	var brokerErr *Error
	return errors.As(err, &brokerErr) && brokerErr.Status == http.StatusNotFound
}

// randomPassword 生成 32 字节随机密码（base64url，无填充），长度 43。
func randomPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// gatewayTopic 仅供测试/文档引用：网关的发布主题。
func gatewayTopic(id string) string {
	return "iot/ingest/" + url.PathEscape(id)
}
