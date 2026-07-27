package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
)

func TestGateBlocksDisabledSource(t *testing.T) {
	cfg := config.Config{Sources: map[string]bool{"codex": false}}
	if reason := Gate(cfg, event.Event{Source: event.SourceCodex}); reason == "" {
		t.Error("已禁用的来源仍被放行")
	}
	// 未列出的来源默认开启。
	if reason := Gate(cfg, event.Event{Source: event.SourceClaude}); reason != "" {
		t.Errorf("未显式配置的来源应默认开启，却被拦截：%s", reason)
	}
}

func TestGateAppliesDurationThreshold(t *testing.T) {
	cfg := config.Config{MinDurationSeconds: 30}

	if reason := Gate(cfg, event.Event{Source: event.SourceClaude, DurationMS: 5_000}); reason == "" {
		t.Error("低于阈值的任务应被拦截")
	}
	if reason := Gate(cfg, event.Event{Source: event.SourceClaude, DurationMS: 60_000}); reason != "" {
		t.Errorf("高于阈值的任务被误拦：%s", reason)
	}
}

// 耗时未知时阈值不能生效，否则会把取不到起点的通知全部挡掉。
func TestGateSkipsThresholdWhenDurationUnknown(t *testing.T) {
	cfg := config.Config{MinDurationSeconds: 30}
	if reason := Gate(cfg, event.Event{Source: event.SourceCodex, DurationMS: 0}); reason != "" {
		t.Errorf("耗时未知时不应套用阈值，却被拦截：%s", reason)
	}
}

// 被 Gate 拦下时不得发出任何请求。
func TestSendSkipsWithoutHTTPCall(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := config.Config{
		Feishu:  config.Feishu{WebhookURL: srv.URL},
		Sources: map[string]bool{"claude": false},
	}
	skipped, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude})

	if err != nil {
		t.Fatal(err)
	}
	if skipped == "" {
		t.Error("应报告跳过原因")
	}
	if hit {
		t.Error("被拦截的事件仍发出了 HTTP 请求")
	}
}

func TestSendReportsMissingWebhook(t *testing.T) {
	skipped, err := Send(context.Background(), config.Config{}, event.Event{Source: event.SourceClaude})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skipped, "未配置") {
		t.Errorf("跳过原因 = %q, 期望提示未配置", skipped)
	}
}

func TestSendDelivers(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := config.Config{
		Feishu:          config.Feishu{WebhookURL: srv.URL},
		DeviceName:      "devbox",
		ShowDeviceName:  true,
		ShowProjectName: true,
		AgentNames:      map[string]string{"claude": "opus"},
	}
	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", Message: "改好了"}

	skipped, err := Send(context.Background(), cfg, ev)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != "" {
		t.Errorf("不应跳过：%s", skipped)
	}
	if !strings.Contains(got, "改好了") {
		t.Errorf("请求体未包含正文：%s", got)
	}
	if !strings.Contains(got, "devbox-opus-acn 任务完成") {
		t.Errorf("请求体未包含设备与来源标题：%s", got)
	}
}

func TestSendPropagatesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":19021,"msg":"sign match fail"}`)
	}))
	defer srv.Close()

	cfg := config.Config{Feishu: config.Feishu{WebhookURL: srv.URL}}
	if _, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude}); err == nil {
		t.Error("飞书业务失败应当上报")
	}
}
