package teams

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

func TestSendPostsMessageCard(t *testing.T) {
	var method, contentType string
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("解析 Teams 请求失败: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	n := New(config.Teams{WebhookURL: srv.URL})
	n.now = func() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }
	ev := event.Event{Source: event.SourceCodex, Cwd: "/work/acn", SessionName: "更新文档", Message: "完成"}
	if err := n.Send(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	if method != http.MethodPost || !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("请求 = %s %s", method, contentType)
	}
	if payload["@type"] != "MessageCard" || payload["@context"] != "http://schema.org/extensions" {
		t.Errorf("MessageCard 元数据错误: %#v", payload)
	}
	if payload["title"] != "Codex-acn-更新文档" {
		t.Errorf("title = %v", payload["title"])
	}
	sections, ok := payload["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("sections = %#v", payload["sections"])
	}
	section, ok := sections[0].(map[string]any)
	if !ok {
		t.Fatalf("section = %#v", sections[0])
	}
	text, _ := section["text"].(string)
	for _, want := range []string{"2026-08-03 12:00:00", "完成", "/work/acn"} {
		if !strings.Contains(text, want) {
			t.Errorf("Teams 正文缺少 %q: %s", want, text)
		}
	}
	if section["markdown"] != true {
		t.Errorf("markdown = %v, 期望 true", section["markdown"])
	}
}

func TestSendReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	err := New(config.Teams{WebhookURL: srv.URL}).Send(context.Background(), event.Event{})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("应报告 HTTP 502，得到 %v", err)
	}
}

func TestSendRequiresWebhook(t *testing.T) {
	if err := New(config.Teams{}).Send(context.Background(), event.Event{}); err == nil {
		t.Error("未配置 Teams URL 时应当报错")
	}
}
