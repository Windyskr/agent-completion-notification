package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTitleVisibility(t *testing.T) {
	cfg := Default()
	if cfg.ShowDeviceName {
		t.Error("默认不应显示设备名")
	}
	if cfg.ShowAgentName {
		t.Error("默认不应显示 Agent 名")
	}
	if cfg.ShowProjectName {
		t.Error("默认不应显示项目名")
	}
	if got := cfg.EffectiveAgentName("claude"); got != "claude" {
		t.Errorf("Claude 默认 Agent 名 = %q", got)
	}
	if got := cfg.EffectiveAgentName("codex"); got != "Codex" {
		t.Errorf("Codex 默认 Agent 名 = %q", got)
	}
	if !cfg.ChannelEnabled("feishu") || !cfg.ChannelEnabled("bark") ||
		!cfg.ChannelEnabled(ChannelSlack) || !cfg.ChannelEnabled(ChannelTeams) {
		t.Error("通知渠道默认应开启")
	}
}

func TestEffectiveAgentNameUsesConfiguredValue(t *testing.T) {
	cfg := Default()
	cfg.AgentNames["claude"] = "opus"
	if got := cfg.EffectiveAgentName("claude"); got != "opus" {
		t.Errorf("EffectiveAgentName = %q, 期望 opus", got)
	}
}

func TestEffectiveDeviceNameUsesConfiguredValue(t *testing.T) {
	cfg := Config{DeviceName: "  devbox  "}
	if got := cfg.EffectiveDeviceName(); got != "devbox" {
		t.Errorf("EffectiveDeviceName = %q, 期望 devbox", got)
	}
}

func TestLoadAppliesDeviceNameEnvironment(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvDeviceName, "env-device")

	cfg := Default()
	cfg.DeviceName = "configured-device"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.EffectiveDeviceName(); got != "env-device" {
		t.Errorf("EffectiveDeviceName = %q, 环境变量应优先", got)
	}
}

func TestLoadAppliesBarkEnvironment(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvBarkURL, "https://api.day.app/env-key")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Bark.URL; got != "https://api.day.app/env-key" {
		t.Errorf("Bark URL = %q, 环境变量应优先", got)
	}
}

func TestLoadAppliesSlackAndTeamsEnvironment(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	t.Setenv(EnvSlackWebhook, "https://hooks.slack.test/webhook")
	t.Setenv(EnvTeamsWebhook, "https://teams.test/webhook")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Slack.WebhookURL != "https://hooks.slack.test/webhook" {
		t.Errorf("Slack URL = %q, 环境变量应优先", cfg.Slack.WebhookURL)
	}
	if cfg.Teams.WebhookURL != "https://teams.test/webhook" {
		t.Errorf("Teams URL = %q, 环境变量应优先", cfg.Teams.WebhookURL)
	}
}

func TestDeliveryReadyRequiresConfiguredEnabledChannel(t *testing.T) {
	cfg := Default()
	if cfg.DeliveryReady() {
		t.Error("未配置渠道时不应可投递")
	}
	cfg.Bark.URL = "https://api.day.app/key"
	if !cfg.DeliveryReady() {
		t.Error("已配置 Bark 时应可投递")
	}
	cfg.Channels["bark"] = false
	if cfg.DeliveryReady() {
		t.Error("Bark 已禁用时不应可投递")
	}
	cfg.Slack.WebhookURL = "https://hooks.slack.test/webhook"
	if !cfg.DeliveryReady() {
		t.Error("已配置 Slack 时应可投递")
	}
	cfg.Channels[ChannelSlack] = false
	cfg.Teams.WebhookURL = "https://teams.test/webhook"
	if !cfg.DeliveryReady() {
		t.Error("已配置 Teams 时应可投递")
	}
}

func TestLoadAppliesTitleDefaultsToLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	legacy := []byte(`{"feishu":{},"sources":{"claude":true,"codex":true}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ShowDeviceName || cfg.ShowAgentName || cfg.ShowProjectName {
		t.Errorf("旧配置的标题默认值错误: device=%v agent=%v project=%v",
			cfg.ShowDeviceName, cfg.ShowAgentName, cfg.ShowProjectName)
	}
	if got := cfg.EffectiveAgentName("claude"); got != "claude" {
		t.Errorf("旧配置的 Claude Agent 名 = %q", got)
	}
	if !cfg.ChannelEnabled("feishu") || !cfg.ChannelEnabled("bark") {
		t.Error("旧配置应默认启用通知渠道")
	}
}

func TestLoadPreservesExplicitTitleVisibility(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	existing := []byte(`{
		"feishu": {},
		"show_device_name": true,
		"show_agent_name": true,
		"show_project_name": true
	}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), existing, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ShowDeviceName || !cfg.ShowAgentName || !cfg.ShowProjectName {
		t.Errorf("已有标题配置未保留: device=%v agent=%v project=%v",
			cfg.ShowDeviceName, cfg.ShowAgentName, cfg.ShowProjectName)
	}
}

func TestIgnoredDirectoryMatchesRootAndDescendants(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	cfg := Config{IgnoredDirectories: []string{root}}

	for _, cwd := range []string{
		root,
		filepath.Join(root, "nested"),
		filepath.Join(root, "nested", "child"),
	} {
		if matched, ok := cfg.IgnoredDirectory(cwd); !ok || matched != root {
			t.Errorf("IgnoredDirectory(%q) = %q, %v; 期望匹配 %q", cwd, matched, ok, root)
		}
	}
}

func TestIgnoredDirectoryDoesNotMatchSiblingPrefixOrEmptyPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	cfg := Config{IgnoredDirectories: []string{root}}

	for _, cwd := range []string{
		root + "-other",
		filepath.Join(filepath.Dir(root), "application"),
		"",
	} {
		if matched, ok := cfg.IgnoredDirectory(cwd); ok {
			t.Errorf("IgnoredDirectory(%q) 意外匹配 %q", cwd, matched)
		}
	}
}

func TestIgnoredDirectoryConfigurationIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	var cfg Config
	if err := cfg.AddIgnoredDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddIgnoredDirectory(root + string(filepath.Separator)); err != nil {
		t.Fatal(err)
	}
	if len(cfg.IgnoredDirectories) != 1 {
		t.Fatalf("重复目录被保存了 %d 次", len(cfg.IgnoredDirectories))
	}
	if err := cfg.RemoveIgnoredDirectory(root); err != nil {
		t.Fatal(err)
	}
	if len(cfg.IgnoredDirectories) != 0 {
		t.Fatalf("目录移除后仍有规则: %v", cfg.IgnoredDirectories)
	}
}
