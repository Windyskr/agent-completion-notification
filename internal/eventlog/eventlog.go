// Package eventlog 将 hook 的完整处理过程追加到用户本地 JSONL 日志。
package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/notify"
)

// Entry 是一条 hook 调用的诊断记录。RawPayload 保留原始 stdin，便于排查
// 上游 CLI 的事件字段；日志文件权限为 0600，仍应避免将其发送到第三方。
type Entry struct {
	Timestamp       time.Time               `json:"timestamp"`
	HookSource      string                  `json:"hook_source"`
	RawPayload      string                  `json:"raw_payload"`
	PaseoAgentID    string                  `json:"paseo_agent_id,omitempty"`
	PaseoTerminalID string                  `json:"paseo_terminal_id,omitempty"`
	Event           *event.Event            `json:"event,omitempty"`
	Skip            bool                    `json:"skip"`
	SkippedReason   string                  `json:"skipped_reason,omitempty"`
	ParseError      string                  `json:"parse_error,omitempty"`
	ConfigError     string                  `json:"config_error,omitempty"`
	Delivery        []notify.DeliveryResult `json:"delivery,omitempty"`
	DeliveryError   string                  `json:"delivery_error,omitempty"`
	ElapsedMS       int64                   `json:"elapsed_ms"`
}

// Append 将一条记录追加到 JSONL 文件。每次 hook 只写一行，避免日志影响 stdout
// 控制协议。
func Append(path string, entry Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("序列化事件日志失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建事件日志目录失败: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开事件日志失败: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("设置事件日志权限失败: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("写入事件日志失败: %w", err)
	}
	return nil
}
