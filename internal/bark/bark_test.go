package bark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/message"
)

func TestSendPostsJSON(t *testing.T) {
	var method, contentType string
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		io.WriteString(w, `{"code":200,"message":"success"}`)
	}))
	t.Cleanup(srv.Close)

	n := New(config.Bark{URL: srv.URL + "/device-key", UpdateBySession: true})
	msg := message.Message{
		Source: event.SourceClaude, SessionID: "s1", Title: "claude-acn 任务完成", Reply: "改好了",
		Details: []string{"完成时间：2026-07-27 12:00:00", "目录：/work/acn"},
	}
	if err := n.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}

	if method != http.MethodPost || !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("请求 = %s %s", method, contentType)
	}
	if got["title"] != "claude-acn 任务完成" {
		t.Errorf("title = %q", got["title"])
	}
	if got["group"] != "agent-completion-notification" {
		t.Errorf("group = %q, 期望 agent-completion-notification", got["group"])
	}
	if got["icon"] != claudeIconURL {
		t.Errorf("icon = %q, 期望 Claude 官方图标", got["icon"])
	}
	// 正文段落内的单换行要改写成 Markdown 强制换行，否则详情会挤成一行。
	if want := "改好了\n\n完成时间：2026-07-27 12:00:00  \n目录：/work/acn"; got["markdown"] != want {
		t.Errorf("markdown = %q, 期望 %q", got["markdown"], want)
	}
	if got["id"] != "s1" {
		t.Errorf("id = %q, 同会话更新应使用会话 ID", got["id"])
	}
}

func TestIconForSource(t *testing.T) {
	tests := map[string]string{
		event.SourceCodex:  "https://raw.githubusercontent.com/Windyskr/agent-completion-notification/main/assets/icons/codex.png",
		event.SourceClaude: "https://raw.githubusercontent.com/Windyskr/agent-completion-notification/main/assets/icons/claude.png",
		"unknown":          "",
	}
	for source, want := range tests {
		if got := iconForSource(source); got != want {
			t.Errorf("iconForSource(%q) = %q, 期望 %q", source, got, want)
		}
	}
}

func TestSendDetectsBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":400,"message":"device key is invalid"}`)
	}))
	t.Cleanup(srv.Close)

	err := New(config.Bark{URL: srv.URL}).Send(context.Background(), message.Message{})
	if err == nil || !strings.Contains(err.Error(), "code=400") {
		t.Errorf("应识别 Bark 业务失败，得到 %v", err)
	}
}

func TestSendReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	err := New(config.Bark{URL: srv.URL}).Send(context.Background(), message.Message{})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("应报告 HTTP 503，得到 %v", err)
	}
}

func TestSendRejectsMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `not json`)
	}))
	t.Cleanup(srv.Close)

	if err := New(config.Bark{URL: srv.URL}).Send(context.Background(), message.Message{}); err == nil {
		t.Error("非法 Bark 应答应当报错")
	}
}

func TestSendRequiresURL(t *testing.T) {
	if err := New(config.Bark{}).Send(context.Background(), message.Message{}); err == nil {
		t.Error("未配置 Bark URL 时应当报错")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSendDoesNotLeakDeviceKeyInNetworkError(t *testing.T) {
	n := New(config.Bark{URL: "https://api.day.app/super-secret-device-key"})
	n.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})

	err := n.Send(context.Background(), message.Message{})
	if err == nil {
		t.Fatal("网络错误应当上报")
	}
	if strings.Contains(err.Error(), "super-secret-device-key") {
		t.Errorf("错误泄露了 Bark device key: %v", err)
	}
}
