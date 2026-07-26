package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome 把 HOME 指向临时目录，让安装函数改写沙箱里的 settings.json
// 而不是开发者真实的配置。
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ACN_CONFIG_DIR", filepath.Join(home, "acn-config"))
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func writeSettings(t *testing.T, home, content string) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("产出的 settings.json 不是合法 JSON: %v\n%s", err, data)
	}
	return out
}

// 安装必须保留用户既有的所有字段——这个文件里有 env、权限等要紧配置。
func TestInstallClaudePreservesExistingKeys(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{
  "env": {"ANTHROPIC_BASE_URL": "https://example.com", "FOO": "bar"},
  "skipDangerousModePermissionPrompt": true
}`)

	if err := installClaude("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}

	got := readSettings(t, path)
	env, ok := got["env"].(map[string]any)
	if !ok {
		t.Fatalf("env 字段丢失: %v", got)
	}
	if env["ANTHROPIC_BASE_URL"] != "https://example.com" || env["FOO"] != "bar" {
		t.Errorf("env 内容被改动: %v", env)
	}
	if got["skipDangerousModePermissionPrompt"] != true {
		t.Error("skipDangerousModePermissionPrompt 丢失")
	}
	if !queryClaude().Installed {
		t.Error("安装后状态仍为未安装")
	}
}

// 重复安装不能堆积条目。
func TestInstallClaudeIdempotent(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{}`)

	for i := 0; i < 3; i++ {
		if err := installClaude("/usr/local/bin/acn"); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := readClaudeGroups(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("重复安装产生了 %d 个分组，应为 1", len(groups))
	}
}

// 卸载要把文件还原成安装前的样子：空的 hooks 容器也要清掉。
func TestUninstallClaudeRestoresOriginalShape(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{"env": {"FOO": "bar"}}`)

	if err := installClaude("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if err := uninstallClaude(); err != nil {
		t.Fatal(err)
	}

	got := readSettings(t, path)
	if _, exists := got["hooks"]; exists {
		t.Errorf("卸载后仍残留空的 hooks 容器: %v", got)
	}
	if env := got["env"].(map[string]any); env["FOO"] != "bar" {
		t.Error("卸载时误伤了 env")
	}
	if queryClaude().Installed {
		t.Error("卸载后状态仍为已安装")
	}
}

// 用户自己的 Stop hook 必须活下来——这是最容易造成实际损失的场景。
func TestUninstallClaudeKeepsUserHooks(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "/usr/bin/say done"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "/my/audit"}]}]
  }
}`)

	if err := installClaude("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if err := uninstallClaude(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "/usr/bin/say done") {
		t.Errorf("用户的 Stop hook 被删掉了:\n%s", text)
	}
	if !strings.Contains(text, "/my/audit") {
		t.Errorf("用户的 PreToolUse hook 被删掉了:\n%s", text)
	}
	if strings.Contains(text, "hook claude") {
		t.Errorf("acn 自己的条目未被清除:\n%s", text)
	}

	groups, err := readClaudeGroups(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Errorf("Stop 分组数 = %d, 期望 1（仅用户自己那条）", len(groups))
	}
}

// 同一分组内混有用户条目与 acn 条目时，只摘 acn 那一条。
func TestStripACNGroupsHandlesMixedGroup(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"hooks":[{"type":"command","command":"/usr/bin/say hi"},{"type":"command","command":"'/usr/local/bin/acn' hook claude"}]}`),
	}
	kept, found := stripACNGroups(raw)
	if !found {
		t.Fatal("未识别出 acn 条目")
	}
	if len(kept) != 1 {
		t.Fatalf("分组数 = %d, 期望 1", len(kept))
	}

	var g hookGroup
	if err := json.Unmarshal(kept[0], &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Hooks) != 1 || g.Hooks[0].Command != "/usr/bin/say hi" {
		t.Errorf("重写后的分组 = %+v, 应只保留用户那条", g.Hooks)
	}
}

// 配置文件不存在时首次安装应当直接创建。
func TestInstallClaudeCreatesMissingFile(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".claude", "settings.json")

	if err := installClaude("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("未创建 settings.json: %v", err)
	}
	if !queryClaude().Installed {
		t.Error("安装后状态仍为未安装")
	}
}

// 文件损坏时必须报错而不是把它覆盖掉。
func TestInstallClaudeRefusesBrokenJSON(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{"env": {broken`)

	if err := installClaude("/usr/local/bin/acn"); err == nil {
		t.Fatal("对损坏的 JSON 应当报错")
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"env": {broken` {
		t.Errorf("损坏的文件被改写了: %s", data)
	}
}

// 安装写入的命令串必须能被正确解析回来（路径含空格时尤其重要）。
func TestInstalledCommandIsQuoted(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{}`)

	exe := "/Users/me/My Tools/acn"
	if err := installClaude(exe); err != nil {
		t.Fatal(err)
	}

	groups, err := readClaudeGroups(path)
	if err != nil {
		t.Fatal(err)
	}
	var g hookGroup
	if err := json.Unmarshal(groups[0], &g); err != nil {
		t.Fatal(err)
	}
	want := `'/Users/me/My Tools/acn' hook claude`
	if g.Hooks[0].Command != want {
		t.Errorf("命令串 = %q, 期望 %q", g.Hooks[0].Command, want)
	}
	if g.Hooks[0].Timeout != claudeHookTimeout {
		t.Errorf("超时 = %d, 期望 %d", g.Hooks[0].Timeout, claudeHookTimeout)
	}
}

// 安装前必须留下备份。
func TestInstallClaudeWritesBackup(t *testing.T) {
	home := withHome(t)
	path := writeSettings(t, home, `{"env":{"FOO":"bar"}}`)

	if err := installClaude("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".acn.bak")
	if err != nil {
		t.Fatalf("未生成备份: %v", err)
	}
	if string(data) != `{"env":{"FOO":"bar"}}` {
		t.Errorf("备份内容有误: %s", data)
	}
}
