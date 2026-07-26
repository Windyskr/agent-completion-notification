// Package config 负责 acn 的配置读写与路径解析。
//
// 配置只有一个文件：$ACN_CONFIG_DIR（默认 ~/.config/acn）下的 config.json。
// 敏感字段（webhook 地址、签名密钥）支持环境变量覆盖，便于 CI 与临时排查。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvConfigDir = "ACN_CONFIG_DIR"
	EnvWebhook   = "ACN_FEISHU_WEBHOOK_URL"
	EnvSecret    = "ACN_FEISHU_SECRET"
)

// Feishu 是飞书自定义机器人的配置。
type Feishu struct {
	WebhookURL string `json:"webhook_url"`
	// Secret 对应机器人「签名校验」安全设置；未开启时留空。
	Secret string `json:"secret,omitempty"`
}

// Config 是 acn 的全部配置。
type Config struct {
	Feishu Feishu `json:"feishu"`
	// Sources 控制各来源是否推送，缺省视为开启。
	Sources map[string]bool `json:"sources"`
	// MinDurationSeconds 低于该耗时的任务不推送。仅对能提供耗时的来源生效
	// （目前只有 Claude Code）。
	MinDurationSeconds int `json:"min_duration_seconds"`
	// CodexNotifyChain 保存安装时被 acn 接管的原 Codex notify 程序。
	// hook 处理完后原样转发，避免破坏用户已有的 Codex 集成；卸载时还原。
	CodexNotifyChain []string `json:"codex_notify_chain,omitempty"`
}

// Default 返回未落盘时的默认配置。
func Default() Config {
	return Config{
		Sources: map[string]bool{"claude": true, "codex": true},
	}
}

// Dir 返回配置目录。
func Dir() string {
	if v := strings.TrimSpace(os.Getenv(EnvConfigDir)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".acn"
	}
	return filepath.Join(home, ".config", "acn")
}

// Path 返回配置文件路径。
func Path() string { return filepath.Join(Dir(), "config.json") }

// SocketPath 返回 daemon 监听的 Unix socket 路径。
func SocketPath() string { return filepath.Join(Dir(), "acn.sock") }

// Load 读取配置；文件不存在时返回默认配置而非报错，让 acn install 之前的
// status / test 等命令仍可运行。
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	switch {
	case err == nil:
		// 用户配置合并进默认值：JSON 反序列化到已有 map 是逐键覆盖，
		// 因此只写 {"codex": false} 不会把 claude 一起关掉。
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("解析 %s 失败: %w", Path(), err)
		}
	case errors.Is(err, os.ErrNotExist):
		// 保持默认配置
	default:
		return cfg, fmt.Errorf("读取 %s 失败: %w", Path(), err)
	}
	cfg.applyEnv()
	return cfg, nil
}

// Save 原子写入配置。文件含密钥，权限收敛到 0600。
func Save(cfg Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("创建 %s 失败: %w", Dir(), err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(Path(), append(data, '\n'), 0o600)
}

// SourceEnabled 判断来源是否开启，未显式配置视为开启。
func (c Config) SourceEnabled(source string) bool {
	if c.Sources == nil {
		return true
	}
	enabled, ok := c.Sources[source]
	return !ok || enabled
}

// FeishuReady 判断飞书渠道是否已具备推送条件。
func (c Config) FeishuReady() bool {
	return strings.TrimSpace(c.Feishu.WebhookURL) != ""
}

func (c *Config) applyEnv() {
	if v := strings.TrimSpace(os.Getenv(EnvWebhook)); v != "" {
		c.Feishu.WebhookURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvSecret)); v != "" {
		c.Feishu.Secret = v
	}
}

// WriteFileAtomic 先写临时文件再 rename，避免写入中途崩溃留下半个文件。
// install 包改写用户既有配置时同样依赖它。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
