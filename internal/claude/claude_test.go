package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTranscript 把若干 JSONL 行写入临时 transcript。
func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// userLine 构造一条真实用户输入。
func userLine(ts, text string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"cwd":"/work/proj","message":{"role":"user","content":%q}}`, ts, text)
}

// toolResultLine 构造一条 tool_result 回填——它不是用户输入，不能当作本轮起点。
func toolResultLine(ts string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"cwd":"/work/proj","message":{"role":"user","content":[{"type":"tool_result","text":"output"}]}}`, ts)
}

func assistantLine(ts, text string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"cwd":"/work/proj","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, ts, text)
}

func TestFromPayloadExtractsReplyAndDuration(t *testing.T) {
	path := writeTranscript(t,
		userLine("2026-07-26T10:00:00.000Z", "帮我改个 bug"),
		assistantLine("2026-07-26T10:00:05.000Z", "我先看看代码"),
		toolResultLine("2026-07-26T10:00:20.000Z"),
		assistantLine("2026-07-26T10:00:30.000Z", "改好了"),
	)

	ev := FromPayload(StopPayload{
		SessionID:      "sess-1",
		TranscriptPath: path,
		Cwd:            "/work/proj",
	})

	if ev.Message != "改好了" {
		t.Errorf("回复 = %q, 期望最后一条 assistant 文本", ev.Message)
	}
	// 起点为真实用户输入（10:00:00），而非 tool_result（10:00:20）。
	if ev.DurationMS != 30_000 {
		t.Errorf("耗时 = %dms, 期望 30000（起点应为真实用户输入）", ev.DurationMS)
	}
	if ev.SessionID != "sess-1" || ev.Cwd != "/work/proj" {
		t.Errorf("事件字段有误: %+v", ev)
	}
}

// 只有 tool_result 而无真实用户输入时（例如续跑），耗时应为未知而非算出个荒谬的值。
func TestDurationUnknownWithoutUserPrompt(t *testing.T) {
	path := writeTranscript(t,
		toolResultLine("2026-07-26T10:00:00.000Z"),
		assistantLine("2026-07-26T10:00:30.000Z", "完成"),
	)

	ev := FromPayload(StopPayload{TranscriptPath: path, Cwd: "/work/proj"})
	if ev.DurationMS != 0 {
		t.Errorf("耗时 = %dms, 期望 0（无法确定起点）", ev.DurationMS)
	}
	if ev.Message != "完成" {
		t.Errorf("回复 = %q", ev.Message)
	}
}

// 子代理（Task 工具）的对话不是主线回复。
func TestSidechainIgnored(t *testing.T) {
	path := writeTranscript(t,
		userLine("2026-07-26T10:00:00.000Z", "查一下"),
		assistantLine("2026-07-26T10:00:10.000Z", "主线回复"),
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-07-26T10:00:20.000Z","message":{"role":"assistant","content":[{"type":"text","text":"子代理回复"}]}}`,
	)

	ev := FromPayload(StopPayload{TranscriptPath: path, Cwd: "/work/proj"})
	if ev.Message != "主线回复" {
		t.Errorf("回复 = %q, 期望主线回复（子代理应被忽略）", ev.Message)
	}
}

// 多个 text block 应拼接；tool_use 应跳过。
func TestExtractTextJoinsBlocksSkipsToolUse(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":[
		{"type":"text","text":"第一段"},
		{"type":"tool_use","name":"Bash","text":"不该出现"},
		{"type":"text","text":"第二段"}
	]}`)

	got := extractText(raw)
	if got != "第一段\n第二段" {
		t.Errorf("extractText = %q, 期望拼接两段 text 且跳过 tool_use", got)
	}
}

// content 为纯字符串的 assistant 消息也要能取到。
func TestExtractTextFromStringContent(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":"直接是字符串"}`)
	if got := extractText(raw); got != "直接是字符串" {
		t.Errorf("extractText = %q", got)
	}
}

func TestIsUserPrompt(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"字符串输入", `{"role":"user","content":"你好"}`, true},
		{"空字符串", `{"role":"user","content":"   "}`, false},
		{"文本 block", `{"role":"user","content":[{"type":"text","text":"你好"}]}`, true},
		{"tool_result", `{"role":"user","content":[{"type":"tool_result","text":"x"}]}`, false},
		{"混合含 tool_result", `{"role":"user","content":[{"type":"text","text":"a"},{"type":"tool_result","text":"x"}]}`, false},
		{"空数组", `{"role":"user","content":[]}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isUserPrompt(json.RawMessage(c.raw)); got != c.want {
				t.Errorf("isUserPrompt = %v, 期望 %v", got, c.want)
			}
		})
	}
}

// transcript 不可读时必须降级产出事件，而不是丢掉这次通知。
func TestFromPayloadDegradesOnMissingTranscript(t *testing.T) {
	ev := FromPayload(StopPayload{
		SessionID:      "sess-2",
		TranscriptPath: "/nonexistent/transcript.jsonl",
		Cwd:            "/work/proj",
	})

	if ev.Source != "claude" || ev.Cwd != "/work/proj" || ev.SessionID != "sess-2" {
		t.Errorf("降级事件字段有误: %+v", ev)
	}
	if ev.Message != "" || ev.DurationMS != 0 {
		t.Errorf("无 transcript 时不应有正文或耗时: %+v", ev)
	}
}

// 载荷缺 cwd 时应从 transcript 里补齐。
func TestCwdFallbackFromTranscript(t *testing.T) {
	path := writeTranscript(t,
		userLine("2026-07-26T10:00:00.000Z", "hi"),
		assistantLine("2026-07-26T10:00:10.000Z", "done"),
	)

	ev := FromPayload(StopPayload{TranscriptPath: path})
	if ev.Cwd != "/work/proj" {
		t.Errorf("cwd = %q, 期望从 transcript 补齐为 /work/proj", ev.Cwd)
	}
}

// 单行超过 bufio 默认 64KB 上限时不能中断扫描——大 tool_result 很常见，
// 用 Scanner 会在这里丢掉后续所有行。
func TestHugeLineDoesNotBreakScan(t *testing.T) {
	huge := fmt.Sprintf(`{"type":"user","timestamp":"2026-07-26T10:00:00.000Z","cwd":"/work/proj","message":{"role":"user","content":[{"type":"tool_result","text":%q}]}}`,
		strings.Repeat("x", 200_000))

	path := writeTranscript(t,
		userLine("2026-07-26T09:59:00.000Z", "开始"),
		huge,
		assistantLine("2026-07-26T10:00:30.000Z", "超大行之后的回复"),
	)

	ev := FromPayload(StopPayload{TranscriptPath: path, Cwd: "/work/proj"})
	if ev.Message != "超大行之后的回复" {
		t.Errorf("回复 = %q, 超大行之后的记录被丢弃了", ev.Message)
	}
}

func TestParseReadsStdin(t *testing.T) {
	path := writeTranscript(t,
		userLine("2026-07-26T10:00:00.000Z", "hi"),
		assistantLine("2026-07-26T10:00:10.000Z", "done"),
	)
	stdin := strings.NewReader(fmt.Sprintf(
		`{"session_id":"s","transcript_path":%q,"cwd":"/work/proj","hook_event_name":"Stop"}`, path))

	ev, err := Parse(stdin)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Message != "done" || ev.DurationMS != 10_000 {
		t.Errorf("事件 = %+v", ev)
	}
}

func TestParseRejectsBadJSON(t *testing.T) {
	if _, err := Parse(strings.NewReader("not json")); err == nil {
		t.Error("非法 JSON 应当报错")
	}
}
