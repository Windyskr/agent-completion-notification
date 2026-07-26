package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 真实载荷：JSON 是最后一个参数，前面还有固定参数。
func TestParseTakesTrailingJSON(t *testing.T) {
	args := []string{"turn-ended", `{"type":"agent-turn-complete","turn-id":"t1","thread-id":"th1","cwd":"/work/proj","input-messages":["改个 bug"],"last-assistant-message":"改好了"}`}

	p, err := Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsTurnComplete() {
		t.Errorf("type = %q, 期望识别为一轮结束", p.Type)
	}

	ev := p.ToEvent()
	if ev.Source != "codex" || ev.Cwd != "/work/proj" || ev.Message != "改好了" {
		t.Errorf("事件 = %+v", ev)
	}
	if ev.SessionID != "th1" {
		t.Errorf("会话 = %q, 期望优先取 thread-id", ev.SessionID)
	}
	// Codex 载荷没有起始时间，耗时必须是未知，否则会算出错误值。
	if ev.DurationMS != 0 {
		t.Errorf("耗时 = %d, Codex 载荷无起点应为 0", ev.DurationMS)
	}
}

// 没有回复正文时用用户输入兜底，通知里至少能认出是哪个任务。
func TestToEventFallsBackToInputMessage(t *testing.T) {
	p := Payload{
		Type:          TypeTurnComplete,
		Cwd:           "/work/proj",
		InputMessages: []string{"第一条", "最后一条"},
	}
	if got := p.ToEvent().Message; got != "最后一条" {
		t.Errorf("正文 = %q, 期望回退到最后一条用户输入", got)
	}
}

// cwd 缺失时退回进程工作目录。
func TestToEventFallsBackToProcessCwd(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p := Payload{Type: TypeTurnComplete, LastAssistantMessage: "done"}
	if got := p.ToEvent().Cwd; got != wd {
		t.Errorf("cwd = %q, 期望退回进程工作目录 %q", got, wd)
	}
}

// 非一轮结束的事件类型要能被识别出来并忽略。
func TestOtherEventTypeNotTurnComplete(t *testing.T) {
	p, err := Parse([]string{`{"type":"some-other-event","cwd":"/x"}`})
	if err != nil {
		t.Fatal(err)
	}
	if p.IsTurnComplete() {
		t.Error("非 agent-turn-complete 被误判为一轮结束")
	}
}

func TestParseRejectsMissingJSON(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"turn-ended"},
		{"not json at all"},
		{`{"no_type_field":1}`}, // 缺 type 视为不可用
	} {
		if _, err := Parse(args); err == nil {
			t.Errorf("Parse(%q) 应当报错", args)
		}
	}
}

// 链式转发必须把原始参数原样传给上游程序——这是不破坏用户既有集成的关键。
func TestForwardPassesArgsThrough(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "got.txt")
	script := filepath.Join(dir, "upstream.sh")

	// 把收到的参数逐行写入文件，供断言检查。
	body := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + out + "\ndone\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	args := []string{"json-payload-here"}
	if err := Forward(context.Background(), []string{script, "turn-ended"}, args); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("上游程序未被调用: %v", err)
	}
	// 固定参数在前，acn 收到的参数在后。
	if string(data) != "turn-ended\njson-payload-here\n" {
		t.Errorf("上游收到的参数 = %q", data)
	}
}

// 未配置上游时转发是空操作，不应报错。
func TestForwardNoChainIsNoop(t *testing.T) {
	if err := Forward(context.Background(), nil, []string{"x"}); err != nil {
		t.Errorf("空链转发应为空操作，却返回 %v", err)
	}
}

// 上游程序失败必须返回错误让调用方记录，但不能 panic。
func TestForwardReportsFailure(t *testing.T) {
	if err := Forward(context.Background(), []string{"/nonexistent/prog"}, []string{"x"}); err == nil {
		t.Error("上游不存在时应当返回错误")
	}
}
