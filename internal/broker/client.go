// Package broker 是平台与 BroMQ 管理接口的客户端：网关注册/注销必须走 Broker 的
// 开放接口（HTTP），平台不直接改 Broker 的库或配置文件。
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Gateway 是注册到 Broker 的网关身份；凭证与 ACL 由 Broker 自己生成与管理。
type Gateway struct {
	ID      string `json:"gatewayId"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
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

// Client 用 HTTP 调 Broker 管理接口。
//
// 契约（Broker 侧）：
//
//	POST   {base}/api/gateways        {"gatewayId","name","enabled"}  → 2xx
//	DELETE {base}/api/gateways/{id}                                   → 2xx 或 404
//
// 鉴权用 Bearer Token（Broker 签发的管理令牌）。Token 为空时不带 Authorization，
// 仅供 Broker 关闭鉴权的内网环境使用。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New 构造客户端；baseURL 为空表示未启用（调用方应跳过注册）。
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
}

// Register 在 Broker 注册（或幂等更新）一个网关。
func (c *Client) Register(ctx context.Context, gw Gateway) error {
	body, err := json.Marshal(gw)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/gateways", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, "register")
}

// Unregister 从 Broker 注销网关；404 视为已注销（幂等）。
func (c *Client) Unregister(ctx context.Context, gatewayID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.BaseURL+"/api/gateways/"+url.PathEscape(gatewayID), nil)
	if err != nil {
		return err
	}
	return c.do(req, "unregister")
}

func (c *Client) do(req *http.Request, op string) error {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("broker %s 请求失败: %w", op, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound && op == "unregister" {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		// Broker 的错误体通常是一行 JSON，整读即可；成功响应不读体。
		raw, _ := io.ReadAll(res.Body)
		return &Error{Op: op, Status: res.StatusCode, Body: string(raw)}
	}
	return nil
}

// IsNotFound 判断错误是否是 Broker 的 404（例如注销时对象已不存在）。
func IsNotFound(err error) bool {
	var brokerErr *Error
	return errors.As(err, &brokerErr) && brokerErr.Status == http.StatusNotFound
}
