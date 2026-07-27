// Package notify 决定一个事件该不该推、并把它推出去。
//
// 目前只有飞书一个渠道，所以这里不做渠道抽象——接口、并发派发、结果汇总
// 都是为「多个渠道」准备的，而那个多样性并不存在。真要加第二个渠道时
// 再引入抽象，成本远低于一直背着它。
package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/feishu"
)

// SendTimeout 是单次投递的整体超时。
//
// 取值要小：hook 会阻塞 CLI 返回，飞书若卡住，用户就得在任务末尾干等这么久。
// 正常一次往返约 80ms，3 秒已是极宽裕的上限。
const SendTimeout = 3 * time.Second

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
	if !cfg.FeishuReady() {
		return "未配置飞书 webhook", nil
	}
	if ev.DeviceName == "" {
		ev.DeviceName = cfg.EffectiveDeviceName()
	}
	ev.AgentName = cfg.EffectiveAgentName(ev.Source)
	ev.ShowDeviceName = cfg.ShowDeviceName
	ev.HideProjectName = !cfg.ShowProjectName

	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	return "", feishu.New(cfg.Feishu).Send(ctx, ev)
}
