// Package slack 通过 Slack Incoming Webhook 推送通知。
package slack

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

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
)

type Notifier struct {
	cfg    config.Slack
	client *http.Client
	now    func() time.Time
}

func New(cfg config.Slack) *Notifier {
	return &Notifier{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

func (n *Notifier) Name() string { return "slack" }

func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	endpoint := strings.TrimSpace(n.cfg.WebhookURL)
	parsed, err := url.Parse(endpoint)
	if endpoint == "" {
		return fmt.Errorf("未配置 Slack webhook 地址")
	}
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Slack webhook 地址格式无效")
	}

	payload := struct {
		Text string `json:"text"`
	}{Text: ev.Title() + "\n\n" + ev.Body(n.now())}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 Slack 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("Slack 请求失败: %w", unwrapURLError(err))
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return checkResponse(raw)
}

// Slack Incoming Webhook 成功时通常返回纯文本 ok，也兼容 JSON 成功应答。
func checkResponse(raw []byte) error {
	response := strings.TrimSpace(string(raw))
	if strings.EqualFold(response, "ok") {
		return nil
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &result) == nil && result.OK {
		return nil
	}
	if response == "" {
		return fmt.Errorf("Slack 应答为空")
	}
	if result.Error != "" {
		return fmt.Errorf("Slack 返回错误: %s", result.Error)
	}
	return fmt.Errorf("Slack 返回: %s", response)
}

func unwrapURLError(err error) error {
	for {
		urlErr, ok := err.(*url.Error)
		if !ok {
			return err
		}
		err = urlErr.Err
	}
}
