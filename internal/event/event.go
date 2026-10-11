// Package event 定义各 AI CLI 来源归一化之后的事件。
//
// 来源适配层（Claude Code / Codex 的 hook、OpenCode 插件）只负责把各自的原始
// 载荷翻译成 Event；notify 再按配置把 Event 渲染成通知交给渠道。新增来源不必
// 改动渠道，新增渠道也不必改动来源。
package event

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	SourceClaude   = "claude"
	SourceCodex    = "codex"
	SourceOpenCode = "opencode"
)

// 事件类别。空值与 KindCompletion 均表示任务完成；其余三类是 Claude Code
// 的「等待用户介入」事件——回合还没结束，但需要人回来做决策。
const (
	KindCompletion = ""
	KindChoice     = "choice"     // Claude 通过 AskUserQuestion 等待选择
	KindPermission = "permission" // Claude 等待工具权限审批
	KindIdle       = "idle"       // 回合结束后长时间无输入
	KindFailure    = "failure"    // Claude 因 API 错误终止回合
)

// DefaultMaxMessageLength 是推送正文中回复原文的默认最大字符数。
const DefaultMaxMessageLength = 1000

// Event 是一次 AI CLI 事件：任务完成，或回合中需要用户介入。它只描述发生了
// 什么，标题与正文如何展示由 notify 按配置渲染。
type Event struct {
	Source      string `json:"source"`
	Cwd         string `json:"cwd"`
	Message     string `json:"message"`
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name,omitempty"`
	// DurationMS 为 0 表示来源未提供耗时（如 transcript 里读不到本轮起点）。
	// 此时耗时阈值不参与判断。
	DurationMS int64 `json:"duration_ms"`
	// Kind 区分事件类别；空值表示任务完成。
	Kind string `json:"kind,omitempty"`
}

// AwaitingLabel 返回等待介入事件在通知标题中的类别词；任务完成返回空串。
func (e Event) AwaitingLabel() string {
	switch e.Kind {
	case KindChoice:
		return "等待选择"
	case KindPermission:
		return "权限确认"
	case KindIdle:
		return "空闲等待"
	default:
		return ""
	}
}

// Project 取工作目录名作为项目名。
func (e Event) Project() string {
	cwd := strings.TrimSpace(e.Cwd)
	if cwd == "" {
		return ""
	}
	return filepath.Base(cwd)
}

// FormatDuration 把毫秒格式化为人读的耗时；未知耗时返回空串。
func FormatDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	sec := ms / 1000
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm%ds", sec/60, sec%60)
	default:
		return fmt.Sprintf("%dh%dm", sec/3600, (sec%3600)/60)
	}
}
