package claude

import (
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/hook"
)

// 选择载荷要带上问题文本与全部选项，手机端才能直接做决定。
func TestFromQuestionPayload(t *testing.T) {
	payload := hook.PreToolUsePayload{
		SessionID:      "s1",
		TranscriptPath: writeTranscript(t, aiTitleLine("发布 v1.5.0")),
		Cwd:            "/work/acn",
		ToolName:       "AskUserQuestion",
		ToolInput:      []byte(`{"questions":[{"question":"是否现在执行 pr-pull？","header":"Homebrew","options":[{"label":"立即执行"},{"label":"先看 PR"}]}]}`),
	}

	ev := FromQuestionPayload(payload)
	if ev.Kind != event.KindChoice {
		t.Errorf("Kind = %q, 期望 %q", ev.Kind, event.KindChoice)
	}
	// 会话名是 transcript 里的真实标题，类别词由渲染层根据 Kind 加在标题前。
	if ev.SessionName != "发布 v1.5.0" {
		t.Errorf("SessionName = %q, 期望 transcript 中的会话标题", ev.SessionName)
	}
	for _, want := range []string{"是否现在执行 pr-pull？", "选项：立即执行 / 先看 PR"} {
		if !strings.Contains(ev.Message, want) {
			t.Errorf("正文缺少 %q:\n%s", want, ev.Message)
		}
	}
}

// 多个问题逐段拼接；问题为空时退回 header。
func TestFromQuestionPayloadMultipleQuestions(t *testing.T) {
	payload := hook.PreToolUsePayload{
		Cwd: "/work",
		ToolInput: []byte(`{"questions":[
			{"question":"第一个问题","options":[{"label":"A"},{"label":"B"}]},
			{"question":"","header":"备用标题","options":[]}
		]}`),
	}

	ev := FromQuestionPayload(payload)
	if got := strings.Count(ev.Message, "\n\n"); got != 1 {
		t.Errorf("两个问题应以空行分隔，实际分隔符数 = %d\n%s", got, ev.Message)
	}
	if !strings.Contains(ev.Message, "备用标题") {
		t.Errorf("空问题时应回退到 header:\n%s", ev.Message)
	}
}

// 载荷异常时仍要产出可读的事件，而不是丢弃通知。
func TestFromQuestionPayloadMalformed(t *testing.T) {
	ev := FromQuestionPayload(hook.PreToolUsePayload{Cwd: "/w", ToolInput: []byte(`{broken`)})
	if ev.Kind != event.KindChoice || ev.Message == "" {
		t.Errorf("损坏载荷应有兜底文案: %+v", ev)
	}
	if ev.SessionName != "" {
		t.Errorf("读不到 transcript 时会话名应为空，得到 %q", ev.SessionName)
	}
}

// 权限与空闲各映射到对应类别；其余类型不推送。
func TestFromNotificationPayload(t *testing.T) {
	cases := []struct {
		notificationType string
		wantKind         event.Event
		wantOK           bool
	}{
		{"permission_prompt", event.Event{Kind: event.KindPermission}, true},
		{"idle_prompt", event.Event{Kind: event.KindIdle}, true},
		{"auth_success", event.Event{}, false},
		{"elicitation_dialog", event.Event{}, false},
		{"", event.Event{}, false},
	}
	for _, c := range cases {
		ev, ok := FromNotificationPayload(hook.NotificationPayload{
			TranscriptPath:   writeTranscript(t, aiTitleLine("排查构建失败")),
			Cwd:              "/w",
			Message:          "Claude needs your permission",
			NotificationType: c.notificationType,
		})
		if ok != c.wantOK {
			t.Errorf("%q ok = %v, 期望 %v", c.notificationType, ok, c.wantOK)
			continue
		}
		if !c.wantOK {
			continue
		}
		if ev.Kind != c.wantKind.Kind {
			t.Errorf("%q Kind = %q, 期望 %q", c.notificationType, ev.Kind, c.wantKind.Kind)
		}
		if ev.Message != "Claude needs your permission" {
			t.Errorf("%q Message = %q, 应保留原始 message", c.notificationType, ev.Message)
		}
		if ev.SessionName != "排查构建失败" {
			t.Errorf("%q SessionName = %q, 期望 transcript 中的会话标题", c.notificationType, ev.SessionName)
		}
	}
}

// 空闲事件没有 message 时给兜底文案。
func TestFromNotificationPayloadEmptyMessage(t *testing.T) {
	ev, ok := FromNotificationPayload(hook.NotificationPayload{NotificationType: "idle_prompt"})
	if !ok || ev.Message == "" {
		t.Errorf("空闲事件应有兜底文案: ok=%v msg=%q", ok, ev.Message)
	}
}

// 异常终止优先展示错误详情，并带上会话标题。
func TestFromStopFailurePayload(t *testing.T) {
	ev := FromStopFailurePayload(hook.StopFailurePayload{
		SessionID:            "s1",
		TranscriptPath:       writeTranscript(t, aiTitleLine("迁移数据库")),
		Error:                "rate_limit",
		ErrorDetails:         "429 Too Many Requests",
		LastAssistantMessage: "API Error",
	})
	if ev.Kind != event.KindFailure || ev.SessionName != "迁移数据库" || ev.Message != "429 Too Many Requests" {
		t.Errorf("异常终止事件 = %+v", ev)
	}
	if got := FromStopFailurePayload(hook.StopFailurePayload{}).Message; got == "" {
		t.Error("缺少错误信息时应有兜底文案")
	}
}
