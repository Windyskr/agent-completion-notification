// Package notify 决定一个事件该不该推，并并发投递到所有已启用渠道。
package notify

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/bark"
	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/dingtalk"
	"github.com/windyskr/agent-completion-notification/internal/email"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/feishu"
	"github.com/windyskr/agent-completion-notification/internal/slack"
	"github.com/windyskr/agent-completion-notification/internal/teams"
	"github.com/windyskr/agent-completion-notification/internal/telegram"
	"github.com/windyskr/agent-completion-notification/internal/wecom"
)

// SendTimeout 是单次投递的整体超时。
//
// 取值要小：hook 会阻塞 CLI 返回，任一渠道若卡住，用户就得在任务末尾等待。
// 正常请求在百毫秒量级，3 秒已是宽裕的上限。
const SendTimeout = 3 * time.Second

// Notifier 是通知渠道的最小契约。文案由 event 统一生成，渠道只负责协议转换与传输。
type Notifier interface {
	Name() string
	Send(context.Context, event.Event) error
}

// Gate 判断该事件是否应当推送，返回的字符串为不推送的原因。
func Gate(cfg config.Config, ev event.Event) string {
	if !cfg.SourceEnabled(ev.Source) {
		return "来源已禁用：" + ev.Source
	}
	if directory, ok := cfg.IgnoredDirectory(ev.Cwd); ok {
		return "目录已忽略：" + directory
	}
	// 等待介入事件有独立开关：Claude 的选择与权限、Codex 的权限默认开，
	// Claude 的 60 秒空闲提醒默认关。
	// 它们发生在回合中间，来源关闭（如 claude off）时同样不推。
	switch ev.Kind {
	case event.KindChoice:
		if !cfg.ClaudeAttentionEnabled() {
			return "等待介入推送已关闭（claude-attention off）"
		}
	case event.KindPermission:
		if ev.Source == event.SourceCodex && !cfg.CodexAttentionEnabled() {
			return "权限确认推送已关闭（codex-attention off）"
		}
		if ev.Source == event.SourceClaude && !cfg.ClaudeAttentionEnabled() {
			return "等待介入推送已关闭（claude-attention off）"
		}
	case event.KindIdle:
		if !cfg.ClaudeIdleReminderEnabled() {
			return "空闲提醒已关闭（claude-idle-reminder off）"
		}
	case event.KindFailure:
		if ev.Source == event.SourceClaude && !cfg.ClaudeFailureAlertEnabled() {
			return "异常终止推送已关闭（claude-failure-alert off）"
		}
	}
	// 耗时未知时不套用阈值，否则会把取不到起点的通知全部挡掉。
	if cfg.MinDurationSeconds > 0 && ev.DurationMS > 0 {
		if ev.DurationMS < int64(cfg.MinDurationSeconds)*1000 {
			return fmt.Sprintf("耗时 %s 低于阈值 %ds",
				event.FormatDuration(ev.DurationMS), cfg.MinDurationSeconds)
		}
	}
	return ""
}

// Send 先过 Gate 再投递。skipped 非空表示按规则未发送，此时 err 为 nil。
func Send(ctx context.Context, cfg config.Config, ev event.Event) (skipped string, err error) {
	if reason := Gate(cfg, ev); reason != "" {
		return reason, nil
	}
	notifiers := configuredNotifiers(cfg)
	if len(notifiers) == 0 {
		return "未配置已启用的通知渠道", nil
	}
	if ev.DeviceName == "" {
		ev.DeviceName = cfg.EffectiveDeviceName()
	}
	ev.AgentName = cfg.EffectiveAgentName(ev.Source)
	ev.ShowDeviceName = cfg.ShowDeviceName
	ev.HideAgentName = !cfg.ShowAgentName
	ev.HideProjectName = !cfg.ShowProjectName
	ev.SetMaxMessageLength(cfg.MaxMessageLength)
	location, locationErr := cfg.NotificationLocation()
	if locationErr != nil {
		return "", locationErr
	}
	ev.SetLocation(location)

	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()

	errs := make([]error, len(notifiers))
	var wg sync.WaitGroup
	for i, notifier := range notifiers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sendErr := notifier.Send(ctx, ev); sendErr != nil {
				errs[i] = fmt.Errorf("%s: %w", notifier.Name(), sendErr)
			}
		}()
	}
	wg.Wait()
	return "", errors.Join(errs...)
}

func configuredNotifiers(cfg config.Config) []Notifier {
	var notifiers []Notifier
	if cfg.ChannelEnabled(config.ChannelFeishu) && cfg.FeishuReady() {
		notifiers = append(notifiers, feishu.New(cfg.Feishu))
	}
	if cfg.ChannelEnabled(config.ChannelBark) && cfg.BarkReady() {
		notifiers = append(notifiers, bark.New(cfg.Bark))
	}
	if cfg.ChannelEnabled(config.ChannelDingTalk) && cfg.DingTalkReady() {
		notifiers = append(notifiers, dingtalk.New(cfg.DingTalk))
	}
	if cfg.ChannelEnabled(config.ChannelWeCom) && cfg.WeComReady() {
		notifiers = append(notifiers, wecom.New(cfg.WeCom))
	}
	if cfg.ChannelEnabled(config.ChannelTelegram) && cfg.TelegramReady() {
		notifiers = append(notifiers, telegram.New(cfg.Telegram))
	}
	if cfg.ChannelEnabled(config.ChannelEmail) && cfg.EmailReady() {
		notifiers = append(notifiers, email.New(cfg.Email))
	}
	if cfg.ChannelEnabled(config.ChannelSlack) && cfg.SlackReady() {
		notifiers = append(notifiers, slack.New(cfg.Slack))
	}
	if cfg.ChannelEnabled(config.ChannelTeams) && cfg.TeamsReady() {
		notifiers = append(notifiers, teams.New(cfg.Teams))
	}
	return notifiers
}
