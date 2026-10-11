package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/message"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// captureText 拦截发往 Bot API 的请求，返回请求中的 text。
func captureText(t *testing.T, n *Notifier, text *string) {
	t.Helper()
	n.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" || r.URL.Path != "/bot123:abc/sendMessage" {
			t.Errorf("请求地址 = %s", r.URL.Redacted())
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("解析 Telegram 请求失败: %v", err)
		}
		*text = payload.Text
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     http.Header{},
		}, nil
	})
}

func TestSendPostsText(t *testing.T) {
	n := New(config.Telegram{BotToken: "123:abc", ChatID: "42"})
	var text string
	captureText(t, n, &text)

	msg := message.Message{Title: "修复通知", Reply: "改好了", Details: []string{"目录：/work/acn"}}
	if err := n.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if want := "修复通知\n\n改好了\n\n目录：/work/acn"; text != want {
		t.Errorf("text = %q, 期望 %q", text, want)
	}
}

// Telegram text 超过 4096 个 UTF-16 码元会被整条拒收，必须在发送前截断。
func TestSendFitsTextLimit(t *testing.T) {
	n := New(config.Telegram{BotToken: "123:abc", ChatID: "42"})
	var text string
	captureText(t, n, &text)

	msg := message.Message{Title: "长回复", Reply: strings.Repeat("🎉", 3000), Details: []string{"目录：/work/acn"}}
	if err := n.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if units := len(utf16.Encode([]rune(text))); units > maxTextLength {
		t.Errorf("text %d 个 UTF-16 码元，超过上限 %d", units, maxTextLength)
	}
	if !strings.HasSuffix(text, "…\n\n目录：/work/acn") {
		t.Errorf("截断后应保留详情行")
	}
}

// bot token 在请求路径里，网络错误不能把它带进错误信息。
func TestSendDoesNotLeakTokenInNetworkError(t *testing.T) {
	n := New(config.Telegram{BotToken: "123:super-secret-token", ChatID: "42"})
	n.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})

	err := n.Send(context.Background(), message.Message{})
	if err == nil || strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("错误 = %v, 应上报且不得包含 token", err)
	}
}
