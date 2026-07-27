// Package telegram 通过 Telegram Bot API 推送通知。
package telegram

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
	cfg    config.Telegram
	client *http.Client
	now    func() time.Time
}

func New(cfg config.Telegram) *Notifier {
	return &Notifier{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}
func (n *Notifier) Name() string { return "telegram" }

func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	token, chatID := strings.TrimSpace(n.cfg.BotToken), strings.TrimSpace(n.cfg.ChatID)
	if token == "" || chatID == "" {
		return fmt.Errorf("Telegram bot token 或 chat_id 未配置")
	}
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(token) + "/sendMessage"
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     ev.Title() + "\n\n" + ev.Body(n.now()),
		"disable_web_page_preview": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := n.client.Do(req)
	if err != nil {
		for {
			u, ok := err.(*url.Error)
			if !ok {
				break
			}
			err = u.Err
		}
		return fmt.Errorf("Telegram 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var result struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("无法解析 Telegram 应答: %s", strings.TrimSpace(string(raw)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.OK {
		return fmt.Errorf("Telegram 返回 HTTP %d code=%d: %s", resp.StatusCode, result.ErrorCode, result.Description)
	}
	return nil
}
