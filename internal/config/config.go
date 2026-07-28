// Package config 负责 acn 的配置读写与路径解析。
//
// 配置只有一个文件：$ACN_CONFIG_DIR（默认 ~/.acn）下的 config.json。
// 敏感字段（通知端点、签名密钥）支持环境变量覆盖，便于 CI 与临时排查。
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
	EnvConfigDir       = "ACN_CONFIG_DIR"
	EnvWebhook         = "ACN_FEISHU_WEBHOOK_URL"
	EnvSecret          = "ACN_FEISHU_SECRET"
	EnvDeviceName      = "ACN_DEVICE_NAME"
	EnvBarkURL         = "ACN_BARK_URL"
	EnvDingTalkWebhook = "ACN_DINGTALK_WEBHOOK_URL"
	EnvDingTalkSecret  = "ACN_DINGTALK_SECRET"
	EnvWeComWebhook    = "ACN_WECOM_WEBHOOK_URL"
	EnvTelegramToken   = "ACN_TELEGRAM_BOT_TOKEN"
	EnvTelegramChatID  = "ACN_TELEGRAM_CHAT_ID"
	EnvEmailSMTP       = "ACN_EMAIL_SMTP"
	EnvEmailUsername   = "ACN_EMAIL_USERNAME"
	EnvEmailPassword   = "ACN_EMAIL_PASSWORD"
	EnvEmailFrom       = "ACN_EMAIL_FROM"
	EnvEmailTo         = "ACN_EMAIL_TO"

	ChannelFeishu   = "feishu"
	ChannelBark     = "bark"
	ChannelDingTalk = "dingtalk"
	ChannelWeCom    = "wecom"
	ChannelTelegram = "telegram"
	ChannelEmail    = "email"

	DefaultBarkCodexURL  = "chatgpt://codex"
	DefaultBarkClaudeURL = "claude://"
)

// Feishu 是飞书自定义机器人的配置。
type Feishu struct {
	WebhookURL string `json:"webhook_url"`
	// Secret 对应机器人「签名校验」安全设置；未开启时留空。
	Secret string `json:"secret,omitempty"`
}

// Bark 是 Bark App 提供的设备推送端点配置。
type Bark struct {
	// URL 形如 https://api.day.app/<device-key>，也支持自托管服务。
	URL             string            `json:"url,omitempty"`
	UpdateBySession bool              `json:"update_by_session"`
	SourceURLs      map[string]string `json:"source_urls"`
}

// DingTalk 是钉钉自定义机器人的配置。
type DingTalk struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret,omitempty"`
}

// WeCom 是企业微信群机器人的配置。
type WeCom struct {
	WebhookURL string `json:"webhook_url"`
}

// Telegram 是 Telegram Bot API 的配置。
type Telegram struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// Email 是 SMTP 邮件渠道的配置。
type Email struct {
	SMTPAddress string `json:"smtp_address"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	From        string `json:"from"`
	To          string `json:"to"`
}

// Config 是 acn 的全部配置。
type Config struct {
	Feishu   Feishu   `json:"feishu"`
	Bark     Bark     `json:"bark"`
	DingTalk DingTalk `json:"dingtalk"`
	WeCom    WeCom    `json:"wecom"`
	Telegram Telegram `json:"telegram"`
	Email    Email    `json:"email"`
	// DeviceName 用于通知标题；留空时取系统 hostname。
	DeviceName      string `json:"device_name,omitempty"`
	ShowDeviceName  bool   `json:"show_device_name"`
	ShowAgentName   bool   `json:"show_agent_name"`
	ShowProjectName bool   `json:"show_project_name"`
	// AgentNames 按 hook 来源覆盖通知标题中的 Agent 名称。
	AgentNames map[string]string `json:"agent_names"`
	// Channels 控制各通知渠道是否启用，缺省视为开启。
	Channels map[string]bool `json:"channels"`
	// Sources 控制各来源是否推送，缺省视为开启。
	Sources map[string]bool `json:"sources"`
	// MinDurationSeconds 低于该耗时的任务不推送。
	MinDurationSeconds int `json:"min_duration_seconds"`
}

// Default 返回未落盘时的默认配置。
func Default() Config {
	return Config{
		Bark: Bark{
			UpdateBySession: true,
			SourceURLs: map[string]string{
				"codex":  DefaultBarkCodexURL,
				"claude": DefaultBarkClaudeURL,
			},
		},
		ShowDeviceName:  false,
		ShowAgentName:   false,
		ShowProjectName: false,
		AgentNames:      map[string]string{"claude": "claude", "codex": "Codex"},
		Channels: map[string]bool{
			ChannelFeishu: true, ChannelBark: true, ChannelDingTalk: true,
			ChannelWeCom: true, ChannelTelegram: true, ChannelEmail: true,
		},
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
	return filepath.Join(home, ".acn")
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

// ChannelEnabled 判断渠道是否开启，旧配置未显式包含渠道时视为开启。
func (c Config) ChannelEnabled(channel string) bool {
	if c.Channels == nil {
		return true
	}
	enabled, ok := c.Channels[channel]
	return !ok || enabled
}

// SetChannelEnabled 显式设置渠道开关。
func (c *Config) SetChannelEnabled(channel string, enabled bool) {
	if c.Channels == nil {
		c.Channels = map[string]bool{}
	}
	c.Channels[channel] = enabled
}

// FeishuReady 判断飞书渠道是否已具备推送条件。
func (c Config) FeishuReady() bool {
	return strings.TrimSpace(c.Feishu.WebhookURL) != ""
}

// BarkReady 判断 Bark 是否已配置设备端点。
func (c Config) BarkReady() bool {
	return strings.TrimSpace(c.Bark.URL) != ""
}

// SourceURL 返回指定来源的通知点击地址；该来源关闭跳转或未配置时返回空字符串。
func (b Bark) SourceURL(source string) string {
	return strings.TrimSpace(b.SourceURLs[source])
}

func (c Config) DingTalkReady() bool { return strings.TrimSpace(c.DingTalk.WebhookURL) != "" }
func (c Config) WeComReady() bool    { return strings.TrimSpace(c.WeCom.WebhookURL) != "" }
func (c Config) TelegramReady() bool {
	return strings.TrimSpace(c.Telegram.BotToken) != "" && strings.TrimSpace(c.Telegram.ChatID) != ""
}
func (c Config) EmailReady() bool {
	return strings.TrimSpace(c.Email.SMTPAddress) != "" &&
		strings.TrimSpace(c.Email.From) != "" && strings.TrimSpace(c.Email.To) != ""
}

// DeliveryReady 判断是否至少有一个已启用且配置完整的通知渠道。
func (c Config) DeliveryReady() bool {
	return (c.ChannelEnabled(ChannelFeishu) && c.FeishuReady()) ||
		(c.ChannelEnabled(ChannelBark) && c.BarkReady()) ||
		(c.ChannelEnabled(ChannelDingTalk) && c.DingTalkReady()) ||
		(c.ChannelEnabled(ChannelWeCom) && c.WeComReady()) ||
		(c.ChannelEnabled(ChannelTelegram) && c.TelegramReady()) ||
		(c.ChannelEnabled(ChannelEmail) && c.EmailReady())
}

// EffectiveDeviceName 返回通知中展示的设备名。显式配置优先，无法读取
// hostname 时使用稳定的占位名称，确保标题始终保留设备前缀。
func (c Config) EffectiveDeviceName() string {
	if name := strings.TrimSpace(c.DeviceName); name != "" {
		return name
	}
	if name, err := os.Hostname(); err == nil {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return "unknown-device"
}

// EffectiveAgentName 返回指定来源的展示名称，未配置时使用内置默认值。
func (c Config) EffectiveAgentName(source string) string {
	if name := strings.TrimSpace(c.AgentNames[source]); name != "" {
		return name
	}
	switch source {
	case "claude":
		return "claude"
	case "codex":
		return "Codex"
	default:
		return strings.TrimSpace(source)
	}
}

func (c *Config) applyEnv() {
	if v := strings.TrimSpace(os.Getenv(EnvWebhook)); v != "" {
		c.Feishu.WebhookURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvSecret)); v != "" {
		c.Feishu.Secret = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvDeviceName)); v != "" {
		c.DeviceName = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvBarkURL)); v != "" {
		c.Bark.URL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvDingTalkWebhook)); v != "" {
		c.DingTalk.WebhookURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvDingTalkSecret)); v != "" {
		c.DingTalk.Secret = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvWeComWebhook)); v != "" {
		c.WeCom.WebhookURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvTelegramToken)); v != "" {
		c.Telegram.BotToken = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvTelegramChatID)); v != "" {
		c.Telegram.ChatID = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvEmailSMTP)); v != "" {
		c.Email.SMTPAddress = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvEmailUsername)); v != "" {
		c.Email.Username = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvEmailPassword)); v != "" {
		c.Email.Password = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvEmailFrom)); v != "" {
		c.Email.From = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvEmailTo)); v != "" {
		c.Email.To = v
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
