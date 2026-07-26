package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/windyskr/acn/internal/hook"
)

// writeRollout 写一份 Codex rollout JSONL。
func writeRollout(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func taskStarted(ts string) string {
	return `{"type":"event_msg","timestamp":"` + ts + `","payload":{"type":"task_started"}}`
}

func taskComplete(ts string) string {
	return `{"type":"event_msg","timestamp":"` + ts + `","payload":{"type":"task_complete"}}`
}

// 回复正文直接来自载荷，无需扫 transcript。
func TestFromPayloadUsesLastAssistantMessage(t *testing.T) {
	p := hook.StopPayload{
		SessionID:            "sess-1",
		Cwd:                  "/work/proj",
		HookEventName:        "Stop",
		LastAssistantMessage: "  改好了  ",
	}

	ev := FromPayload(p, time.Now())
	if ev.Source != "codex" {
		t.Errorf("来源 = %q", ev.Source)
	}
	if ev.Message != "改好了" {
		t.Errorf("正文 = %q, 期望去除首尾空白后的载荷内容", ev.Message)
	}
	if ev.Cwd != "/work/proj" || ev.SessionID != "sess-1" {
		t.Errorf("事件字段有误: %+v", ev)
	}
}

// 耗时由 rollout 里最后一条 task_started 算出——这是 notify 路径拿不到的信息。
func TestDurationFromLastTaskStarted(t *testing.T) {
	path := writeRollout(t,
		taskStarted("2026-07-26T10:00:00.000Z"),
		taskComplete("2026-07-26T10:01:00.000Z"),
		taskStarted("2026-07-26T10:05:00.000Z"), // 本轮起点
		`{"type":"response_item","timestamp":"2026-07-26T10:05:30.000Z","payload":{"type":"message"}}`,
	)
	now, _ := time.Parse(time.RFC3339, "2026-07-26T10:06:15.000Z")

	ev := FromPayload(hook.StopPayload{TranscriptPath: path, Cwd: "/work/proj"}, now)
	if ev.DurationMS != 75_000 {
		t.Errorf("耗时 = %dms, 期望 75000（取最后一条 task_started）", ev.DurationMS)
	}
}

// 没有 task_started 时耗时按未知处理，而不是算出个荒谬的值。
func TestDurationUnknownWithoutTaskStarted(t *testing.T) {
	path := writeRollout(t,
		`{"type":"session_meta","timestamp":"2026-07-26T10:00:00.000Z"}`,
		`{"type":"response_item","timestamp":"2026-07-26T10:00:30.000Z","payload":{"type":"message"}}`,
	)

	ev := FromPayload(hook.StopPayload{TranscriptPath: path}, time.Now())
	if ev.DurationMS != 0 {
		t.Errorf("耗时 = %dms, 期望 0", ev.DurationMS)
	}
}

// transcript 缺失时必须降级产出事件，而不是丢掉这次通知。
func TestFromPayloadDegradesOnMissingTranscript(t *testing.T) {
	ev := FromPayload(hook.StopPayload{
		SessionID:            "sess-2",
		TranscriptPath:       "/nonexistent/rollout.jsonl",
		Cwd:                  "/work/proj",
		LastAssistantMessage: "完成",
	}, time.Now())

	if ev.Message != "完成" || ev.Cwd != "/work/proj" {
		t.Errorf("降级事件应保留载荷内容: %+v", ev)
	}
	if ev.DurationMS != 0 {
		t.Errorf("耗时 = %d, 期望 0", ev.DurationMS)
	}
}

// 时钟回拨或时间戳异常时不得产出负耗时。
func TestNegativeDurationRejected(t *testing.T) {
	path := writeRollout(t, taskStarted("2026-07-26T10:05:00.000Z"))
	now, _ := time.Parse(time.RFC3339, "2026-07-26T10:00:00.000Z") // 早于起点

	if ev := FromPayload(hook.StopPayload{TranscriptPath: path}, now); ev.DurationMS != 0 {
		t.Errorf("耗时 = %d, 起点晚于终点时应为 0", ev.DurationMS)
	}
}

// 缺 session_id 时退回 turn_id。
func TestSessionIDFallsBackToTurnID(t *testing.T) {
	ev := FromPayload(hook.StopPayload{TurnID: "turn-9"}, time.Now())
	if ev.SessionID != "turn-9" {
		t.Errorf("会话 = %q, 期望退回 turn_id", ev.SessionID)
	}
}
