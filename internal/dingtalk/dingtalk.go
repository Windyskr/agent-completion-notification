// Package dingtalk 通过钉钉自定义机器人推送通知。
package dingtalk

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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
)

type Notifier struct {
	cfg    config.DingTalk
	client *http.Client
	now    func() time.Time
}

func New(cfg config.DingTalk) *Notifier {
	return &Notifier{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

func (n *Notifier) Name() string { return "dingtalk" }

func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	endpoint := strings.TrimSpace(n.cfg.WebhookURL)
	if endpoint == "" {
		return fmt.Errorf("未配置钉钉 webhook 地址")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("钉钉 webhook 地址格式无效")
	}
	now := n.now()
	if secret := strings.TrimSpace(n.cfg.Secret); secret != "" {
		timestamp := now.UnixMilli()
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "\n" + secret))
		query := parsed.Query()
		query.Set("timestamp", strconv.FormatInt(timestamp, 10))
		query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		parsed.RawQuery = query.Encode()
	}
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"title": ev.Title(), "text": "### " + ev.Title() + "\n\n" + ev.Body(now)},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("钉钉请求失败: %w", unwrapURLError(err))
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
		return fmt.Errorf("无法解析钉钉应答: %s", strings.TrimSpace(string(raw)))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("钉钉返回 errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func unwrapURLError(err error) error {
	for {
		u, ok := err.(*url.Error)
		if !ok {
			return err
		}
		err = u.Err
	}
}
