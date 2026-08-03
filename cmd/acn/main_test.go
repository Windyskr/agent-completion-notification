package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/install"
)

func TestCmdConfigUsesChannelPrefixedNames(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	if err := cmdConfig([]string{"feishu", "off"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"bark", "off"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"feishu-url", "https://example.com/feishu-key"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"bark-url", "https://api.day.app/bark-key"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"slack-url", "https://hooks.slack.test/slack-key"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"teams-url", "https://teams.test/teams-key"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Feishu.WebhookURL != "https://example.com/feishu-key" {
		t.Errorf("Feishu URL = %q", cfg.Feishu.WebhookURL)
	}
	if cfg.Bark.URL != "https://api.day.app/bark-key" {
		t.Errorf("Bark URL = %q", cfg.Bark.URL)
	}
	if cfg.Slack.WebhookURL != "https://hooks.slack.test/slack-key" {
		t.Errorf("Slack URL = %q", cfg.Slack.WebhookURL)
	}
	if cfg.Teams.WebhookURL != "https://teams.test/teams-key" {
		t.Errorf("Teams URL = %q", cfg.Teams.WebhookURL)
	}
	if !cfg.ChannelEnabled(config.ChannelFeishu) || !cfg.ChannelEnabled(config.ChannelBark) ||
		!cfg.ChannelEnabled(config.ChannelSlack) || !cfg.ChannelEnabled(config.ChannelTeams) {
		t.Error("设置渠道 URL 后应自动启用渠道")
	}
	if err := cmdConfig([]string{"slack-url", "off"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"teams-url", "off"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SlackReady() || cfg.ChannelEnabled(config.ChannelSlack) ||
		!cfg.TeamsReady() || cfg.ChannelEnabled(config.ChannelTeams) {
		t.Error("Slack/Teams URL off 应保留地址并关闭渠道")
	}
	if err := cmdConfig([]string{"slack", "on"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"teams", "on"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ChannelEnabled(config.ChannelSlack) || !cfg.ChannelEnabled(config.ChannelTeams) {
		t.Error("重新开启 Slack/Teams 应复用原地址")
	}

	if err := cmdConfig([]string{"bark-url", "off"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BarkReady() || cfg.ChannelEnabled(config.ChannelBark) {
		t.Error("bark-url off 应保留地址并关闭渠道")
	}
	if err := cmdConfig([]string{"bark", "on"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BarkReady() || !cfg.ChannelEnabled(config.ChannelBark) {
		t.Error("重新开启 Bark 应复用原地址")
	}
}

func TestCmdConfigIgnoredDirectories(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	root := filepath.Join(t.TempDir(), "project")
	other := filepath.Join(t.TempDir(), "other")

	if err := cmdConfig([]string{"ignore-dir", root}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"ignore-dir", root}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"ignore-dir", other}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IgnoredDirectories) != 2 {
		t.Fatalf("忽略目录数量 = %d, 期望 2: %v", len(cfg.IgnoredDirectories), cfg.IgnoredDirectories)
	}

	if err := cmdConfig([]string{"unignore-dir", root}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IgnoredDirectories) != 1 || cfg.IgnoredDirectories[0] != other {
		t.Errorf("移除目录后规则错误: %v", cfg.IgnoredDirectories)
	}

	if err := cmdConfig([]string{"ignore-dir", "off"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IgnoredDirectories) != 0 {
		t.Errorf("ignore-dir off 后仍有规则: %v", cfg.IgnoredDirectories)
	}
}

func TestCmdConfigIgnoreList(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	root := filepath.Join(t.TempDir(), "project")
	if err := cmdConfig([]string{"ignore-dir", root}); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return cmdConfig([]string{"ignore-list"})
	})
	if !strings.Contains(output, "忽略目录：") || !strings.Contains(output, root) {
		t.Errorf("ignore-list 输出错误: %q", output)
	}

	if err := cmdConfig([]string{"ignore-dir", "off"}); err != nil {
		t.Fatal(err)
	}
	output = captureStdout(t, func() error {
		return cmdConfig([]string{"ignore-list"})
	})
	if !strings.Contains(output, "（无）") {
		t.Errorf("空 ignore-list 输出错误: %q", output)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	callErr := fn()
	_ = writer.Close()
	os.Stdout = original
	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if callErr != nil {
		t.Fatal(callErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data)
}

func TestCmdConfigDeviceNameEnablesDisplay(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	if err := cmdConfig([]string{"show-device-name", "off"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConfig([]string{"device-name", "mac"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceName != "mac" || !cfg.ShowDeviceName {
		t.Errorf("device-name 未自动开启显示: name=%q show=%v", cfg.DeviceName, cfg.ShowDeviceName)
	}
}

func TestCmdConfigAgentNameVisibility(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	if err := cmdConfig([]string{"show-agent-name", "off"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ShowAgentName {
		t.Error("show-agent-name off 未关闭 Agent 名显示")
	}

	if err := cmdConfig([]string{"claude-agent-name", "opus"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAgentName(event.SourceClaude) != "opus" || !cfg.ShowAgentName {
		t.Errorf("设置 Agent 名后未自动显示: name=%q show=%v",
			cfg.EffectiveAgentName(event.SourceClaude), cfg.ShowAgentName)
	}
}

func TestCmdConfigRejectsLegacyChannelNames(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	for _, key := range []string{"webhook", "secret"} {
		err := cmdConfig([]string{key, "value"})
		if err == nil || !strings.Contains(err.Error(), "未知配置项") {
			t.Errorf("旧配置名 %q 应被拒绝，得到 %v", key, err)
		}
	}
}

func TestCheckTargetTreatsSymlinkAsCurrentExecutable(t *testing.T) {
	current, err := install.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "acn")
	if err := os.Symlink(current, link); err != nil {
		t.Fatal(err)
	}

	got := checkTarget(install.TargetStatus{Name: "test", Installed: true, Exe: link})
	if got.level != levelOK {
		t.Errorf("同一二进制的 symlink 被误报: %+v", got)
	}
}
