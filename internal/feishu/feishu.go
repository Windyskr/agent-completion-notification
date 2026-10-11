// Package feishu 通过飞书自定义机器人 webhook 推送通知。
package feishu

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

// Notifier 实现 notify.Notifier。
type Notifier struct {
	cfg    config.Feishu
	client *http.Client
	now    func() time.Time
}

// New 构造飞书渠道。
func New(cfg config.Feishu) *Notifier {
	return &Notifier{
		cfg:    cfg,
		client: webhook.NewClient(),
		now:    time.Now,
	}
}

func (n *Notifier) Name() string { return "feishu" }

// Send 以富文本（post）消息推送，标题与正文已由 notify 统一渲染。
func (n *Notifier) Send(ctx context.Context, msg message.Message) error {
	endpoint, err := webhook.ParseEndpoint(n.cfg.WebhookURL, "飞书 webhook 地址")
	if err != nil {
		return err
	}
	payload := map[string]any{
		"msg_type": "post",
		"content": map[string]any{
			"post": map[string]any{
				"zh_cn": map[string]any{
					"title":   msg.Title,
					"content": [][]map[string]string{{{"tag": "text", "text": msg.Body(message.Format{})}}},
				},
			},
		},
	}
	// 机器人开启「签名校验」时必须带 timestamp 与 sign，否则飞书拒收。
	if secret := strings.TrimSpace(n.cfg.Secret); secret != "" {
		ts := n.now().Unix()
		payload["timestamp"] = strconv.FormatInt(ts, 10)
		payload["sign"] = sign(ts, secret)
	}

	resp, err := webhook.PostJSON(ctx, n.client, endpoint, payload)
	if err != nil {
		return fmt.Errorf("飞书请求失败: %w", err)
	}
	if err := resp.CheckStatus(); err != nil {
		return err
	}
	return checkResponse(resp.Body)
}

// sign 生成飞书签名：以 "{timestamp}\n{secret}" 为 HMAC-SHA256 密钥对空串签名。
func sign(timestamp int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(strconv.FormatInt(timestamp, 10)+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// checkResponse 解析飞书应答。飞书即使业务失败也返回 HTTP 200，必须看 code。
func checkResponse(raw []byte) error {
	var resp struct {
		Code       *int   `json:"code"`
		Msg        string `json:"msg"`
		StatusCode *int   `json:"StatusCode"`
		StatusMsg  string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("无法解析飞书应答: %s", strings.TrimSpace(string(raw)))
	}
	switch {
	case resp.Code != nil && *resp.Code != 0:
		return fmt.Errorf("飞书返回 code=%d: %s", *resp.Code, resp.Msg)
	case resp.StatusCode != nil && *resp.StatusCode != 0:
		return fmt.Errorf("飞书返回 StatusCode=%d: %s", *resp.StatusCode, resp.StatusMsg)
	}
	return nil
}
