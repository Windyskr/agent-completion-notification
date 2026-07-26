package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claude 的载荷只有基础字段，Codex 的是它的超集，同一个结构体都要吃得下。
func TestReadStopAcceptsBothSchemas(t *testing.T) {
	claude := `{"session_id":"s1","transcript_path":"/t.jsonl","cwd":"/work","hook_event_name":"Stop","stop_hook_active":false}`
	p, err := ReadStop(strings.NewReader(claude))
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "s1" || p.Cwd != "/work" || p.LastAssistantMessage != "" {
		t.Errorf("Claude 载荷解析有误: %+v", p)
	}

	codex := `{"session_id":"s2","transcript_path":"/r.jsonl","cwd":"/work","hook_event_name":"Stop",
	           "turn_id":"t9","stop_hook_active":false,"last_assistant_message":"完成了"}`
	p, err = ReadStop(strings.NewReader(codex))
	if err != nil {
		t.Fatal(err)
	}
	if p.TurnID != "t9" || p.LastAssistantMessage != "完成了" {
		t.Errorf("Codex 载荷解析有误: %+v", p)
	}
}

func TestReadStopRejectsBadJSON(t *testing.T) {
	for _, in := range []string{"", "not json", "[1,2]"} {
		if _, err := ReadStop(strings.NewReader(in)); err == nil {
			t.Errorf("ReadStop(%q) 应当报错", in)
		}
	}
}

func TestScanTailReadsAllLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(path, []byte("a\nb\n\nc\n"), 0o644)

	var got []string
	if err := ScanTail(path, MaxScanBytes, func(l string) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("扫描结果 = %v, 空行应被跳过", got)
	}
}

// 超出上限时只扫尾部，且必须丢弃截断处的半行——半行是非法 JSON，
// 留着会让调用方解析出垃圾。
func TestScanTailTruncatesAndDropsPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString(strings.Repeat("x", 100) + "\n")
	}
	b.WriteString("LAST\n")
	os.WriteFile(path, []byte(b.String()), 0o644)

	var got []string
	if err := ScanTail(path, 1000, func(l string) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[len(got)-1] != "LAST" {
		t.Fatalf("未读到最后一行，得到 %d 行", len(got))
	}
	for _, l := range got {
		if l != "LAST" && len(l) != 100 {
			t.Errorf("出现了被截断的半行（长度 %d）", len(l))
		}
	}
}

// 单行超过 bufio 默认 64KB 上限也不能中断扫描。
func TestScanTailHandlesHugeLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(path, []byte(strings.Repeat("x", 200_000)+"\nLAST\n"), 0o644)

	var last string
	if err := ScanTail(path, MaxScanBytes, func(l string) { last = l }); err != nil {
		t.Fatal(err)
	}
	if last != "LAST" {
		t.Errorf("超大行之后的记录被丢弃了，last = %q", last[:min(20, len(last))])
	}
}

func TestScanTailErrors(t *testing.T) {
	if err := ScanTail("", MaxScanBytes, func(string) {}); err == nil {
		t.Error("空路径应当报错")
	}
	if err := ScanTail("/nonexistent/x.jsonl", MaxScanBytes, func(string) {}); err == nil {
		t.Error("文件不存在应当报错")
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandHome("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandHome = %q", got)
	}
	if got := ExpandHome("/abs/x"); got != "/abs/x" {
		t.Errorf("绝对路径不应被改写，得到 %q", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
