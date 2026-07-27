// Package event 定义各 AI CLI 来源归一化之后的「任务完成」事件。
//
// 来源适配层（Claude Code 的 Stop hook、Codex 的 notify 回调）只负责把各自的
// 原始载荷翻译成 Event；daemon 与通知渠道只认识 Event。新增来源不必改动渠道，
// 新增渠道也不必改动来源。
package event

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	SourceClaude = "claude"
	SourceCodex  = "codex"
)

// messagePreviewRunes 限制推送正文里回复原文的长度，避免把整篇回答塞进通知。
const messagePreviewRunes = 500

// Event 是一次 AI CLI 任务完成。
type Event struct {
	Source     string `json:"source"`
	AgentName  string `json:"agent_name,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	// 使用 HideProjectName 是为了让 Event 零值与产品默认值一致：隐藏设备、显示项目。
	ShowDeviceName  bool   `json:"show_device_name,omitempty"`
	HideProjectName bool   `json:"hide_project_name,omitempty"`
	Cwd             string `json:"cwd"`
	Message         string `json:"message"`
	SessionID       string `json:"session_id"`
	// DurationMS 为 0 表示来源未提供耗时（Codex 的 notify 回调只有结束时刻，
	// 没有本轮起点）。此时耗时阈值不参与判断。
	DurationMS int64 `json:"duration_ms"`
}

// DisplayAgentName 返回适合放进连字符标题前缀的 Agent 名。
func (e Event) DisplayAgentName() string {
	if name := strings.TrimSpace(e.AgentName); name != "" {
		return name
	}
	switch e.Source {
	case SourceClaude:
		return "claude"
	case SourceCodex:
		return "Codex"
	default:
		return strings.TrimSpace(e.Source)
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

// Title 默认形如 "Codex-acn 任务完成"；设备名和项目名由显示设置控制。
func (e Event) Title() string {
	parts := make([]string, 0, 3)
	if e.ShowDeviceName {
		if device := strings.TrimSpace(e.DeviceName); device != "" {
			parts = append(parts, device)
		}
	}
	if agent := e.DisplayAgentName(); agent != "" {
		parts = append(parts, agent)
	}
	if !e.HideProjectName {
		if project := e.Project(); project != "" {
			parts = append(parts, project)
		}
	}
	if len(parts) == 0 {
		return "任务完成"
	}
	return strings.Join(parts, "-") + " 任务完成"
}

// Body 组装推送正文。渠道只负责传输，不再各自拼文案。
func (e Event) Body(now time.Time) string {
	lines := []string{"完成时间：" + now.Format("2006-01-02 15:04:05")}
	if d := FormatDuration(e.DurationMS); d != "" {
		lines = append(lines, "耗时："+d)
	}
	if cwd := strings.TrimSpace(e.Cwd); cwd != "" {
		lines = append(lines, "目录："+cwd)
	}
	if msg := truncate(strings.TrimSpace(e.Message), messagePreviewRunes); msg != "" {
		lines = append(lines, "", msg)
	}
	return strings.Join(lines, "\n")
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

// truncate 按字符（而非字节）截断，避免切断中文。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}
