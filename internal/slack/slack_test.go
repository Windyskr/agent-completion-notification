package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
)

func TestSendPostsText(t *testing.T) {
	var method, contentType string
	var payload struct {
		Text string `json:"text"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("解析 Slack 请求失败: %v", err)
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)

	n := New(config.Slack{WebhookURL: srv.URL})
	n.now = func() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }
	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", SessionName: "修复通知", Message: "改好了"}
	if err := n.Send(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	if method != http.MethodPost || !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("请求 = %s %s", method, contentType)
	}
	for _, want := range []string{"修复通知", "2026-08-03 12:00:00", "改好了", "/work/acn"} {
		if !strings.Contains(payload.Text, want) {
			t.Errorf("Slack 文本缺少 %q: %s", want, payload.Text)
		}
	}
}

func TestSendDetectsSlackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "invalid_payload")
	}))
	t.Cleanup(srv.Close)

	err := New(config.Slack{WebhookURL: srv.URL}).Send(context.Background(), event.Event{})
	if err == nil || !strings.Contains(err.Error(), "invalid_payload") {
		t.Errorf("应识别 Slack 业务失败，得到 %v", err)
	}
}

func TestSendReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	err := New(config.Slack{WebhookURL: srv.URL}).Send(context.Background(), event.Event{})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("应报告 HTTP 503，得到 %v", err)
	}
}

func TestSendRequiresWebhook(t *testing.T) {
	if err := New(config.Slack{}).Send(context.Background(), event.Event{}); err == nil {
		t.Error("未配置 Slack URL 时应当报错")
	}
}

func TestCheckResponseAcceptsJSONSuccess(t *testing.T) {
	if err := checkResponse([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("JSON 成功应被接受: %v", err)
	}
}
