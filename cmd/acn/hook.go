package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/windyskr/acn/internal/claude"
	"github.com/windyskr/acn/internal/codex"
	"github.com/windyskr/acn/internal/config"
	"github.com/windyskr/acn/internal/event"
)

// parseClaudeHook 解析 Claude Code 的 Stop hook 载荷。
func parseClaudeHook(stdin io.Reader) (event.Event, error) {
	return claude.Parse(stdin)
}

// parseCodexHook 解析 Codex 的 notify 载荷，并把原始参数转发给安装 acn 之前
// 配置的程序。
//
// 转发在解析之后、发送之前完成：解析失败也要转发（不能因为 acn 看不懂载荷就
// 弄坏用户既有的集成），但转发失败不影响 acn 自己推送。
func parseCodexHook(ctx context.Context, args []string) (event.Event, error) {
	payload, parseErr := codex.Parse(args)

	if chain := loadNotifyChain(); len(chain) > 0 {
		if err := codex.Forward(ctx, chain, args); err != nil {
			fmt.Fprintln(os.Stderr, "acn hook: "+err.Error())
		}
	}

	if parseErr != nil {
		return event.Event{}, parseErr
	}
	// 只有「一轮结束」需要通知；其余类型静默忽略，但上面的转发已经完成。
	if !payload.IsTurnComplete() {
		return event.Event{}, errSkip
	}
	return payload.ToEvent(), nil
}

// errSkip 表示该回调无需通知，且不算异常。调用方据此静默返回。
var errSkip = errors.New("该事件无需通知")

// loadNotifyChain 读取被 acn 接管的原 Codex notify 程序。
func loadNotifyChain() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return cfg.CodexNotifyChain
}
