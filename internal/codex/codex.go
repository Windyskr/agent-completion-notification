// Package codex 把 Codex CLI 的 Stop hook 载荷翻译成通用 Event。
//
// Codex 在 config.toml 里以 [[hooks.Stop]] 注册命令，一轮结束时执行并通过 stdin
// 传入 JSON。这是 Codex 的现行 hooks 引擎；顶层 notify 是它的遗留路径
// （二进制里那个文件就叫 legacy_notify.rs），且全局只有一个槽位，会与用户已有的
// 集成（如 Codex Computer Use）相互覆盖，故不采用。
//
// 载荷直接给出 last_assistant_message，回复正文无需扫 transcript；耗时则由
// rollout 里最后一条 task_started 算出。
package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/hook"
)

// eventTaskStarted 是 Codex 标记「本轮开始」的事件，与 task_complete 成对出现。
// 拿它当起点比「找最近一条用户输入」更准——用户敲下回车与模型真正开跑之间
// 可能隔着审批等待。
const eventTaskStarted = "task_started"

// rolloutRow 是 rollout JSONL 的一行，只声明用得到的字段。
type rolloutRow struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type string `json:"type"`
	} `json:"payload"`
}

// FromPayload 由 Stop 载荷组装 Event。
func FromPayload(p hook.StopPayload, now time.Time) event.Event {
	ev := event.Event{
		Source:    event.SourceCodex,
		Cwd:       p.Cwd,
		Message:   strings.TrimSpace(p.LastAssistantMessage),
		SessionID: firstNonEmpty(p.SessionID, p.TurnID),
	}
	ev.SessionName = sessionName(ev.SessionID)
	if started, ok := lastTaskStart(p.TranscriptPath); ok && now.After(started) {
		ev.DurationMS = now.Sub(started).Milliseconds()
	}
	return ev
}

// sessionIndexRow 是 ~/.codex/session_index.jsonl 的一条会话索引记录。
// 同一会话重命名后可能出现多条记录，因此扫描时保留最后一个匹配名称。
type sessionIndexRow struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
}

func sessionName(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}

	dir := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".codex")
	}

	f, err := os.Open(filepath.Join(dir, "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer f.Close()

	var name string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var row sessionIndexRow
		if json.Unmarshal(scanner.Bytes(), &row) == nil && row.ID == sessionID {
			if candidate := strings.TrimSpace(row.ThreadName); candidate != "" {
				name = candidate
			}
		}
	}
	return name
}

// lastTaskStart 返回 rollout 中最后一条 task_started 的时间。
//
// Stop hook 触发时 task_complete 未必已落盘，因此以「现在」作为终点，
// 误差在毫秒级。transcript 不可读时返回 false，耗时按未知处理。
func lastTaskStart(path string) (time.Time, bool) {
	var latest time.Time
	err := hook.ScanTail(path, hook.MaxScanBytes, func(line string) {
		var row rolloutRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return
		}
		if row.Type != "event_msg" || row.Payload.Type != eventTaskStarted {
			return
		}
		if ts, err := time.Parse(time.RFC3339, row.Timestamp); err == nil {
			latest = ts
		}
	})
	if err != nil || latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
