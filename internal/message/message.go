// Package message 定义渲染完成、可直接投递的通知。
//
// 事件（event）只描述发生了什么；标题与正文怎么写由 notify 按配置统一渲染成
// Message，渠道只负责按自身格式排版与传输，不再各自拼文案。
package message

import (
	"math"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Message 是一条渲染完成的通知。
type Message struct {
	// Source 是事件来源，供渠道选择图标、点击地址等来源相关的展示。
	Source string `json:"source"`
	// SessionID 供支持「同会话更新同一条通知」的渠道使用。
	SessionID string `json:"session_id,omitempty"`
	Title     string `json:"title"`
	// Reply 是 Agent 回复原文，已按 max-message-length 截断。
	Reply string `json:"reply,omitempty"`
	// Details 是完成时间、耗时、目录等详情，每项一行。
	Details []string `json:"details,omitempty"`
}

// Format 描述渠道对正文的排版与长度要求，零值表示纯文本、不限长度。
type Format struct {
	// Header 原样置于正文之前，用于把标题与正文合并进同一字段的渠道；它计入长度上限。
	Header string
	// HardLineBreaks 为 true 时，段落内的单换行改写成 Markdown 强制换行；
	// 否则多数 Markdown 渲染器会把相邻两行并成一行。
	HardLineBreaks bool
	// MaxBytes 是渠道允许的最大 UTF-8 字节数，0 表示不限。
	MaxBytes int
	// MaxUTF16 是渠道允许的最大 UTF-16 码元数（Telegram 的计数方式），0 表示不限。
	MaxUTF16 int
}

const ellipsis = "…"

// Body 按渠道格式拼出正文：回复原文、空行、详情行。
//
// 超出渠道上限的消息会被整条拒收，因此这里先截断：只截回复原文，时间、耗时与
// 目录等详情始终保留。max-message-length 是用户偏好，这里的上限是渠道硬约束。
func (m Message) Body(f Format) string {
	reply := strings.TrimSpace(m.Reply)
	details := strings.Join(m.Details, "\n")
	if f.HardLineBreaks {
		reply = hardLineBreaks(reply)
		details = hardLineBreaks(details)
	}
	compose := func(reply string) string {
		var parts []string
		if reply != "" {
			parts = append(parts, reply)
		}
		if details != "" {
			parts = append(parts, details)
		}
		return f.Header + strings.Join(parts, "\n\n")
	}

	body := compose(reply)
	if f.fits(body) {
		return body
	}
	// 回复原文换成省略号后的长度就是固定开销，剩余额度全部留给回复原文。
	minimal := compose(ellipsis)
	if !f.fits(minimal) {
		// 标题与详情本身就超限（如极长的目录），只能硬截断。
		return prefix(compose(""), f.MaxBytes, f.MaxUTF16)
	}
	byteBudget, utf16Budget := 0, 0
	if f.MaxBytes > 0 {
		byteBudget = f.MaxBytes - len(minimal)
	}
	if f.MaxUTF16 > 0 {
		utf16Budget = f.MaxUTF16 - utf16Len(minimal)
	}
	kept := strings.TrimRight(prefix(reply, byteBudget, utf16Budget), " \n")
	return compose(kept + ellipsis)
}

func (f Format) fits(s string) bool {
	return (f.MaxBytes <= 0 || len(s) <= f.MaxBytes) &&
		(f.MaxUTF16 <= 0 || utf16Len(s) <= f.MaxUTF16)
}

// prefix 返回 s 在两种计量下都不超限的最长前缀，只在字符边界截断，避免切坏中文。
// 上限为 0 表示该计量不限。
func prefix(s string, maxBytes, maxUTF16 int) string {
	if maxBytes <= 0 {
		maxBytes = math.MaxInt
	}
	if maxUTF16 <= 0 {
		maxUTF16 = math.MaxInt
	}
	units := 0
	for i, r := range s {
		units += utf16.RuneLen(r)
		if i+utf8.RuneLen(r) > maxBytes || units > maxUTF16 {
			return s[:i]
		}
	}
	return s
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// hardLineBreaks 保留正文段落，并让段落内的单换行符合 Markdown 强制换行语法。
func hardLineBreaks(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines)-1; i++ {
		if strings.TrimSpace(lines[i]) != "" &&
			strings.TrimSpace(lines[i+1]) != "" &&
			!strings.HasSuffix(lines[i], "  ") {
			lines[i] += "  "
		}
	}
	return strings.Join(lines, "\n")
}
