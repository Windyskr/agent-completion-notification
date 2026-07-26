// Package claude 把 Claude Code 的 Stop hook 载荷翻译成通用 Event。
//
// Claude Code 在一轮对话结束时执行 Stop hook，并通过 stdin 传入 JSON：
//
//	{"session_id":"...","transcript_path":"...","cwd":"...","hook_event_name":"Stop"}
//
// 载荷本身不含回复正文与耗时，二者都从 transcript（JSONL）里取：
// 最后一条 assistant 的文本即回复；最近一条「真实用户输入」的时间戳即本轮起点。
// 因此只需安装 Stop 一个 hook，不必再装 UserPromptSubmit 记录起点。
package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/windyskr/acn/internal/event"
)

// maxScanBytes 限制 transcript 的扫描量。只关心最后一轮，从尾部截取即可，
// 避免超长会话把 hook 拖慢（hook 会阻塞 Claude Code 返回）。
const maxScanBytes = 8 << 20

// StopPayload 是 Stop hook 经 stdin 传入的载荷。
type StopPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// transcriptRow 是 transcript JSONL 的一行，只声明用得到的字段。
type transcriptRow struct {
	Type        string          `json:"type"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   string          `json:"timestamp"`
	Cwd         string          `json:"cwd"`
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

// Parse 读取 stdin 的 Stop 载荷并组装 Event。
func Parse(stdin io.Reader) (event.Event, error) {
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return event.Event{}, fmt.Errorf("读取 stdin 失败: %w", err)
	}
	var payload StopPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return event.Event{}, fmt.Errorf("解析 Stop 载荷失败: %w", err)
	}
	return FromPayload(payload), nil
}

// FromPayload 由 Stop 载荷组装 Event。transcript 读取失败时降级：
// 仍然产出事件，只是缺少回复正文与耗时。
func FromPayload(p StopPayload) event.Event {
	ev := event.Event{
		Source:    event.SourceClaude,
		Cwd:       p.Cwd,
		SessionID: p.SessionID,
	}

	digest, err := readTranscript(expandHome(p.TranscriptPath))
	if err != nil {
		return ev
	}
	ev.Message = digest.reply
	if ev.Cwd == "" {
		ev.Cwd = digest.cwd
	}
	if !digest.promptAt.IsZero() && digest.replyAt.After(digest.promptAt) {
		ev.DurationMS = digest.replyAt.Sub(digest.promptAt).Milliseconds()
	}
	return ev
}

// digest 是从 transcript 中提取的最后一轮信息。
type digest struct {
	reply    string
	replyAt  time.Time
	promptAt time.Time
	cwd      string
}

// readTranscript 正向扫描 JSONL，保留最后一条主线 assistant 文本与最近一次
// 真实用户输入的时间。正向扫描即可取到「最后」的记录，无需反向解析。
func readTranscript(path string) (digest, error) {
	var d digest
	if strings.TrimSpace(path) == "" {
		return d, fmt.Errorf("transcript 路径为空")
	}
	f, err := os.Open(path)
	if err != nil {
		return d, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return d, err
	}
	// 超长会话只扫尾部，把 hook 耗时控制在有界范围内。
	truncated := info.Size() > maxScanBytes
	if truncated {
		if _, err := f.Seek(info.Size()-maxScanBytes, io.SeekStart); err != nil {
			return d, err
		}
	}

	// 用 Reader 而非 Scanner：单行可能远超 Scanner 的 64KB 上限
	// （大 tool_result），Scanner 遇到超长行会中断并丢掉后续所有行。
	r := bufio.NewReaderSize(f, 256<<10)
	if truncated {
		// 截断点通常落在某行中间，丢弃这半行。
		if _, err := r.ReadString('\n'); err != nil && err != io.EOF {
			return d, err
		}
	}
	for {
		line, err := r.ReadString('\n')
		if s := strings.TrimSpace(line); s != "" {
			d.absorb(s)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return d, err
		}
	}
	return d, nil
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

// expandHome 展开路径开头的 ~。
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
