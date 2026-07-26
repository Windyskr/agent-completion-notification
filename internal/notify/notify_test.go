package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/windyskr/acn/internal/config"
	"github.com/windyskr/acn/internal/event"
)

// stubNotifier 记录是否被调用，用于验证 Gate 的拦截行为。
type stubNotifier struct {
	name   string
	err    error
	called bool
}

func (s *stubNotifier) Name() string { return s.name }
func (s *stubNotifier) Send(context.Context, event.Event) error {
	s.called = true
	return s.err
}

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

// 耗时未知（Codex）时阈值不能生效，否则会把该来源的通知全部挡掉。
func TestGateSkipsThresholdWhenDurationUnknown(t *testing.T) {
	cfg := config.Config{MinDurationSeconds: 30}
	if reason := Gate(cfg, event.Event{Source: event.SourceCodex, DurationMS: 0}); reason != "" {
		t.Errorf("耗时未知时不应套用阈值，却被拦截：%s", reason)
	}
}

// 被 Gate 拦下时，渠道一次都不该被调用。
func TestDispatchSkipsChannelsWhenGated(t *testing.T) {
	cfg := config.Config{Sources: map[string]bool{"claude": false}}
	stub := &stubNotifier{name: "stub"}

	results := Dispatch(context.Background(), cfg, []Notifier{stub}, event.Event{Source: event.SourceClaude})

	if stub.called {
		t.Error("被拦截的事件仍调用了渠道")
	}
	if len(results) != 1 || results[0].Skipped == "" {
		t.Errorf("结果 = %+v, 期望一条跳过记录", results)
	}
}

func TestDispatchWithoutChannelsReportsSkip(t *testing.T) {
	results := Dispatch(context.Background(), config.Config{}, nil, event.Event{Source: event.SourceClaude})
	if len(results) != 1 || !strings.Contains(results[0].Skipped, "未配置") {
		t.Errorf("结果 = %+v, 期望提示未配置渠道", results)
	}
}

// 单个渠道失败不能影响其余渠道。
func TestDispatchIsolatesChannelFailures(t *testing.T) {
	bad := &stubNotifier{name: "bad", err: errors.New("boom")}
	good := &stubNotifier{name: "good"}

	results := Dispatch(context.Background(), config.Config{}, []Notifier{bad, good}, event.Event{Source: event.SourceClaude})

	if !bad.called || !good.called {
		t.Error("并非所有渠道都被调用")
	}
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, 期望 2", len(results))
	}
	if results[0].Err == nil || results[1].Err != nil {
		t.Errorf("结果 = %+v, 期望仅第一个失败", results)
	}
}

func TestBuildRegistersFeishuOnlyWhenConfigured(t *testing.T) {
	if got := Build(config.Config{}); len(got) != 0 {
		t.Errorf("未配置 webhook 时不应有渠道，得到 %d 个", len(got))
	}
	cfg := config.Config{Feishu: config.Feishu{WebhookURL: "https://example.com/hook"}}
	if got := Build(cfg); len(got) != 1 || got[0].Name() != "feishu" {
		t.Errorf("配置 webhook 后应注册飞书渠道，得到 %+v", got)
	}
}
