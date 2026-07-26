// Package notify 定义通知渠道抽象与投递入口。
//
// daemon 与 hook 直发两条路径共用 Dispatch，保证「是否该推送」的判定只有一处实现。
package notify

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/windyskr/acn/internal/config"
	"github.com/windyskr/acn/internal/event"
	"github.com/windyskr/acn/internal/feishu"
)

// Build 按配置装配可用渠道。新增渠道只需在此追加一行，daemon 与 hook 无需改动。
func Build(cfg config.Config) []Notifier {
	var out []Notifier
	if cfg.FeishuReady() {
		out = append(out, feishu.New(cfg.Feishu))
	}
	return out
}

// Notifier 是一个通知渠道。新增渠道只需实现该接口并在 Build 中注册。
type Notifier interface {
	Name() string
	Send(ctx context.Context, ev event.Event) error
}

// Result 是单个渠道的投递结果。Skipped 非空表示未发送及其原因。
type Result struct {
	Channel string
	Skipped string
	Err     error
}

func (r Result) String() string {
	switch {
	case r.Skipped != "":
		return fmt.Sprintf("%s: 跳过（%s）", r.Channel, r.Skipped)
	case r.Err != nil:
		return fmt.Sprintf("%s: 失败（%v）", r.Channel, r.Err)
	default:
		return fmt.Sprintf("%s: 成功", r.Channel)
	}
}

// Gate 判断该事件是否应当推送，返回的字符串为不推送的原因。
// 与渠道无关的过滤都收敛在这里。
func Gate(cfg config.Config, ev event.Event) string {
	if !cfg.SourceEnabled(ev.Source) {
		return "来源已禁用：" + ev.Source
	}
	// 耗时未知（如 Codex）时不套用阈值，否则会把所有通知都挡掉。
	if cfg.MinDurationSeconds > 0 && ev.DurationMS > 0 {
		if ev.DurationMS < int64(cfg.MinDurationSeconds)*1000 {
			return fmt.Sprintf("耗时 %s 低于阈值 %ds",
				event.FormatDuration(ev.DurationMS), cfg.MinDurationSeconds)
		}
	}
	return ""
}

// Dispatch 并发投递到所有已配置渠道。渠道之间互不阻塞，单个失败不影响其余。
func Dispatch(ctx context.Context, cfg config.Config, notifiers []Notifier, ev event.Event) []Result {
	if reason := Gate(cfg, ev); reason != "" {
		return []Result{{Channel: "-", Skipped: reason}}
	}
	if len(notifiers) == 0 {
		return []Result{{Channel: "-", Skipped: "未配置任何通知渠道"}}
	}

	results := make([]Result, len(notifiers))
	var wg sync.WaitGroup
	for i, n := range notifiers {
		wg.Add(1)
		go func(i int, n Notifier) {
			defer wg.Done()
			results[i] = Result{Channel: n.Name(), Err: n.Send(ctx, ev)}
		}(i, n)
	}
	wg.Wait()
	return results
}

// SendTimeout 是单次投递的整体超时。
const SendTimeout = 10 * time.Second
