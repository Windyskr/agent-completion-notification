package claude

import (
	"encoding/json"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/hook"
)

// questionInput 是 AskUserQuestion 的 tool_input 结构。
type questionInput struct {
	Questions []question `json:"questions"`
}

type question struct {
	Question string `json:"question"`
	Header   string `json:"header"`
	Options  []struct {
		Label string `json:"label"`
	} `json:"options"`
}

// FromQuestionPayload 把 PreToolUse(AskUserQuestion) 载荷翻译成「等待选择」事件。
// 问题文本与选项都会放进正文，让手机端不用回到电脑也能做决定。
func FromQuestionPayload(p hook.PreToolUsePayload) event.Event {
	ev := event.Event{
		Source:    event.SourceClaude,
		Cwd:       p.Cwd,
		SessionID: p.SessionID,
		Kind:      event.KindChoice,
	}
	var input questionInput
	if err := json.Unmarshal(p.ToolInput, &input); err != nil || len(input.Questions) == 0 {
		ev.SessionName = "等待选择"
		ev.Message = "Claude 正在等待你的选择。"
		return ev
	}

	parts := make([]string, 0, len(input.Questions))
	for _, q := range input.Questions {
		text := strings.TrimSpace(q.Question)
		if text == "" {
			text = strings.TrimSpace(q.Header)
		}
		if text == "" {
			continue
		}
		labels := make([]string, 0, len(q.Options))
		for _, opt := range q.Options {
			if label := strings.TrimSpace(opt.Label); label != "" {
				labels = append(labels, label)
			}
		}
		if len(labels) > 0 {
			text += "\n选项：" + strings.Join(labels, " / ")
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		ev.SessionName = "等待选择"
		ev.Message = "Claude 正在等待你的选择。"
		return ev
	}
	ev.SessionName = "等待选择"
	ev.Message = strings.Join(parts, "\n\n")
	return ev
}

// FromNotificationPayload 把 Notification 载荷翻译成对应的等待介入事件。
// 返回 false 表示该通知类型 acn 不推送（如 auth_success），调用方应静默跳过。
func FromNotificationPayload(p hook.NotificationPayload) (event.Event, bool) {
	ev := event.Event{
		Source:    event.SourceClaude,
		Cwd:       p.Cwd,
		SessionID: p.SessionID,
	}
	switch p.NotificationType {
	case "permission_prompt":
		ev.Kind = event.KindPermission
		ev.SessionName = "权限确认"
		ev.Message = strings.TrimSpace(p.Message)
		if ev.Message == "" {
			ev.Message = "Claude 正在等待权限审批。"
		}
	case "idle_prompt":
		ev.Kind = event.KindIdle
		ev.SessionName = "空闲等待"
		ev.Message = strings.TrimSpace(p.Message)
		if ev.Message == "" {
			ev.Message = "Claude 已完成回合，等待你的输入。"
		}
	default:
		// auth_success、elicitation_* 等类型要么无需行动，要么高频且嘈杂，
		// 一律不推。
		return event.Event{}, false
	}
	return ev, true
}
