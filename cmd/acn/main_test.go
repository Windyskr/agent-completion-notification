package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/install"
)

func TestMaskURL(t *testing.T) {
	tests := map[string]string{
		"https://api.day.app/abcdefghijkl":          "https://api.day.app/abcd…ijkl",
		"https://api.day.app/abcdefghijkl/":         "https://api.day.app/abcd…ijkl",
		"https://api.day.app/abcdefghijkl?x=secret": "https://api.day.app/abcd…ijkl",
		"https://api.day.app/short":                 "https://api.day.app/****",
		"not-a-url":                                 "已配置（地址格式异常）",
	}
	for raw, want := range tests {
		if got := maskURL(raw); got != want {
			t.Errorf("maskURL(%q) = %q, 期望 %q", raw, got, want)
		}
	}
}

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
	if !cfg.ChannelEnabled(config.ChannelFeishu) || !cfg.ChannelEnabled(config.ChannelBark) {
		t.Error("设置渠道 URL 后应自动启用渠道")
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
