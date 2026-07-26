// Package event 定义各 AI CLI 来源归一化之后的「任务完成」事件。
//
// 来源适配层（Claude Code 的 Stop hook、Codex 的 notify 回调）只负责把各自的
// 原始载荷翻译成 Event；daemon 与通知渠道只认识 Event。新增来源不必改动渠道，
// 新增渠道也不必改动来源。
package event

import (
	"crypto/sha256"
	"encoding/hex"
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

// fingerprintRunes 参与去重指纹的回复前缀长度。取前缀而非全文，是为了让
// 「同一次完成经由不同路径上报」仍能命中同一指纹。
const fingerprintRunes = 240

// Event 是一次 AI CLI 任务完成。
type Event struct {
	Source    string `json:"source"`
	Cwd       string `json:"cwd"`
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
	// DurationMS 为 0 表示来源未提供耗时（Codex 的 notify 回调只有结束时刻，
	// 没有本轮起点）。此时耗时阈值不参与判断。
	DurationMS int64 `json:"duration_ms"`
}

// Project 取工作目录名作为项目名。
func (e Event) Project() string {
	cwd := strings.TrimSpace(e.Cwd)
	if cwd == "" {
		return ""
	}
	return filepath.Base(cwd)
}

// SourceLabel 返回展示用的来源名。
func (e Event) SourceLabel() string {
	switch e.Source {
	case SourceClaude:
		return "Claude Code"
	case SourceCodex:
		return "Codex"
	default:
		return e.Source
	}
}

// Fingerprint 标识「同一次完成」。daemon 路径与 hook 直发路径可能先后触发，
// 靠它在时间窗口内压掉重复推送。
func (e Event) Fingerprint() string {
	raw := strings.Join([]string{e.Source, e.Cwd, truncate(collapse(e.Message), fingerprintRunes)}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

// Title 形如 "[Claude Code] acn 任务完成"。
func (e Event) Title() string {
	if p := e.Project(); p != "" {
		return fmt.Sprintf("[%s] %s 任务完成", e.SourceLabel(), p)
	}
	return fmt.Sprintf("[%s] 任务完成", e.SourceLabel())
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

// collapse 折叠所有连续空白，让换行差异不影响指纹。
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate 按字符（而非字节）截断，避免切断中文。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}
