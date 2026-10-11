package notify

import (
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/bark"
	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/dingtalk"
	"github.com/windyskr/agent-completion-notification/internal/email"
	"github.com/windyskr/agent-completion-notification/internal/feishu"
	"github.com/windyskr/agent-completion-notification/internal/slack"
	"github.com/windyskr/agent-completion-notification/internal/teams"
	"github.com/windyskr/agent-completion-notification/internal/telegram"
	"github.com/windyskr/agent-completion-notification/internal/wecom"
)

// Channel 描述一个通知渠道。新增渠道只需在 Channels 里登记一项，投递、
// acn status、acn doctor 与安装后的配置提示都会自动覆盖它。
type Channel struct {
	// Name 是配置里的渠道开关名，与 config.Channel* 常量及渠道自身的 Name() 一致。
	Name string
	// Label 是 status / doctor 中的展示名。
	Label string
	// Setup 是尚未配置任何渠道时给出的配置示例。
	Setup string
	// Ready 判断渠道配置是否完整。
	Ready func(config.Config) bool
	// Describe 返回状态说明：配置完整时为端点等详情，否则说明缺少什么。
	Describe func(config.Config) string
	// New 按配置构造渠道。
	New func(config.Config) Notifier
}

// Active 判断渠道是否已启用且配置完整，即本次投递是否会发往它。
func (c Channel) Active(cfg config.Config) bool {
	return cfg.ChannelEnabled(c.Name) && c.Ready(cfg)
}

// Channels 是全部通知渠道，顺序即 status / doctor 的展示顺序。
var Channels = []Channel{
	{
		Name: config.ChannelFeishu, Label: "飞书 webhook",
		Setup: "飞书：acn config feishu-url <机器人地址>",
		Ready: config.Config.FeishuReady,
		Describe: func(cfg config.Config) string {
			return describeSigned(cfg.FeishuReady(), cfg.Feishu.WebhookURL, cfg.Feishu.Secret)
		},
		New: func(cfg config.Config) Notifier { return feishu.New(cfg.Feishu) },
	},
	{
		Name: config.ChannelBark, Label: "Bark endpoint",
		Setup: "Bark：acn config bark-url https://api.day.app/<key>",
		Ready: config.Config.BarkReady,
		Describe: func(cfg config.Config) string {
			return describeEndpoint(cfg.BarkReady(), cfg.Bark.URL)
		},
		New: func(cfg config.Config) Notifier { return bark.New(cfg.Bark) },
	},
	{
		Name: config.ChannelDingTalk, Label: "钉钉 webhook",
		Setup: "钉钉：acn config dingtalk-url <机器人地址>",
		Ready: config.Config.DingTalkReady,
		Describe: func(cfg config.Config) string {
			return describeSigned(cfg.DingTalkReady(), cfg.DingTalk.WebhookURL, cfg.DingTalk.Secret)
		},
		New: func(cfg config.Config) Notifier { return dingtalk.New(cfg.DingTalk) },
	},
	{
		Name: config.ChannelWeCom, Label: "企微 webhook",
		Setup: "企微：acn config wecom-url <机器人地址>",
		Ready: config.Config.WeComReady,
		Describe: func(cfg config.Config) string {
			return describeEndpoint(cfg.WeComReady(), cfg.WeCom.WebhookURL)
		},
		New: func(cfg config.Config) Notifier { return wecom.New(cfg.WeCom) },
	},
	{
		Name: config.ChannelTelegram, Label: "Telegram bot",
		Setup:    "Telegram：acn config telegram-token <token> && acn config telegram-chat-id <id>",
		Ready:    config.Config.TelegramReady,
		Describe: describeTelegram,
		New:      func(cfg config.Config) Notifier { return telegram.New(cfg.Telegram) },
	},
	{
		Name: config.ChannelEmail, Label: "Email SMTP",
		Setup:    "Email：acn config email-smtp <host:port>（并配置账号、发件人和收件人）",
		Ready:    config.Config.EmailReady,
		Describe: describeEmail,
		New:      func(cfg config.Config) Notifier { return email.New(cfg.Email) },
	},
	{
		Name: config.ChannelSlack, Label: "Slack webhook",
		Setup: "Slack：acn config slack-url <Incoming Webhook 地址>",
		Ready: config.Config.SlackReady,
		Describe: func(cfg config.Config) string {
			return describeEndpoint(cfg.SlackReady(), cfg.Slack.WebhookURL)
		},
		New: func(cfg config.Config) Notifier { return slack.New(cfg.Slack) },
	},
	{
		Name: config.ChannelTeams, Label: "Teams webhook",
		Setup: "Teams：acn config teams-url <Incoming Webhook 地址>",
		Ready: config.Config.TeamsReady,
		Describe: func(cfg config.Config) string {
			return describeEndpoint(cfg.TeamsReady(), cfg.Teams.WebhookURL)
		},
		New: func(cfg config.Config) Notifier { return teams.New(cfg.Teams) },
	},
}

// DeliveryReady 判断是否至少有一个已启用且配置完整的通知渠道。
func DeliveryReady(cfg config.Config) bool {
	for _, channel := range Channels {
		if channel.Active(cfg) {
			return true
		}
	}
	return false
}

func configuredNotifiers(cfg config.Config) []Notifier {
	var notifiers []Notifier
	for _, channel := range Channels {
		if channel.Active(cfg) {
			notifiers = append(notifiers, channel.New(cfg))
		}
	}
	return notifiers
}

func describeEndpoint(ready bool, endpoint string) string {
	if !ready {
		return "未配置"
	}
	return strings.TrimSpace(endpoint)
}

func describeSigned(ready bool, endpoint, secret string) string {
	detail := describeEndpoint(ready, endpoint)
	if ready && strings.TrimSpace(secret) != "" {
		detail += "（已启用签名校验）"
	}
	return detail
}

// describeTelegram 只显示 token 首尾各四位。
func describeTelegram(cfg config.Config) string {
	if strings.TrimSpace(cfg.Telegram.BotToken) == "" {
		return "未配置 token"
	}
	token := cfg.Telegram.BotToken
	masked := "****"
	if len(token) > 8 {
		masked = token[:4] + "…" + token[len(token)-4:]
	}
	if strings.TrimSpace(cfg.Telegram.ChatID) == "" {
		return masked + "（缺少 chat_id）"
	}
	return masked + " → " + cfg.Telegram.ChatID
}

func describeEmail(cfg config.Config) string {
	if !cfg.EmailReady() {
		return "未完整配置"
	}
	return cfg.Email.SMTPAddress + "（" + cfg.Email.From + " → " + cfg.Email.To + "）"
}
