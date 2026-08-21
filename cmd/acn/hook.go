package main

import (
	"fmt"
	"io"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/claude"
	"github.com/windyskr/agent-completion-notification/internal/codex"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/hook"
	"github.com/windyskr/agent-completion-notification/internal/opencode"
)

// buildHookEvent 解析各 CLI 通过 stdin 发来的载荷并按来源归一化。
// Claude Code 与 Codex 共用 Stop 契约，OpenCode 使用 ACN 插件载荷；
// claude-question / claude-notification 是 Claude Code 回合中的等待介入事件。
// skip 为 true 表示该载荷无需推送（如无需行动的通知类型），调用方应静默返回。
func buildHookEvent(source string, stdin io.Reader) (ev event.Event, skip bool, err error) {
	switch source {
	case "claude":
		payload, err := hook.ReadStop(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return claude.FromPayload(payload), false, nil
	case "claude-question":
		payload, err := hook.ReadPreToolUse(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return claude.FromQuestionPayload(payload), false, nil
	case "claude-notification":
		payload, err := hook.ReadNotification(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		ev, ok := claude.FromNotificationPayload(payload)
		return ev, !ok, nil
	case "codex":
		payload, err := hook.ReadStop(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return codex.FromPayload(payload, time.Now()), false, nil
	case "opencode":
		ev, err := opencode.FromReader(stdin)
		return ev, false, err
	default:
		return event.Event{}, false, fmt.Errorf("未知来源 %q", source)
	}
}
