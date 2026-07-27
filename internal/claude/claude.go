// Package claude 把 Claude Code 的 Stop hook 载荷翻译成通用 Event。
//
// 载荷本身不含回复正文与耗时（这点与 Codex 不同），二者都从 transcript（JSONL）
// 里取：最后一条 assistant 的文本即回复；最近一条「真实用户输入」的时间戳即本轮
// 起点。因此只需安装 Stop 一个 hook，不必再装 UserPromptSubmit 记录起点。
package claude

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/hook"
)

// transcriptRow 是 transcript JSONL 的一行，只声明用得到的字段。
type transcriptRow struct {
	Type        string          `json:"type"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   string          `json:"timestamp"`
	Cwd         string          `json:"cwd"`
	AITitle     string          `json:"aiTitle"`
	Message     json.RawMessage `json:"message"`
}

// message 是 transcriptRow.Message 的结构。content 既可能是字符串，
// 也可能是 block 数组，故用 RawMessage 延后解析。
type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// FromPayload 由 Stop 载荷组装 Event。transcript 读取失败时降级：
// 仍然产出事件，只是缺少回复正文与耗时。
func FromPayload(p hook.StopPayload) event.Event {
	ev := event.Event{
		Source:    event.SourceClaude,
		Cwd:       p.Cwd,
		SessionID: p.SessionID,
	}

	var d digest
	if err := hook.ScanTail(p.TranscriptPath, hook.MaxScanBytes, d.absorb); err != nil {
		return ev
	}
	ev.Message = d.reply
	ev.SessionName = d.sessionName
	if ev.Cwd == "" {
		ev.Cwd = d.cwd
	}
	if !d.promptAt.IsZero() && d.replyAt.After(d.promptAt) {
		ev.DurationMS = d.replyAt.Sub(d.promptAt).Milliseconds()
	}
	return ev
}

// digest 是从 transcript 中提取的最后一轮信息。正向扫描即可取到「最后」的记录，
// 无需反向解析。
type digest struct {
	reply       string
	replyAt     time.Time
	promptAt    time.Time
	cwd         string
	sessionName string
}

// absorb 解析一行并更新 digest。无法解析的行直接忽略。
func (d *digest) absorb(line string) {
	var row transcriptRow
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		return
	}
	// 子代理（Task 工具）的对话不是主线回复，跳过。
	if row.IsSidechain {
		return
	}
	if row.Cwd != "" {
		d.cwd = row.Cwd
	}
	ts, _ := time.Parse(time.RFC3339, row.Timestamp)

	switch row.Type {
	case "ai-title":
		if title := strings.TrimSpace(row.AITitle); title != "" {
			d.sessionName = title
		}
	case "assistant":
		if text := extractText(row.Message); text != "" {
			d.reply = text
			d.replyAt = ts
		}
	case "user":
		// 只有真实用户输入才算本轮起点；tool_result 是模型自己触发的回填。
		if isUserPrompt(row.Message) && !ts.IsZero() {
			d.promptAt = ts
		}
	}
}

// extractText 拼接 assistant 消息里的所有 text block，忽略 tool_use 等。
func extractText(raw json.RawMessage) string {
	blocks, ok := decodeContent(raw)
	if !ok {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			if t := strings.TrimSpace(b.Text); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// isUserPrompt 判断 user 消息是否为真实输入：content 为字符串，
// 或 block 数组中不含 tool_result。
func isUserPrompt(raw json.RawMessage) bool {
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return strings.TrimSpace(s) != ""
	}
	var blocks []contentBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
	}
	return len(blocks) > 0
}

// decodeContent 把 message.content 统一成 block 数组。
func decodeContent(raw json.RawMessage) ([]contentBlock, bool) {
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	var blocks []contentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		return blocks, true
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return []contentBlock{{Type: "text", Text: s}}, true
	}
	return nil, false
}
