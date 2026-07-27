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
	if !cfg.ShowProjectName {
		t.Error("默认应显示项目名")
	}
	if got := cfg.EffectiveAgentName("claude"); got != "claude" {
		t.Errorf("Claude 默认 Agent 名 = %q", got)
	}
	if got := cfg.EffectiveAgentName("codex"); got != "Codex" {
		t.Errorf("Codex 默认 Agent 名 = %q", got)
	}
	if !cfg.ChannelEnabled("feishu") || !cfg.ChannelEnabled("bark") {
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
	if cfg.ShowDeviceName || !cfg.ShowProjectName {
		t.Errorf("旧配置的标题默认值错误: device=%v project=%v",
			cfg.ShowDeviceName, cfg.ShowProjectName)
	}
	if got := cfg.EffectiveAgentName("claude"); got != "claude" {
		t.Errorf("旧配置的 Claude Agent 名 = %q", got)
	}
	if !cfg.ChannelEnabled("feishu") || !cfg.ChannelEnabled("bark") {
		t.Error("旧配置应默认启用通知渠道")
	}
}
