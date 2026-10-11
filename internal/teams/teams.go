// Package teams 通过 Microsoft Teams Incoming Webhook 推送通知。
package teams

import (
	"context"
	"fmt"
	"net/http"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/message"
	"github.com/windyskr/agent-completion-notification/internal/webhook"
)

type Notifier struct {
	cfg    config.Teams
	client *http.Client
}

func New(cfg config.Teams) *Notifier {
	return &Notifier{cfg: cfg, client: webhook.NewClient()}
}

func (n *Notifier) Name() string { return "teams" }

func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	endpoint, err := webhook.ParseEndpoint(n.cfg.WebhookURL, "Teams webhook 地址")
	if err != nil {
		return err
	}
	payload := struct {
		Type       string    `json:"@type"`
		Context    string    `json:"@context"`
		Summary    string    `json:"summary"`
		ThemeColor string    `json:"themeColor"`
		Title      string    `json:"title"`
		Sections   []section `json:"sections"`
	}{
		Type:       "MessageCard",
		Context:    "http://schema.org/extensions",
		Summary:    msg.Title,
		ThemeColor: "0076D7",
		Title:      msg.Title,
		Sections:   []section{{Text: msg.Body(message.Format{HardLineBreaks: true}), Markdown: true}},
	}
	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("Teams 请求失败: %w", err)
	}
	return resp.CheckStatus()
}

type section struct {
	Text     string `json:"text"`
	Markdown bool   `json:"markdown"`
}
