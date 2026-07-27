// Package wecom 通过企业微信群机器人推送通知。
package wecom

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
	cfg    config.WeCom
	client *http.Client
	now    func() time.Time
}

func New(cfg config.WeCom) *Notifier {
	return &Notifier{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}
func (n *Notifier) Name() string { return "wecom" }

func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	endpoint := strings.TrimSpace(n.cfg.WebhookURL)
	parsed, err := url.Parse(endpoint)
	if endpoint == "" {
		return fmt.Errorf("未配置企微 webhook 地址")
	}
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("企微 webhook 地址格式无效")
	}
	payload := map[string]any{"msgtype": "markdown", "markdown": map[string]string{
		"content": "### " + ev.Title() + "\n" + ev.Body(n.now()),
	}}
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
		return fmt.Errorf("企微请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("无法解析企微应答: %s", strings.TrimSpace(string(raw)))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("企微返回 errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}
