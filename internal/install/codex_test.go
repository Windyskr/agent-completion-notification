package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 真实用户配置的形状：顶层键 + 被别的集成占用的 notify + 若干表与注释。
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

// withCodexHome 把 CODEX_HOME 指向沙箱并写入初始配置。
func withCodexHome(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	t.Setenv("ACN_CONFIG_DIR", filepath.Join(dir, "acn"))
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// 装的是 [[hooks.Stop]]，绝不能碰用户的 notify——那是别的集成在用。
func TestInstallCodexAddsHookAndLeavesNotifyAlone(t *testing.T) {
	path := withCodexHome(t, sampleConfig)

	if err := installCodex("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)

	for _, want := range []string{"[[hooks.Stop]]", "[[hooks.Stop.hooks]]", `type = "command"`, "hook codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q:\n%s", want, out)
		}
	}
	// 用户既有的 notify 必须原样存活。
	if !strings.Contains(out, `notify = ["/Applications/Codex.app/SkyComputerUseClient", "turn-ended"]`) {
		t.Errorf("用户的 notify 被改动了:\n%s", out)
	}
	for _, keep := range []string{`model = "gpt-5.6-sol"`, "[model_providers.OpenAI]", "# 用户的注释", "[mcp_servers.node_repl]"} {
		if !strings.Contains(out, keep) {
			t.Errorf("丢失了原有内容 %q", keep)
		}
	}
	if !queryCodex().Installed {
		t.Error("安装后状态仍为未安装")
	}
}

// hook 块必须追加在所有表之后，否则会被并入最后一个表。
func TestInstallCodexAppendsAfterExistingTables(t *testing.T) {
	path := withCodexHome(t, sampleConfig)
	if err := installCodex("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)

	if strings.Index(out, "[[hooks.Stop]]") < strings.Index(out, "[mcp_servers.node_repl]") {
		t.Errorf("hook 块落在了已有表之前:\n%s", out)
	}
}

func TestInstallCodexIdempotent(t *testing.T) {
	path := withCodexHome(t, sampleConfig)

	for i := 0; i < 3; i++ {
		if err := installCodex("/usr/local/bin/acn"); err != nil {
			t.Fatal(err)
		}
	}
	out := read(t, path)

	if n := strings.Count(out, "[[hooks.Stop]]"); n != 1 {
		t.Errorf("重复安装产生了 %d 个 hook 块，应为 1:\n%s", n, out)
	}
	if n := strings.Count(out, codexBegin); n != 1 {
		t.Errorf("哨兵出现 %d 次，应为 1", n)
	}
}

// 卸载必须逐字节还原。
func TestUninstallCodexRestoresExactly(t *testing.T) {
	path := withCodexHome(t, sampleConfig)

	if err := installCodex("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if err := uninstallCodex(); err != nil {
		t.Fatal(err)
	}

	if got := read(t, path); got != sampleConfig {
		t.Errorf("卸载后与原始文件不一致：\n--- 期望 ---\n%s\n--- 实际 ---\n%s", sampleConfig, got)
	}
	if queryCodex().Installed {
		t.Error("卸载后状态仍为已安装")
	}
}

// 反复装卸不能堆出空行。
func TestInstallUninstallCyclesAreStable(t *testing.T) {
	path := withCodexHome(t, sampleConfig)

	for i := 0; i < 3; i++ {
		if err := installCodex("/usr/local/bin/acn"); err != nil {
			t.Fatal(err)
		}
		if err := uninstallCodex(); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, path); got != sampleConfig {
		t.Errorf("多轮装卸后出现漂移：\n%q", got)
	}
}

// 没装过时卸载是空操作，不能动文件。
func TestUninstallCodexNoopWhenAbsent(t *testing.T) {
	path := withCodexHome(t, sampleConfig)
	if err := uninstallCodex(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != sampleConfig {
		t.Error("未安装时卸载改动了文件")
	}
}

// 配置不存在时应给出可操作的提示，而不是默默创建一个空配置。
func TestInstallCodexRequiresExistingConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)

	err := installCodex("/usr/local/bin/acn")
	if err == nil {
		t.Fatal("配置不存在时应当报错")
	}
	if !strings.Contains(err.Error(), "请先运行一次 Codex") {
		t.Errorf("错误信息不够可操作: %v", err)
	}
}

// 含空格的路径必须被正确引用，且写出的 TOML 仍然合法。
func TestInstallCodexQuotesPathWithSpaces(t *testing.T) {
	path := withCodexHome(t, sampleConfig)

	if err := installCodex("/Users/me/My Tools/acn"); err != nil {
		t.Fatal(err)
	}
	out := read(t, path)

	want := `command = "'/Users/me/My Tools/acn' hook codex"`
	if !strings.Contains(out, want) {
		t.Errorf("命令行未正确引用，期望包含 %s：\n%s", want, out)
	}
}

// 安装前必须留下备份。
func TestInstallCodexWritesBackup(t *testing.T) {
	path := withCodexHome(t, sampleConfig)

	if err := installCodex("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path+".acn.bak"); got != sampleConfig {
		t.Errorf("备份内容有误:\n%s", got)
	}
}

// 哨兵不成对（用户手删了一半）时不能乱删内容。
func TestStripACNBlockIgnoresUnpairedSentinel(t *testing.T) {
	src := sampleConfig + codexBegin + "\n[[hooks.Stop]]\n"
	if got := stripACNBlock(src); got != src {
		t.Errorf("哨兵不成对时不应改动内容:\n%s", got)
	}
}

// [features] hooks = false 时块照样写得进去，但永远不会触发——必须警告。
func TestQueryCodexWarnsWhenHooksDisabled(t *testing.T) {
	withCodexHome(t, "[features]\nhooks = false\n\n"+sampleConfig)
	if err := installCodex("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}

	st := queryCodex()
	if !st.Installed {
		t.Fatal("应识别为已安装")
	}
	if !strings.Contains(st.Warning, "hooks = false") {
		t.Errorf("未就 hooks 被禁用发出警告，Warning = %q", st.Warning)
	}
}

// 正常配置不该有警告——误报比不报更糟。
func TestQueryCodexNoWarningNormally(t *testing.T) {
	withCodexHome(t, sampleConfig)
	if err := installCodex("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if w := queryCodex().Warning; w != "" {
		t.Errorf("正常配置不应有警告，得到 %q", w)
	}
}

func TestHooksDisabled(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"显式关闭", "[features]\nhooks = false\n", true},
		{"显式开启", "[features]\nhooks = true\n", false},
		{"未提及", "[features]\ngoals = true\n", false},
		{"无 features 表", sampleConfig, false},
		{"注释掉的关闭", "[features]\n# hooks = false\n", false},
		{"带行内注释", "[features]\nhooks = false # 关掉\n", true},
		{"别的表里的同名键", "[other]\nhooks = false\n", false},
		{"features 表之后的其它表", "[features]\ngoals = true\n[other]\nhooks = false\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hooksDisabled(c.in); got != c.want {
				t.Errorf("hooksDisabled = %v, 期望 %v", got, c.want)
			}
		})
	}
}
