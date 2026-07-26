// Package feishu 通过飞书自定义机器人 webhook 推送通知。
package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
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
		client: &http.Client{Timeout: 10 * time.Second},
		now:    time.Now,
	}
}

func (n *Notifier) Name() string { return "feishu" }

// Send 以富文本（post）消息推送，标题与正文由 Event 统一组装。
func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	url := strings.TrimSpace(n.cfg.WebhookURL)
	if url == "" {
		return fmt.Errorf("未配置飞书 webhook 地址")
	}

	now := n.now()
	payload := map[string]any{
		"msg_type": "post",
		"content": map[string]any{
			"post": map[string]any{
				"zh_cn": map[string]any{
					"title":   ev.Title(),
					"content": [][]map[string]string{{{"tag": "text", "text": ev.Body(now)}}},
				},
			},
		},
	}
	// 机器人开启「签名校验」时必须带 timestamp 与 sign，否则飞书拒收。
	if secret := strings.TrimSpace(n.cfg.Secret); secret != "" {
		ts := now.Unix()
		payload["timestamp"] = strconv.FormatInt(ts, 10)
		payload["sign"] = sign(ts, secret)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return checkResponse(raw)
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
