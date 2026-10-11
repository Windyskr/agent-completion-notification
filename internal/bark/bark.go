// Package bark 通过 Bark HTTP API 推送通知。
package bark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/message"
	"github.com/windyskr/agent-completion-notification/internal/webhook"
)

const (
	defaultGroup  = "agent-completion-notification"
	codexIconURL  = "https://raw.githubusercontent.com/Windyskr/agent-completion-notification/main/assets/icons/codex.png"
	claudeIconURL = "https://raw.githubusercontent.com/Windyskr/agent-completion-notification/main/assets/icons/claude.png"
)

type Notifier struct {
	cfg    config.Bark
	client *http.Client
}

func New(cfg config.Bark) *Notifier {
	return &Notifier{
		cfg:    cfg,
		client: webhook.NewClient(),
	}
}

func (n *Notifier) Name() string { return "bark" }

// Send 使用 Bark 官方支持的 JSON POST，避免把中文正文拼进 URL。
func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	endpoint, err := webhook.ParseEndpoint(n.cfg.URL, "Bark 设备端点")
	if err != nil {
		return err
	}
	payload := struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
		Group    string `json:"group"`
		Icon     string `json:"icon,omitempty"`
		URL      string `json:"url,omitempty"`
		ID       string `json:"id,omitempty"`
	}{
		Title:    msg.Title,
		Markdown: msg.Body(message.Format{HardLineBreaks: true}),
		Group:    defaultGroup,
		Icon:     iconForSource(msg.Source),
		URL:      n.cfg.SourceURL(msg.Source),
	}
	// Bark 要求 id 在 JSON 中为字符串；相同会话的后续推送会更新原通知。
	if n.cfg.UpdateBySession {
		payload.ID = msg.SessionID
	}
	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("Bark 请求失败: %w", err)
	}
	if err := resp.CheckStatus(); err != nil {
		return err
	}
	return checkResponse(resp.Body)
}

func iconForSource(source string) string {
	switch source {
	case event.SourceCodex:
		return codexIconURL
	case event.SourceClaude:
		return claudeIconURL
	default:
		return ""
	}
}

func checkResponse(raw []byte) error {
	var resp struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("无法解析 Bark 应答: %s", strings.TrimSpace(string(raw)))
	}
	if resp.Code == nil {
		return fmt.Errorf("Bark 应答缺少 code")
	}
	if *resp.Code != http.StatusOK {
		return fmt.Errorf("Bark 返回 code=%d: %s", *resp.Code, resp.Message)
	}
	return nil
}
