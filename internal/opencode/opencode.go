// Package opencode 把 OpenCode 插件发送的 session.idle 载荷翻译成通用 Event。
package opencode

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/event"
)

const maxPayloadBytes = 1 << 20

// Payload 是 acn 安装的 OpenCode 插件通过 stdin 发送的完成事件。
type Payload struct {
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	Cwd         string `json:"cwd"`
	Message     string `json:"message"`
	DurationMS  int64  `json:"duration_ms"`
}

// FromReader 读取插件载荷并组装 Event。
func FromReader(r io.Reader) (event.Event, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxPayloadBytes))
	if err != nil {
		return event.Event{}, fmt.Errorf("读取 stdin 失败: %w", err)
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return event.Event{}, fmt.Errorf("解析 OpenCode 载荷失败: %w", err)
	}
	return event.Event{
		Source:      event.SourceOpenCode,
		Cwd:         strings.TrimSpace(p.Cwd),
		Message:     strings.TrimSpace(p.Message),
		SessionID:   strings.TrimSpace(p.SessionID),
		SessionName: strings.TrimSpace(p.SessionName),
		DurationMS:  p.DurationMS,
	}, nil
}
