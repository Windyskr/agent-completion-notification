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

// buildHookEvent 解析各 CLI 通过 stdin 发来的完成载荷并按来源归一化。
// Claude Code 与 Codex 共用 Stop 契约，OpenCode 使用 ACN 插件载荷。
func buildHookEvent(source string, stdin io.Reader) (event.Event, error) {
	switch source {
	case "claude":
		payload, err := hook.ReadStop(stdin)
		if err != nil {
			return event.Event{}, err
		}
		return claude.FromPayload(payload), nil
	case "codex":
		payload, err := hook.ReadStop(stdin)
		if err != nil {
			return event.Event{}, err
		}
		return codex.FromPayload(payload, time.Now()), nil
	case "opencode":
		return opencode.FromReader(stdin)
	default:
		return event.Event{}, fmt.Errorf("未知来源 %q", source)
	}
}
