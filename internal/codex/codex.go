// Package codex 把 Codex CLI 的 notify 回调翻译成通用 Event。
//
// Codex 在 ~/.codex/config.toml 里通过 notify 指定一个外部程序，一轮结束时执行：
//
//	notify_program [固定参数...] '<JSON 载荷>'
//
// JSON 恒为最后一个参数，形如：
//
//	{"type":"agent-turn-complete","turn-id":"...","thread-id":"...","cwd":"...",
//	 "input-messages":["..."],"last-assistant-message":"..."}
//
// 载荷不含本轮起始时间，故 Codex 事件的耗时始终未知（DurationMS 为 0）。
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/windyskr/acn/internal/event"
)

// TypeTurnComplete 是我们关心的唯一事件类型。
const TypeTurnComplete = "agent-turn-complete"

// forwardTimeout 限制链式转发原 notify 程序的等待时长。
const forwardTimeout = 30 * time.Second

// Payload 是 Codex notify 的 JSON 载荷。
type Payload struct {
	Type                 string   `json:"type"`
	TurnID               string   `json:"turn-id"`
	ThreadID             string   `json:"thread-id"`
	Cwd                  string   `json:"cwd"`
	InputMessages        []string `json:"input-messages"`
	LastAssistantMessage string   `json:"last-assistant-message"`
}

// Parse 从 notify 的命令行参数中解析载荷。args 为 acn 收到的全部尾随参数，
// 取最后一个可解析为 JSON 对象者。
func Parse(args []string) (Payload, error) {
	for i := len(args) - 1; i >= 0; i-- {
		var p Payload
		if err := json.Unmarshal([]byte(args[i]), &p); err == nil && p.Type != "" {
			return p, nil
		}
	}
	return Payload{}, fmt.Errorf("参数中未找到 Codex notify JSON 载荷")
}

// IsTurnComplete 判断是否为一轮结束事件。其余类型（若未来新增）直接忽略。
func (p Payload) IsTurnComplete() bool { return p.Type == TypeTurnComplete }

// ToEvent 组装通用事件。载荷缺 cwd 时退回进程工作目录——Codex 以自身
// 工作目录派生 notify 子进程，多数情况下二者一致。
func (p Payload) ToEvent() event.Event {
	cwd := strings.TrimSpace(p.Cwd)
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	message := strings.TrimSpace(p.LastAssistantMessage)
	if message == "" && len(p.InputMessages) > 0 {
		// 没有回复正文时，用用户本轮输入兜底，通知里至少能认出是哪个任务。
		message = strings.TrimSpace(p.InputMessages[len(p.InputMessages)-1])
	}
	return event.Event{
		Source:    event.SourceCodex,
		Cwd:       cwd,
		Message:   message,
		SessionID: firstNonEmpty(p.ThreadID, p.TurnID),
	}
}

// Forward 把原始参数原样转发给安装 acn 之前配置的 notify 程序。
//
// Codex 的 notify 只能配一个程序，acn 接管后必须代为转发，否则会静默破坏
// 用户已有的集成。转发失败只返回错误由调用方记录，不影响 acn 自身的推送。
func Forward(ctx context.Context, chain []string, args []string) error {
	if len(chain) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, forwardTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, chain[0], append(append([]string{}, chain[1:]...), args...)...)
	cmd.Stdout = os.Stderr // 保持 stdout 干净，子进程输出并入日志流
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("转发至 %s 失败: %w", chain[0], err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
