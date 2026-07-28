// Package bark 通过 Bark HTTP API 推送通知。
package bark

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

const (
	defaultGroup  = "agent-completion-notification"
	codexIconURL  = "https://raw.githubusercontent.com/Windyskr/agent-completion-notification/main/assets/icons/codex.png"
	claudeIconURL = "https://raw.githubusercontent.com/Windyskr/agent-completion-notification/main/assets/icons/claude.png"
)

type Notifier struct {
	cfg    config.Bark
	client *http.Client
	now    func() time.Time
}

func New(cfg config.Bark) *Notifier {
	return &Notifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		now:    time.Now,
	}
}

func (n *Notifier) Name() string { return "bark" }

// Send 使用 Bark 官方支持的 JSON POST，避免把中文正文拼进 URL。
func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	endpoint := strings.TrimSpace(n.cfg.URL)
	if endpoint == "" {
		return fmt.Errorf("未配置 Bark 设备端点")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Bark 设备端点格式无效")
	}
	payload := struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
		Group    string `json:"group"`
		Icon     string `json:"icon,omitempty"`
		URL      string `json:"url,omitempty"`
		ID       string `json:"id,omitempty"`
	}{
		Title:    ev.Title(),
		Markdown: ev.Body(n.now()),
		Group:    defaultGroup,
		Icon:     iconForSource(ev.Source),
		URL:      n.cfg.SourceURL(ev.Source),
	}
	// Bark 要求 id 在 JSON 中为字符串；相同会话的后续推送会更新原通知。
	if n.cfg.UpdateBySession {
		payload.ID = strings.TrimSpace(ev.SessionID)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 Bark 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := n.client.Do(req)
	if err != nil {
		// url.Error.String 会包含带设备 key 的完整 URL，只保留底层错误。
		for {
			urlErr, ok := err.(*url.Error)
			if !ok {
				break
			}
			err = urlErr.Err
		}
		return fmt.Errorf("Bark 请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return checkResponse(raw)
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
