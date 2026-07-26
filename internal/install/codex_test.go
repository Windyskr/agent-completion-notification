package install

import (
	"strings"
	"testing"
)

// 真实用户配置的形状：顶层键 + 空行 + 已被占用的 notify + 若干表。
const sampleConfig = `model_provider = "OpenAI"
model = "gpt-5.6-sol"

notify = ["/Applications/Codex.app/SkyComputerUseClient", "turn-ended"]

[model_providers]
[model_providers.OpenAI]
name = "404 Not Found"

# 用户的注释
[mcp_servers.node_repl]
command = "/path/to/node_repl"
`

func TestNotifyValueReadsExistingChain(t *testing.T) {
	got, found := parseTOMLLines(sampleConfig).notifyValue()
	if !found {
		t.Fatal("未能定位 notify")
	}
	want := []string{"/Applications/Codex.app/SkyComputerUseClient", "turn-ended"}
	if len(got) != len(want) {
		t.Fatalf("链长度 = %d, 期望 %d (%q)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("链[%d] = %q, 期望 %q", i, got[i], want[i])
		}
	}
}

// setNotify 只能动 notify 那一行，用户的注释、空行与表结构必须原样存活。
func TestSetNotifyPreservesEverythingElse(t *testing.T) {
	out := parseTOMLLines(sampleConfig).setNotify([]string{"/usr/local/bin/acn", "hook", "codex"})

	if !strings.Contains(out, `notify = ["/usr/local/bin/acn", "hook", "codex"]`) {
		t.Errorf("notify 未被正确改写:\n%s", out)
	}
	if strings.Contains(out, "SkyComputerUseClient") {
		t.Error("旧 notify 未被替换")
	}
	for _, keep := range []string{
		`model = "gpt-5.6-sol"`,
		"[model_providers.OpenAI]",
		"# 用户的注释",
		"[mcp_servers.node_repl]",
		`name = "404 Not Found"`,
	} {
		if !strings.Contains(out, keep) {
			t.Errorf("丢失了原有内容 %q", keep)
		}
	}
	// 行数不变：替换而非追加。
	if a, b := len(splitLines(sampleConfig)), len(splitLines(out)); a != b {
		t.Errorf("行数 %d → %d，应保持不变", a, b)
	}
}

// 没有 notify 时必须插到首个表头之前，否则会被解析成该表的字段。
func TestSetNotifyInsertsBeforeFirstTable(t *testing.T) {
	src := "model = \"gpt-5\"\n\n[mcp_servers]\nfoo = 1\n"
	out := parseTOMLLines(src).setNotify([]string{"/bin/acn", "hook", "codex"})

	notifyAt := strings.Index(out, "notify =")
	tableAt := strings.Index(out, "[mcp_servers]")
	if notifyAt < 0 {
		t.Fatalf("未插入 notify:\n%s", out)
	}
	if notifyAt > tableAt {
		t.Errorf("notify 落到了表头之后，会被解析成表字段:\n%s", out)
	}
}

func TestRemoveNotifyDropsOnlyThatLine(t *testing.T) {
	out := parseTOMLLines(sampleConfig).removeNotify()

	if strings.Contains(out, "notify") {
		t.Errorf("notify 未被删除:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.node_repl]") {
		t.Error("删除时误伤了其它内容")
	}
	if a, b := len(splitLines(sampleConfig)), len(splitLines(out)); a-b != 1 {
		t.Errorf("行数 %d → %d，应只少一行", a, b)
	}
}

// 跨行数组必须被完整识别，否则替换会留下半个数组，把 config.toml 弄成非法 TOML。
func TestMultilineNotifyArray(t *testing.T) {
	src := "notify = [\n  \"/path/to/prog\",\n  \"arg\",\n]\n\n[table]\n"
	doc := parseTOMLLines(src)

	got, found := doc.notifyValue()
	if !found {
		t.Fatal("未能定位跨行 notify")
	}
	if len(got) != 2 || got[0] != "/path/to/prog" || got[1] != "arg" {
		t.Fatalf("解析结果 = %q", got)
	}

	out := doc.setNotify([]string{"/bin/acn"})
	if strings.Count(out, "notify") != 1 {
		t.Errorf("跨行数组未被整体替换:\n%s", out)
	}
	if strings.Contains(out, "/path/to/prog") {
		t.Errorf("残留旧值:\n%s", out)
	}
	if strings.Contains(out, "]\n\n[table]\n") && strings.Contains(out, "  \"arg\",") {
		t.Errorf("残留半个数组:\n%s", out)
	}
}

// 注释掉的 notify 不能被当成真配置。
func TestCommentedNotifyIgnored(t *testing.T) {
	src := "# notify = [\"/old/prog\"]\nmodel = \"gpt-5\"\n"
	if _, found := parseTOMLLines(src).notifyValue(); found {
		t.Error("注释行被误判为 notify 配置")
	}
}

// 带空格的路径与转义必须能往返，macOS 的 .app 路径里空格很常见。
func TestQuotingRoundTrip(t *testing.T) {
	original := []string{`/Users/me/Codex Computer Use.app/prog`, `turn-ended`}
	src := "notify = " + formatTOMLStringArray(original) + "\n"

	got, found := parseTOMLLines(src).notifyValue()
	if !found {
		t.Fatal("未能解析回写的值")
	}
	if len(got) != 2 || got[0] != original[0] || got[1] != original[1] {
		t.Errorf("往返后 = %q, 期望 %q", got, original)
	}
}

func TestIsACNChain(t *testing.T) {
	cases := []struct {
		chain []string
		want  bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"/usr/local/bin/acn", "hook", "codex"}, true},
		{[]string{"/opt/homebrew/Cellar/acn/0.1.0/bin/acn", "hook", "codex"}, true},
		{[]string{"/Applications/Codex.app/SkyComputerUseClient", "turn-ended"}, false},
	}
	for _, c := range cases {
		if got := isACNChain(c.chain); got != c.want {
			t.Errorf("isACNChain(%q) = %v, 期望 %v", c.chain, got, c.want)
		}
	}
}

func TestBracketsBalanced(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`["a", "b"]`, true},
		{`[`, false},
		{`["a",`, false},
		{`["with ] bracket"]`, true}, // 字符串内的括号不计
		{`["a"] # 注释 [`, true},       // 行内注释后的括号不计
		{`["esc \" quote"]`, true},
	}
	for _, c := range cases {
		if got := bracketsBalanced(c.in); got != c.want {
			t.Errorf("bracketsBalanced(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}
}
