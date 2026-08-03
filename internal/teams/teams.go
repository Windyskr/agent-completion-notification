// Package teams 通过 Microsoft Teams Incoming Webhook 推送通知。
package teams

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
	cfg    config.Teams
	client *http.Client
	now    func() time.Time
}

func New(cfg config.Teams) *Notifier {
	return &Notifier{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

func (n *Notifier) Name() string { return "teams" }

func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	endpoint := strings.TrimSpace(n.cfg.WebhookURL)
	parsed, err := url.Parse(endpoint)
	if endpoint == "" {
		return fmt.Errorf("未配置 Teams webhook 地址")
	}
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Teams webhook 地址格式无效")
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
		Summary:    ev.Title(),
		ThemeColor: "0076D7",
		Title:      ev.Title(),
		Sections:   []section{{Text: ev.Body(n.now()), Markdown: true}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 Teams 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("Teams 请求失败: %w", unwrapURLError(err))
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

type section struct {
	Text     string `json:"text"`
	Markdown bool   `json:"markdown"`
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
