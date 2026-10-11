package dingtalk

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
	"github.com/windyskr/agent-completion-notification/internal/message"
)

func TestSendPostsMarkdownWithSignature(t *testing.T) {
	var query map[string][]string
	var payload struct {
		Markdown struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"markdown"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewDecoder(r.Body).Decode(&payload)
		io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	n := New(config.DingTalk{WebhookURL: srv.URL + "?access_token=key", Secret: "SEC123"})
	n.now = func() time.Time { return time.UnixMilli(1700000000000) }
	msg := message.Message{Title: "修复通知", Reply: "改好了", Details: []string{"完成时间：2026-08-03 12:00:00", "目录：/work/acn"}}
	if err := n.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}

	if payload.Markdown.Title != "修复通知" {
		t.Errorf("title = %q", payload.Markdown.Title)
	}
	if want := "### 修复通知\n\n改好了\n\n完成时间：2026-08-03 12:00:00  \n目录：/work/acn"; payload.Markdown.Text != want {
		t.Errorf("text = %q, 期望 %q", payload.Markdown.Text, want)
	}
	if query["access_token"][0] != "key" || query["timestamp"][0] != "1700000000000" || query["sign"][0] == "" {
		t.Errorf("加签参数错误: %v", query)
	}
}

func TestSendDetectsBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errcode":310000,"errmsg":"sign not match"}`)
	}))
	t.Cleanup(srv.Close)

	err := New(config.DingTalk{WebhookURL: srv.URL}).Send(context.Background(), message.Message{})
	if err == nil || !strings.Contains(err.Error(), "310000") {
		t.Errorf("应识别钉钉业务失败，得到 %v", err)
	}
}
