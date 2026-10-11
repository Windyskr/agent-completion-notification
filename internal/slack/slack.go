// Package slack 通过 Slack Incoming Webhook 推送通知。
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/message"
	"github.com/windyskr/agent-completion-notification/internal/webhook"
)

type Notifier struct {
	cfg    config.Slack
	client *http.Client
}

func New(cfg config.Slack) *Notifier {
	return &Notifier{cfg: cfg, client: webhook.NewClient()}
}

func (n *Notifier) Name() string { return "slack" }

func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	endpoint, err := webhook.ParseEndpoint(n.cfg.WebhookURL, "Slack webhook 地址")
	if err != nil {
		return err
	}
	payload := struct {
		Text string `json:"text"`
	}{Text: msg.Body(message.Format{Header: msg.Title + "\n\n"})}
	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("Slack 请求失败: %w", err)
	}
	if err := resp.CheckStatus(); err != nil {
		return err
	}
	return checkResponse(resp.Body)
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
