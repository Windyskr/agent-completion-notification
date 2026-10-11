// Package webhook 封装各通知渠道共用的 JSON POST：地址校验、错误脱敏与应答限读。
//
// webhook 地址里通常带着机器人 token 或设备 key，而渠道错误会写进 stderr 与事件
// 日志，因此本包返回的任何错误都不包含请求地址。
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponseBytes 限制应答读取量。渠道应答只用于判定成败，几 KB 足矣。
const maxResponseBytes = 8 << 10

// NewClient 返回渠道使用的 HTTP 客户端。notify 已为整次投递设置更短的总超时，
// 这里的超时只为直接调用渠道、没有截止时间的场景兜底。
func NewClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// ParseEndpoint 校验地址必须是带主机名的 http(s) URL。name 用于错误信息，
// 如「飞书 webhook 地址」。不能把 url.Parse 的错误原样返回：它会带上完整地址。
func ParseEndpoint(raw, name string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s未配置", name)
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, fmt.Errorf("%s格式无效", name)
	}
	return endpoint, nil
}

// Response 是渠道的 HTTP 应答；Body 最多读取 8KB。
type Response struct {
	StatusCode int
	Body       []byte
}

// CheckStatus 在 HTTP 状态码不是 2xx 时返回带状态码与应答内容的错误。
// 多数渠道业务失败时仍返回 200，状态码通过后还需各自检查应答体。
func (r Response) CheckStatus() error {
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("HTTP %d: %s", r.StatusCode, strings.TrimSpace(string(r.Body)))
}

// PostJSON 把 payload 序列化为 JSON 发往 endpoint，并读取受限长度的应答。
// 返回的错误只说明网络层原因，不含请求地址。
func PostJSON(ctx context.Context, client *http.Client, endpoint *url.URL, payload any) (Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("序列化请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, unwrapURLError(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return Response{}, unwrapURLError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Response{}, fmt.Errorf("读取应答失败: %w", unwrapURLError(err))
	}
	return Response{StatusCode: resp.StatusCode, Body: raw}, nil
}

// unwrapURLError 剥掉 url.Error：它的 Error() 会包含完整的请求地址。
func unwrapURLError(err error) error {
	for {
		urlErr, ok := err.(*url.Error)
		if !ok {
			return err
		}
		err = urlErr.Err
	}
}
