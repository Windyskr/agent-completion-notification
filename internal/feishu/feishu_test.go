package feishu

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

// serveCapture 起一个假飞书，记录收到的请求体并返回指定应答。
func serveCapture(t *testing.T, response string, captured *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if captured != nil {
			_ = json.Unmarshal(body, captured)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendPostsRichText(t *testing.T) {
	var got map[string]any
	srv := serveCapture(t, `{"code":0,"msg":"success"}`, &got)

	n := New(config.Feishu{WebhookURL: srv.URL})
	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", Message: "改好了", DurationMS: 90_000}

	if err := n.Send(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	if got["msg_type"] != "post" {
		t.Errorf("msg_type = %v, 期望 post", got["msg_type"])
	}
	// 标题与正文应由 Event 统一组装。
	raw, _ := json.Marshal(got)
	text := string(raw)
	for _, want := range []string{"Claude Code", "acn", "1m30s", "改好了"} {
		if !strings.Contains(text, want) {
			t.Errorf("请求体缺少 %q:\n%s", want, text)
		}
	}
	// 未配置密钥时不应带签名字段。
	if _, exists := got["sign"]; exists {
		t.Error("未开启签名校验却带了 sign")
	}
}

// 开启签名校验时必须同时带 timestamp 与 sign，否则飞书会拒收。
func TestSendIncludesSignatureWhenSecretSet(t *testing.T) {
	var got map[string]any
	srv := serveCapture(t, `{"code":0}`, &got)

	n := New(config.Feishu{WebhookURL: srv.URL, Secret: "my-secret"})
	n.now = func() time.Time { return time.Unix(1700000000, 0) }

	if err := n.Send(context.Background(), event.Event{Source: event.SourceClaude}); err != nil {
		t.Fatal(err)
	}

	if got["timestamp"] != "1700000000" {
		t.Errorf("timestamp = %v", got["timestamp"])
	}
	want := sign(1700000000, "my-secret")
	if got["sign"] != want {
		t.Errorf("sign = %v, 期望 %v", got["sign"], want)
	}
}

// 飞书业务失败时仍返回 HTTP 200，必须靠 code 判定，否则会把失败当成功。
func TestSendDetectsBusinessError(t *testing.T) {
	srv := serveCapture(t, `{"code":19021,"msg":"sign match fail"}`, nil)

	err := New(config.Feishu{WebhookURL: srv.URL}).Send(context.Background(), event.Event{})
	if err == nil {
		t.Fatal("HTTP 200 但 code 非 0，应当判定为失败")
	}
	if !strings.Contains(err.Error(), "19021") {
		t.Errorf("错误信息应包含 code：%v", err)
	}
}

// 另一种应答格式：StatusCode/StatusMessage。
func TestSendDetectsStatusCodeError(t *testing.T) {
	srv := serveCapture(t, `{"StatusCode":9499,"StatusMessage":"bad request"}`, nil)

	err := New(config.Feishu{WebhookURL: srv.URL}).Send(context.Background(), event.Event{})
	if err == nil || !strings.Contains(err.Error(), "9499") {
		t.Errorf("应识别出 StatusCode 形式的失败，得到 %v", err)
	}
}

func TestSendReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "not found")
	}))
	defer srv.Close()

	err := New(config.Feishu{WebhookURL: srv.URL}).Send(context.Background(), event.Event{})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("应报告 HTTP 404，得到 %v", err)
	}
}

func TestSendRequiresWebhook(t *testing.T) {
	if err := New(config.Feishu{}).Send(context.Background(), event.Event{}); err == nil {
		t.Error("未配置地址时应当报错")
	}
}
