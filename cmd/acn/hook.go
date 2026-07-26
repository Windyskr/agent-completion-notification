package main

import (
	"fmt"
	"io"
	"time"

	"github.com/windyskr/acn/internal/claude"
	"github.com/windyskr/acn/internal/codex"
	"github.com/windyskr/acn/internal/event"
	"github.com/windyskr/acn/internal/hook"
)

// buildHookEvent 解析 Stop 载荷并按来源归一化。
//
// 两个 CLI 用的是同一套 Stop hook 契约，载荷都从 stdin 进来，差别仅在于
// 回复正文与耗时的取法。
func buildHookEvent(source string, stdin io.Reader) (event.Event, error) {
	payload, err := hook.ReadStop(stdin)
	if err != nil {
		return event.Event{}, err
	}

	switch source {
	case "claude":
		return claude.FromPayload(payload), nil
	case "codex":
		return codex.FromPayload(payload, time.Now()), nil
	default:
		return event.Event{}, fmt.Errorf("未知来源 %q", source)
	}
}
