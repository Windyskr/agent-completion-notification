// Package message 定义渲染完成、可直接投递的通知。
//
// 事件（event）只描述发生了什么；标题与正文怎么写由 notify 按配置统一渲染成
// Message，渠道只负责按自身格式排版与传输，不再各自拼文案。
package message

import "strings"

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

// Format 描述渠道对正文的排版要求，零值表示纯文本。
type Format struct {
	// Header 原样置于正文之前，用于把标题与正文合并进同一字段的渠道。
	Header string
	// HardLineBreaks 为 true 时，段落内的单换行改写成 Markdown 强制换行；
	// 否则多数 Markdown 渲染器会把相邻两行并成一行。
	HardLineBreaks bool
}

// Body 按渠道格式拼出正文：回复原文、空行、详情行。
func (m Message) Body(f Format) string {
	var parts []string
	if reply := strings.TrimSpace(m.Reply); reply != "" {
		parts = append(parts, reply)
	}
	if len(m.Details) > 0 {
		parts = append(parts, strings.Join(m.Details, "\n"))
	}
	body := strings.Join(parts, "\n\n")
	if f.HardLineBreaks {
		body = hardLineBreaks(body)
	}
	return f.Header + body
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
