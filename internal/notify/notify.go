// Package notify 决定一个事件该不该推，并并发投递到所有已启用渠道。
package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/message"
)

// SendTimeout 是单次投递的整体超时。
//
// 取值要小：hook 会阻塞 CLI 返回，任一渠道若卡住，用户就得在任务末尾等待。
// 正常请求在百毫秒量级，3 秒已是宽裕的上限。
const SendTimeout = 3 * time.Second

// Notifier 是通知渠道的最小契约。文案由 render 统一生成，渠道只负责排版、协议
// 转换与传输。
type Notifier interface {
	Name() string
	Send(context.Context, message.Message) error
}

// DeliveryResult 记录一个通知渠道的本次投递结果。
type DeliveryResult struct {
	Channel string `json:"channel"`
	Sent    bool   `json:"sent"`
	Error   string `json:"error,omitempty"`
}

// Report 记录一次通知判定与投递的完整结果。
type Report struct {
	Event event.Event `json:"event"`
	// Message 是实际投递的通知；被跳过时为 nil。
	Message        *message.Message `json:"message,omitempty"`
	SkippedReason  string           `json:"skipped_reason,omitempty"`
	DeliveryResult []DeliveryResult `json:"delivery"`
	// Warnings 记录不影响投递、但需要用户修正的配置问题。
	Warnings []string `json:"warnings,omitempty"`
}

// Gate 判断该事件是否应当推送，返回的字符串为不推送的原因。
func Gate(cfg config.Config, ev event.Event) string {
	if !cfg.SourceEnabled(ev.Source) {
		return "来源已禁用：" + ev.Source
	}
	if directory, ok := cfg.IgnoredDirectory(ev.Cwd); ok {
		return "目录已忽略：" + directory
	}
	if cfg.SkipUntitled && ev.Kind == event.KindCompletion && strings.TrimSpace(ev.SessionName) == "" {
		return "会话标题为空（skip-untitled on）"
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
	report, err := SendWithReport(ctx, cfg, ev)
	return report.SkippedReason, err
}

// SendWithReport 在发送后返回过滤判定和每个渠道的投递结果。
func SendWithReport(ctx context.Context, cfg config.Config, ev event.Event) (Report, error) {
	if reason := Gate(cfg, ev); reason != "" {
		return Report{Event: ev, SkippedReason: reason}, nil
	}
	return Deliver(ctx, cfg, ev)
}

// Deliver 不经 Gate，直接投递到全部已启用渠道。acn doctor 用它验证链路：
// 自检要回答的是「渠道通不通」，不应被来源开关、忽略目录、耗时阈值或
// skip-untitled 等推送规则拦下。
func Deliver(ctx context.Context, cfg config.Config, ev event.Event) (Report, error) {
	report := Report{Event: ev}
	notifiers := configuredNotifiers(cfg)
	if len(notifiers) == 0 {
		report.SkippedReason = "未配置已启用的通知渠道"
		return report, nil
	}
	location, locationErr := cfg.NotificationLocation()
	if locationErr != nil {
		// 时区只影响正文里的时间显示，配置写错不该让整条通知发不出去。
		location = time.Local
		report.Warnings = append(report.Warnings, locationErr.Error()+"，已改用本地时区")
	}
	msg := render(cfg, ev, time.Now().In(location))
	report.Message = &msg

	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()

	errs := make([]error, len(notifiers))
	report.DeliveryResult = make([]DeliveryResult, len(notifiers))
	var wg sync.WaitGroup
	for i, notifier := range notifiers {
		wg.Add(1)
		go func(index int, notifier Notifier) {
			defer wg.Done()
			result := DeliveryResult{Channel: notifier.Name()}
			if sendErr := notifier.Send(ctx, msg); sendErr != nil {
				errs[index] = fmt.Errorf("%s: %w", notifier.Name(), sendErr)
				result.Error = sendErr.Error()
			} else {
				result.Sent = true
			}
			report.DeliveryResult[index] = result
		}(i, notifier)
	}
	wg.Wait()
	return report, errors.Join(errs...)
}
