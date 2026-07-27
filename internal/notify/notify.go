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
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/feishu"
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
	return notifiers
}
