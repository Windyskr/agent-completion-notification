package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
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
