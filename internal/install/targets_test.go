package install

import "testing"

func TestValidateTargets(t *testing.T) {
	for _, ok := range [][]string{nil, {}, {"claude"}, {"codex"}, {"claude", "codex"}} {
		if err := ValidateTargets(ok); err != nil {
			t.Errorf("ValidateTargets(%q) 不应报错: %v", ok, err)
		}
	}
	for _, bad := range [][]string{{"gemini"}, {"claude", "typo"}, {""}} {
		if err := ValidateTargets(bad); err == nil {
			t.Errorf("ValidateTargets(%q) 应当报错", bad)
		}
	}
}

func TestSelected(t *testing.T) {
	cases := []struct {
		targets []string
		name    string
		want    bool
	}{
		{nil, "claude", true}, // 空表示全部
		{[]string{}, "codex", true},
		{[]string{"claude"}, "claude", true},
		{[]string{"claude"}, "codex", false},
		{[]string{"claude", "codex"}, "codex", true},
	}
	for _, c := range cases {
		if got := selected(c.targets, c.name); got != c.want {
			t.Errorf("selected(%q, %q) = %v, 期望 %v", c.targets, c.name, got, c.want)
		}
	}
}

// 只装一个时，另一个必须原封不动。
func TestInstallOnlyClaude(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, `{}`)
	codexPath := withCodexHomeIn(t, home, sampleConfig)

	if _, err := Install("/usr/local/bin/acn", TargetClaude); err != nil {
		t.Fatal(err)
	}
	st := Query()
	if !st.Claude.Installed {
		t.Error("Claude 未安装")
	}
	if st.Codex.Installed {
		t.Error("只指定 claude，Codex 却被装上了")
	}
	if got := read(t, codexPath); got != sampleConfig {
		t.Error("Codex 配置被改动了")
	}
}

func TestInstallOnlyCodex(t *testing.T) {
	home := withHome(t)
	settingsPath := writeSettings(t, home, `{"env":{"FOO":"bar"}}`)
	withCodexHomeIn(t, home, sampleConfig)

	if _, err := Install("/usr/local/bin/acn", TargetCodex); err != nil {
		t.Fatal(err)
	}
	st := Query()
	if !st.Codex.Installed {
		t.Error("Codex 未安装")
	}
	if st.Claude.Installed {
		t.Error("只指定 codex，Claude 却被装上了")
	}
	if got := read(t, settingsPath); got != `{"env":{"FOO":"bar"}}` {
		t.Errorf("Claude 配置被改动了: %s", got)
	}
}

// 单独摘除也不能误伤另一侧。
func TestUninstallOnlyClaude(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, `{}`)
	withCodexHomeIn(t, home, sampleConfig)

	if _, err := Install("/usr/local/bin/acn"); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(TargetClaude); err != nil {
		t.Fatal(err)
	}
	st := Query()
	if st.Claude.Installed {
		t.Error("Claude 应已摘除")
	}
	if !st.Codex.Installed {
		t.Error("只摘除 claude，Codex 却也没了")
	}
}

// 配置里记录的 acn 路径要能被原样读回——doctor 靠它发现路径失效。
func TestRecordedExeRoundTrip(t *testing.T) {
	home := withHome(t)
	writeSettings(t, home, `{}`)
	withCodexHomeIn(t, home, sampleConfig)

	exe := "/Users/me/My Tools/acn"
	if _, err := Install(exe); err != nil {
		t.Fatal(err)
	}
	st := Query()
	if st.Claude.Exe != exe {
		t.Errorf("Claude 记录的路径 = %q, 期望 %q", st.Claude.Exe, exe)
	}
	if st.Codex.Exe != exe {
		t.Errorf("Codex 记录的路径 = %q, 期望 %q", st.Codex.Exe, exe)
	}
}

func TestExtractExe(t *testing.T) {
	cases := []struct {
		cmd, source, want string
	}{
		{`'/usr/local/bin/acn' hook claude`, "claude", "/usr/local/bin/acn"},
		{`'/My Tools/acn' hook codex`, "codex", "/My Tools/acn"},
		{`'/a'\''b/acn' hook claude`, "claude", "/a'b/acn"},
		{`/bare/acn hook claude`, "claude", "/bare/acn"},
		{`'/usr/local/bin/acn' hook claude`, "codex", ""}, // 来源不匹配
		{`something else`, "claude", ""},
	}
	for _, c := range cases {
		if got := extractExe(c.cmd, c.source); got != c.want {
			t.Errorf("extractExe(%q, %q) = %q, 期望 %q", c.cmd, c.source, got, c.want)
		}
	}
}
