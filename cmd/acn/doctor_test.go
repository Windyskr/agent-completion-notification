package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/notify"
)

// 推送规则是合法配置，不能让 doctor 因此永远无法验证渠道。
func TestCheckDeliveryIgnoresNotificationRules(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := config.Config{
		Feishu:             config.Feishu{WebhookURL: srv.URL},
		Sources:            map[string]bool{"claude": false},
		SkipUntitled:       true,
		MinDurationSeconds: 3600,
	}

	got := checkDelivery(cfg)
	if got.level != levelOK {
		t.Errorf("自检被推送规则拦下: %+v", got)
	}
	if hits != 1 {
		t.Errorf("自检请求次数 = %d, 期望 1", hits)
	}
}

func TestCheckChannel(t *testing.T) {
	var feishu notify.Channel
	for _, channel := range notify.Channels {
		if channel.Name == config.ChannelFeishu {
			feishu = channel
		}
	}
	cases := []struct {
		cfg    config.Config
		level  level
		detail string
	}{
		{config.Config{}, levelUnknown, "未配置（可选）"},
		{config.Config{Channels: map[string]bool{config.ChannelFeishu: false}}, levelUnknown, "已禁用"},
		{config.Config{Feishu: config.Feishu{WebhookURL: "https://open.feishu.cn/hook/key"}}, levelOK, "https://open.feishu.cn/hook/key"},
	}
	for _, c := range cases {
		got := checkChannel(c.cfg, feishu)
		if got.name != "飞书 webhook" || got.level != c.level || got.detail != c.detail {
			t.Errorf("checkChannel = %+v, 期望 level=%v detail=%q", got, c.level, c.detail)
		}
	}
}
