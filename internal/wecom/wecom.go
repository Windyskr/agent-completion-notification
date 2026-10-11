// Package wecom 通过企业微信群机器人推送通知。
package wecom

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

// maxContentBytes 是企微 markdown 消息 content 的上限；超出会被整条拒收。
const maxContentBytes = 4096

type Notifier struct {
	cfg    config.WeCom
	client *http.Client
}

func New(cfg config.WeCom) *Notifier {
	return &Notifier{cfg: cfg, client: webhook.NewClient()}
}

func (n *Notifier) Name() string { return "wecom" }

func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	endpoint, err := webhook.ParseEndpoint(n.cfg.WebhookURL, "企微 webhook 地址")
	if err != nil {
		return err
	}
	payload := map[string]any{"msgtype": "markdown", "markdown": map[string]string{
		"content": msg.Body(message.Format{
			Header:         "### " + msg.Title + "\n",
			HardLineBreaks: true,
			MaxBytes:       maxContentBytes,
		}),
	}}
	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("企微请求失败: %w", err)
	}
	if err := resp.CheckStatus(); err != nil {
		return err
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return fmt.Errorf("无法解析企微应答: %s", strings.TrimSpace(string(resp.Body)))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("企微返回 errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}
