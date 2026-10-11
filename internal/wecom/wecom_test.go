package wecom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/message"
)

func serveCapture(t *testing.T, response string, content *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if content != nil {
			*content = payload.Markdown.Content
		}
		io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendPostsMarkdown(t *testing.T) {
	var content string
	srv := serveCapture(t, `{"errcode":0,"errmsg":"ok"}`, &content)

	msg := message.Message{Title: "修复通知", Reply: "改好了", Details: []string{"完成时间：2026-08-03 12:00:00", "目录：/work/acn"}}
	if err := New(config.WeCom{WebhookURL: srv.URL}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if want := "### 修复通知\n改好了\n\n完成时间：2026-08-03 12:00:00  \n目录：/work/acn"; content != want {
		t.Errorf("content = %q, 期望 %q", content, want)
	}
}

// 企微 markdown content 超过 4096 字节会被整条拒收，必须在发送前截断。
func TestSendFitsContentLimit(t *testing.T) {
	var content string
	srv := serveCapture(t, `{"errcode":0}`, &content)

	msg := message.Message{Title: "长回复", Reply: strings.Repeat("中", 3000), Details: []string{"目录：/work/acn"}}
	if err := New(config.WeCom{WebhookURL: srv.URL}).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if len(content) > maxContentBytes {
		t.Errorf("content %d 字节，超过企微上限 %d", len(content), maxContentBytes)
	}
	if !strings.HasSuffix(content, "…\n\n目录：/work/acn") {
		t.Errorf("截断后应保留详情行: %q", content[len(content)-40:])
	}
}

func TestSendDetectsBusinessError(t *testing.T) {
	srv := serveCapture(t, `{"errcode":93000,"errmsg":"invalid webhook url"}`, nil)

	err := New(config.WeCom{WebhookURL: srv.URL}).Send(context.Background(), message.Message{})
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Errorf("应识别企微业务失败，得到 %v", err)
	}
}
