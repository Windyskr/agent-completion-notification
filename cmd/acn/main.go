// 命令 acn（Agent Completion Notification）在 AI CLI（Claude Code / Codex）
// 完成任务时推送通知。
//
// 事件流：
//
//	Claude Code ──Stop hook──┐
//	                         ├─→ acn hook ─→ 飞书
//	Codex ───────Stop hook───┘
//
// 两者用的是同一套 Stop hook 契约，载荷都从 stdin 进来。
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/install"
	"github.com/windyskr/agent-completion-notification/internal/notify"
)

// version 由构建时通过 -ldflags "-X main.version=..." 注入。
var version = "dev"

const usage = `acn (Agent Completion Notification) — Agent 任务完成通知

用法：
  acn install [目标]     接入 AI CLI（写入其配置，自动备份）
  acn uninstall [目标]   摘除接入
  acn status             查看接入状态与配置
  acn doctor             自检整条链路，并实际推送一条通知
  acn config <k> <v>     修改配置项
  acn hook claude        Claude Code 的 Stop hook 入口（读 stdin）
  acn hook codex         Codex 的 Stop hook 入口（读 stdin）
  acn version            打印版本

目标（install / uninstall，省略则两个都做）：
  claude                 仅 Claude Code
  codex                  仅 Codex

配置项（acn config）：
  feishu-url <url|off>   配置并启用飞书；off 仅关闭渠道
  feishu-secret <str|off> 飞书签名密钥（未开启则留空）
  bark-url <url|off>     配置并启用 Bark；off 仅关闭渠道
  dingtalk-url <url|off> 配置并启用钉钉机器人
  dingtalk-secret <str|off> 钉钉机器人加签密钥
  wecom-url <url|off>    配置并启用企微群机器人
  telegram-token <token|off> 配置并启用 Telegram Bot
  telegram-chat-id <id> Telegram 目标用户、群组或频道 ID
  email-smtp <host:port|off> SMTP 服务器地址
  email-username <str> SMTP 用户名
  email-password <str|off> SMTP 密码或授权码
  email-from <address> 发件人地址
  email-to <addresses> 收件人地址，多个用逗号分隔
  feishu <on|off>        是否启用飞书渠道
  bark <on|off>          是否启用 Bark 渠道
  dingtalk|wecom|telegram|email <on|off> 是否启用对应渠道
  min-duration <秒>      低于该耗时不推送，0 为不限
  device-name <名称|auto> 设置并显示设备名（auto 使用系统 hostname）
  show-device-name <on|off>  标题是否显示设备名（默认 off）
  show-agent-name <on|off>   标题是否显示 Agent 名（默认 off）
  show-project-name <on|off> 标题是否显示项目名（默认 off）
  claude-agent-name <名称|auto> Claude Agent 名（默认 claude）
  codex-agent-name <名称|auto>  Codex Agent 名（默认 Codex）
  claude <on|off>        是否推送 Claude Code
  codex <on|off>         是否推送 Codex

快速开始：
  acn config feishu-url https://open.feishu.cn/open-apis/bot/v2/hook/xxxx
  # 或：acn config bark-url https://api.day.app/your_key
  acn install
  acn doctor
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "acn: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch args[0] {
	case "install":
		return cmdInstall(args[1:])
	case "uninstall":
		return cmdUninstall(args[1:])
	case "status":
		return cmdStatus()
	case "config":
		return cmdConfig(args[1:])
	case "doctor", "test":
		return cmdDoctor()
	case "hook":
		return cmdHook(args[1:])
	case "version", "--version", "-v":
		fmt.Println("acn " + version)
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("未知命令 %q，运行 acn help 查看用法", args[0])
	}
}

// cmdHook 处理来自 AI CLI 的 Stop 回调。
//
// 无论内部发生什么都返回 nil，且**绝不往 stdout 写任何东西**：
//   - 非零退出码会在用户终端里显示报错；
//   - Codex 的 Stop hook 若在 stdout 收到 {"decision":"block"}，会自动续跑一轮。
//
// 通知失败远不如打断工作流严重，因此一切诊断信息只走 stderr。
func cmdHook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：acn hook <claude|codex>")
	}

	ev, err := buildHookEvent(args[0], os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "acn hook: "+err.Error())
		return nil
	}
	deliver(ev)
	return nil
}

// deliver 在当前进程内完成投递。
//
// 曾经这里有一条「先丢给常驻 daemon 异步发」的快路径，实测只省 78ms
// （本地开销 45ms 两者相同，差的仅是一次约 80ms 的飞书往返），
// 却要付出常驻服务、socket 生命周期与两套投递路径的代价，已移除。
func deliver(ev event.Event) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acn: 加载配置失败: "+err.Error())
		return
	}

	skipped, err := notify.Send(context.Background(), cfg, ev)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "acn: 推送失败: "+err.Error())
	case skipped != "":
		fmt.Fprintln(os.Stderr, "acn: 未推送（"+skipped+"）")
	}
}

func cmdInstall(targets []string) error {
	if err := install.ValidateTargets(targets); err != nil {
		return err
	}
	exe, err := install.Executable()
	if err != nil {
		return fmt.Errorf("获取自身路径失败: %w", err)
	}

	st, installErr := install.Install(exe, targets...)
	printTarget(st.Claude)
	printTarget(st.Codex)
	if installErr != nil {
		return installErr
	}

	cfg, _ := config.Load()
	fmt.Println()
	if !cfg.DeliveryReady() {
		fmt.Println("下一步：配置至少一个通知渠道")
		fmt.Println("  飞书：acn config feishu-url <机器人地址>")
		fmt.Println("  Bark：acn config bark-url https://api.day.app/<key>")
		fmt.Println("  钉钉：acn config dingtalk-url <机器人地址>")
		fmt.Println("  企微：acn config wecom-url <机器人地址>")
		fmt.Println("  Telegram：acn config telegram-token <token> && acn config telegram-chat-id <id>")
		fmt.Println("  Email：acn config email-smtp <host:port>（并配置账号、发件人和收件人）")
	}
	if st.Codex.Installed {
		// Codex 要求用户逐条审阅并信任非托管的命令 hook，否则它不会执行。
		fmt.Println("下一步：请在终端中运行 codex 进行信任授权（/hooks）")
	}
	fmt.Println("提示：两个 CLI 均需重启后生效；装好后可用 acn doctor 自检")
	return nil
}

func cmdUninstall(targets []string) error {
	if err := install.ValidateTargets(targets); err != nil {
		return err
	}
	st, err := install.Uninstall(targets...)
	printTarget(st.Claude)
	printTarget(st.Codex)
	return err
}

func cmdStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fmt.Println("接入状态：")
	st := install.Query()
	printTarget(st.Claude)
	printTarget(st.Codex)

	fmt.Println("\n配置（" + config.Path() + "）：")
	fmt.Println("  " + mark(cfg.ChannelEnabled(config.ChannelFeishu) && cfg.FeishuReady()) + " 飞书 webhook：" + describeWebhook(cfg))
	fmt.Println("  " + mark(cfg.ChannelEnabled(config.ChannelBark) && cfg.BarkReady()) + " Bark endpoint：" + describeBark(cfg))
	fmt.Println("  " + mark(cfg.ChannelEnabled(config.ChannelDingTalk) && cfg.DingTalkReady()) + " 钉钉 webhook：" + describeEndpoint(cfg.DingTalkReady(), cfg.DingTalk.WebhookURL))
	fmt.Println("  " + mark(cfg.ChannelEnabled(config.ChannelWeCom) && cfg.WeComReady()) + " 企微 webhook：" + describeEndpoint(cfg.WeComReady(), cfg.WeCom.WebhookURL))
	fmt.Println("  " + mark(cfg.ChannelEnabled(config.ChannelTelegram) && cfg.TelegramReady()) + " Telegram bot：" + describeTelegram(cfg))
	fmt.Println("  " + mark(cfg.ChannelEnabled(config.ChannelEmail) && cfg.EmailReady()) + " Email SMTP：" + describeEmail(cfg))
	fmt.Printf("  · 渠道开关：feishu=%s bark=%s dingtalk=%s wecom=%s telegram=%s email=%s\n",
		onOff(cfg.ChannelEnabled(config.ChannelFeishu)), onOff(cfg.ChannelEnabled(config.ChannelBark)),
		onOff(cfg.ChannelEnabled(config.ChannelDingTalk)), onOff(cfg.ChannelEnabled(config.ChannelWeCom)),
		onOff(cfg.ChannelEnabled(config.ChannelTelegram)), onOff(cfg.ChannelEnabled(config.ChannelEmail)))
	fmt.Printf("  · 标题字段：device-name=%s agent-name=%s project-name=%s\n",
		onOff(cfg.ShowDeviceName), onOff(cfg.ShowAgentName), onOff(cfg.ShowProjectName))
	fmt.Println("  · 设备名称：" + cfg.EffectiveDeviceName())
	fmt.Printf("  · Agent 名称：claude=%s codex=%s\n",
		cfg.EffectiveAgentName(event.SourceClaude), cfg.EffectiveAgentName(event.SourceCodex))
	if cfg.Feishu.Secret != "" {
		fmt.Println("  · 飞书签名校验：已启用")
	}
	if cfg.DingTalk.Secret != "" {
		fmt.Println("  · 钉钉签名校验：已启用")
	}
	if cfg.MinDurationSeconds > 0 {
		fmt.Printf("  · 耗时阈值：%ds\n", cfg.MinDurationSeconds)
	}
	fmt.Printf("  · 来源开关：claude=%s codex=%s\n",
		onOff(cfg.SourceEnabled(event.SourceClaude)), onOff(cfg.SourceEnabled(event.SourceCodex)))

	return nil
}

func cmdConfig(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(args) < 2 {
		return fmt.Errorf("用法：acn config <配置项> <值>，运行 acn help 查看全部配置项")
	}

	key, value := args[0], strings.TrimSpace(args[1])
	switch key {
	case "feishu-url":
		if strings.EqualFold(value, "off") {
			cfg.SetChannelEnabled(config.ChannelFeishu, false)
		} else {
			cfg.Feishu.WebhookURL = value
			cfg.SetChannelEnabled(config.ChannelFeishu, true)
		}
	case "feishu-secret":
		if strings.EqualFold(value, "off") {
			cfg.Feishu.Secret = ""
		} else {
			cfg.Feishu.Secret = value
		}
	case "bark-url":
		if strings.EqualFold(value, "off") {
			cfg.SetChannelEnabled(config.ChannelBark, false)
		} else {
			cfg.Bark.URL = value
			cfg.SetChannelEnabled(config.ChannelBark, true)
		}
	case "dingtalk-url":
		if strings.EqualFold(value, "off") {
			cfg.SetChannelEnabled(config.ChannelDingTalk, false)
		} else {
			cfg.DingTalk.WebhookURL = value
			cfg.SetChannelEnabled(config.ChannelDingTalk, true)
		}
	case "dingtalk-secret":
		if strings.EqualFold(value, "off") {
			cfg.DingTalk.Secret = ""
		} else {
			cfg.DingTalk.Secret = value
		}
	case "wecom-url":
		if strings.EqualFold(value, "off") {
			cfg.SetChannelEnabled(config.ChannelWeCom, false)
		} else {
			cfg.WeCom.WebhookURL = value
			cfg.SetChannelEnabled(config.ChannelWeCom, true)
		}
	case "telegram-token":
		if strings.EqualFold(value, "off") {
			cfg.SetChannelEnabled(config.ChannelTelegram, false)
		} else {
			cfg.Telegram.BotToken = value
			cfg.SetChannelEnabled(config.ChannelTelegram, true)
		}
	case "telegram-chat-id":
		cfg.Telegram.ChatID = value
		cfg.SetChannelEnabled(config.ChannelTelegram, true)
	case "email-smtp":
		if strings.EqualFold(value, "off") {
			cfg.SetChannelEnabled(config.ChannelEmail, false)
		} else {
			cfg.Email.SMTPAddress = value
			cfg.SetChannelEnabled(config.ChannelEmail, true)
		}
	case "email-username":
		cfg.Email.Username = value
	case "email-password":
		if strings.EqualFold(value, "off") {
			cfg.Email.Password = ""
		} else {
			cfg.Email.Password = value
		}
	case "email-from":
		cfg.Email.From = value
	case "email-to":
		cfg.Email.To = value
	case "feishu", "bark", "dingtalk", "wecom", "telegram", "email":
		on, err := parseBool(value)
		if err != nil {
			return err
		}
		cfg.SetChannelEnabled(key, on)
	case "min-duration":
		n, err := parseSeconds(value)
		if err != nil {
			return err
		}
		cfg.MinDurationSeconds = n
	case "device-name":
		if strings.EqualFold(value, "auto") {
			cfg.DeviceName = ""
		} else {
			cfg.DeviceName = value
		}
		cfg.ShowDeviceName = true
	case "show-device-name", "show-agent-name", "show-project-name":
		on, err := parseBool(value)
		if err != nil {
			return err
		}
		switch key {
		case "show-device-name":
			cfg.ShowDeviceName = on
		case "show-agent-name":
			cfg.ShowAgentName = on
		case "show-project-name":
			cfg.ShowProjectName = on
		}
	case "claude-agent-name", "codex-agent-name":
		source := strings.TrimSuffix(key, "-agent-name")
		if cfg.AgentNames == nil {
			cfg.AgentNames = map[string]string{}
		}
		if strings.EqualFold(value, "auto") {
			delete(cfg.AgentNames, source)
		} else {
			cfg.AgentNames[source] = value
		}
		cfg.ShowAgentName = true
	case "claude", "codex":
		on, err := parseBool(value)
		if err != nil {
			return err
		}
		if cfg.Sources == nil {
			cfg.Sources = map[string]bool{}
		}
		cfg.Sources[key] = on
	default:
		return fmt.Errorf("未知配置项 %q，运行 acn help 查看全部配置项", key)
	}

	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("已更新 %s\n", key)

	// 环境变量优先级高于配置文件，静默失效会很难排查。
	if (key == "feishu-url" && os.Getenv(config.EnvWebhook) != "") ||
		(key == "feishu-secret" && os.Getenv(config.EnvSecret) != "") ||
		(key == "device-name" && os.Getenv(config.EnvDeviceName) != "") ||
		(key == "bark-url" && os.Getenv(config.EnvBarkURL) != "") ||
		(key == "dingtalk-url" && os.Getenv(config.EnvDingTalkWebhook) != "") ||
		(key == "dingtalk-secret" && os.Getenv(config.EnvDingTalkSecret) != "") ||
		(key == "wecom-url" && os.Getenv(config.EnvWeComWebhook) != "") ||
		(key == "telegram-token" && os.Getenv(config.EnvTelegramToken) != "") ||
		(key == "telegram-chat-id" && os.Getenv(config.EnvTelegramChatID) != "") ||
		(key == "email-smtp" && os.Getenv(config.EnvEmailSMTP) != "") ||
		(key == "email-username" && os.Getenv(config.EnvEmailUsername) != "") ||
		(key == "email-password" && os.Getenv(config.EnvEmailPassword) != "") ||
		(key == "email-from" && os.Getenv(config.EnvEmailFrom) != "") ||
		(key == "email-to" && os.Getenv(config.EnvEmailTo) != "") {
		fmt.Println("注意：同名环境变量已设置，其值会覆盖此处配置")
	}
	return nil
}

func printTarget(t install.TargetStatus) {
	fmt.Printf("  %s %s：%s\n", mark(t.Installed), t.Name, t.Detail)
	if t.Warning != "" {
		fmt.Printf("    ⚠ %s\n", t.Warning)
	}
}

func describeWebhook(cfg config.Config) string {
	if !cfg.FeishuReady() {
		return "未配置"
	}
	return maskURL(cfg.Feishu.WebhookURL)
}

func describeBark(cfg config.Config) string {
	if !cfg.BarkReady() {
		return "未配置"
	}
	return maskURL(cfg.Bark.URL)
}

func describeEndpoint(ready bool, endpoint string) string {
	if !ready {
		return "未配置"
	}
	return maskURL(endpoint)
}

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

// maskURL 隐藏通知端点末段的 token——status 的输出常被贴进聊天记录。
func maskURL(u string) string {
	parsed, err := url.Parse(strings.TrimSpace(u))
	if err != nil || parsed.Host == "" {
		return "已配置（地址格式异常）"
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	idx := strings.LastIndex(path, "/")
	if idx < 0 || idx == len(path)-1 {
		return parsed.Scheme + "://" + parsed.Host + path
	}
	token := path[idx+1:]
	masked := "****"
	if len(token) > 8 {
		masked = token[:4] + "…" + token[len(token)-4:]
	}
	return parsed.Scheme + "://" + parsed.Host + path[:idx+1] + masked
}

func parseSeconds(v string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return 0, fmt.Errorf("min-duration 需为非负整数秒，收到 %q", v)
	}
	return n, nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("需为 on 或 off，收到 %q", v)
	}
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "×"
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
