// Package telegram 通过 Telegram Bot API 推送通知。
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/message"
	"github.com/windyskr/agent-completion-notification/internal/webhook"
)

// maxTextLength 是 sendMessage 的 text 上限，按 UTF-16 码元计；超出会被整条拒收。
const maxTextLength = 4096

type Notifier struct {
	cfg    config.Telegram
	client *http.Client
}

func New(cfg config.Telegram) *Notifier {
	return &Notifier{cfg: cfg, client: webhook.NewClient()}
}

func (n *Notifier) Name() string { return "telegram" }

func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	token, chatID := strings.TrimSpace(n.cfg.BotToken), strings.TrimSpace(n.cfg.ChatID)
	if token == "" || chatID == "" {
		return fmt.Errorf("Telegram bot token 或 chat_id 未配置")
	}
	// 直接构造 URL 而不是拼字符串再解析：token 在路径里，解析失败的错误会带出它。
	endpoint := &url.URL{Scheme: "https", Host: "api.telegram.org", Path: "/bot" + token + "/sendMessage"}
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     msg.Body(message.Format{Header: msg.Title + "\n\n", MaxUTF16: maxTextLength}),
		"disable_web_page_preview": true,
	}
	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("Telegram 请求失败: %w", err)
	}
	// Bot API 失败时也返回 JSON，description 比状态码更能说明原因，因此先解析再判定。
	var result struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return fmt.Errorf("无法解析 Telegram 应答: %s", strings.TrimSpace(string(resp.Body)))
	}
	if resp.CheckStatus() != nil || !result.OK {
		return fmt.Errorf("Telegram 返回 HTTP %d code=%d: %s", resp.StatusCode, result.ErrorCode, result.Description)
	}
	return nil
}
