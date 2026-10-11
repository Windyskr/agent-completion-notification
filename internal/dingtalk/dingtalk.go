// Package dingtalk 通过钉钉自定义机器人推送通知。
package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/message"
	"github.com/windyskr/agent-completion-notification/internal/webhook"
)

type Notifier struct {
	cfg    config.DingTalk
	client *http.Client
	now    func() time.Time
}

func New(cfg config.DingTalk) *Notifier {
	return &Notifier{cfg: cfg, client: webhook.NewClient(), now: time.Now}
}

func (n *Notifier) Name() string { return "dingtalk" }

func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	endpoint, err := webhook.ParseEndpoint(n.cfg.WebhookURL, "钉钉 webhook 地址")
	if err != nil {
		return err
	}
	// 机器人开启「加签」时，timestamp 与 sign 以查询参数附在地址上。
	if secret := strings.TrimSpace(n.cfg.Secret); secret != "" {
		timestamp := n.now().UnixMilli()
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "\n" + secret))
		query := endpoint.Query()
		query.Set("timestamp", strconv.FormatInt(timestamp, 10))
		query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		endpoint.RawQuery = query.Encode()
	}
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": msg.Title,
			"text":  msg.Body(message.Format{Header: "### " + msg.Title + "\n\n", HardLineBreaks: true}),
		},
	}
	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("钉钉请求失败: %w", err)
	}
	if err := resp.CheckStatus(); err != nil {
		return err
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return fmt.Errorf("无法解析钉钉应答: %s", strings.TrimSpace(string(resp.Body)))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("钉钉返回 errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}
