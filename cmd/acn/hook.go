package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/claude"
	"github.com/windyskr/agent-completion-notification/internal/codex"
	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/eventlog"
	"github.com/windyskr/agent-completion-notification/internal/hook"
	"github.com/windyskr/agent-completion-notification/internal/hookdedup"
	"github.com/windyskr/agent-completion-notification/internal/notify"
	"github.com/windyskr/agent-completion-notification/internal/opencode"
	"github.com/windyskr/agent-completion-notification/internal/paseo"
)

// cmdHook 处理来自 AI CLI 的回调。
//
// 无论内部发生什么都返回 nil，且**绝不往 stdout 写任何东西**：
//   - 非零退出码会在用户终端里显示报错；
//   - Codex 的 Stop hook 若在 stdout 收到 {"decision":"block"}，会自动续跑一轮。
//
// 通知失败远不如打断工作流严重，因此一切诊断信息只走 stderr。
func cmdHook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：acn hook <claude|claude-question|claude-notification|claude-failure|codex|codex-permission|opencode>")
	}
	runHook(args[0], os.Stdin, os.Stderr)
	return nil
}

// runHook 是 hook 的完整处理流程：解析载荷 → 并发去重 → Paseo 补全 → 投递。
// 每一步的结论都记进同一条事件日志，流程结束时统一写出。
func runHook(source string, stdin io.Reader, stderr io.Writer) {
	started := time.Now()
	raw, err := hook.ReadRaw(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "acn hook: "+err.Error())
		return
	}

	cfg, cfgErr := config.Load()
	entry := eventlog.Entry{
		Timestamp:         started,
		HookSource:        source,
		RawPayload:        string(raw),
		RawPayloadPreview: eventlog.Preview(string(raw), 10),
		PaseoAgentID:      strings.TrimSpace(os.Getenv("PASEO_AGENT_ID")),
		PaseoTerminalID:   strings.TrimSpace(os.Getenv("PASEO_TERMINAL_ID")),
	}
	defer func() {
		if cfgErr != nil || !cfg.EventLogEnabled {
			return
		}
		if entry.Event != nil {
			entry.MessagePreview = eventlog.Preview(entry.Event.Message, 10)
		}
		entry.ElapsedMS = time.Since(started).Milliseconds()
		if err := eventlog.Append(config.EventLogPath(), entry); err != nil {
			fmt.Fprintln(stderr, "acn: 写入事件日志失败: "+err.Error())
		}
	}()
	markSkipped := func(ev event.Event, reason string) {
		entry.Event = &ev
		entry.Skip = true
		entry.SkippedReason = reason
	}

	ev, skip, err := buildHookEvent(source, bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintln(stderr, "acn hook: "+err.Error())
		entry.ParseError = err.Error()
		return
	}
	if skip {
		markSkipped(ev, "上游事件无需推送")
		return
	}

	claimed, release, claimErr := hookdedup.Claim(source, ev.SessionID)
	switch {
	case claimErr != nil:
		fmt.Fprintln(stderr, "acn: 并发事件判定失败: "+claimErr.Error())
	case !claimed:
		markSkipped(ev, "重复 hook 事件")
		return
	default:
		defer release()
	}

	details := enrichPaseoEvent(source, &ev)
	entry.PaseoAgentName = details.AgentName
	entry.PaseoAgentLookupError = details.AgentLookupError
	entry.PaseoWorkspaceTitle = details.WorkspaceTitle
	entry.PaseoWorkspaceLookupError = details.WorkspaceLookupError
	entry.PaseoTitleGeneration = details.TitleGeneration
	if details.TitleGeneration {
		markSkipped(ev, "Paseo 标题生成任务")
		return
	}

	// 曾经这里有一条「先丢给常驻 daemon 异步发」的快路径，实测只省 78ms
	// （本地开销 45ms 两者相同，差的仅是一次约 80ms 的飞书往返），
	// 却要付出常驻服务、socket 生命周期与两套投递路径的代价，已移除。
	if cfgErr != nil {
		fmt.Fprintln(stderr, "acn: 加载配置失败: "+cfgErr.Error())
		return
	}
	report, err := notify.SendWithReport(context.Background(), cfg, ev)
	entry.Event = &report.Event
	entry.Skip = report.SkippedReason != ""
	entry.SkippedReason = report.SkippedReason
	entry.Delivery = report.DeliveryResult
	entry.Warnings = report.Warnings
	for _, warning := range report.Warnings {
		fmt.Fprintln(stderr, "acn: "+warning)
	}
	switch {
	case err != nil:
		entry.DeliveryError = err.Error()
		fmt.Fprintln(stderr, "acn: 推送失败: "+err.Error())
	case report.SkippedReason != "":
		fmt.Fprintln(stderr, "acn: 未推送（"+report.SkippedReason+"）")
	}
}

// buildHookEvent 解析各 CLI 通过 stdin 发来的载荷并按来源归一化。
// Claude Code 与 Codex 共用 Stop 契约，OpenCode 使用 ACN 插件载荷；
// claude-question / claude-notification 是 Claude Code 回合中的等待介入事件。
// skip 为 true 表示该载荷无需推送（如无需行动的通知类型），调用方应静默返回。
func buildHookEvent(source string, stdin io.Reader) (ev event.Event, skip bool, err error) {
	switch source {
	case "claude":
		payload, err := hook.ReadStop(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return claude.FromPayload(payload), false, nil
	case "claude-question":
		payload, err := hook.ReadPreToolUse(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return claude.FromQuestionPayload(payload), false, nil
	case "claude-notification":
		payload, err := hook.ReadNotification(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		ev, ok := claude.FromNotificationPayload(payload)
		return ev, !ok, nil
	case "claude-failure":
		payload, err := hook.ReadStopFailure(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return claude.FromStopFailurePayload(payload), false, nil
	case "codex":
		payload, err := hook.ReadStop(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return codex.FromPayload(payload, time.Now()), false, nil
	case "codex-permission":
		payload, err := hook.ReadPermissionRequest(stdin)
		if err != nil {
			return event.Event{}, false, err
		}
		return codex.FromPermissionRequestPayload(payload), false, nil
	case "opencode":
		ev, err := opencode.FromReader(stdin)
		return ev, false, err
	default:
		return event.Event{}, false, fmt.Errorf("未知来源 %q", source)
	}
}

type paseoEventDetails struct {
	AgentName            string
	AgentLookupError     string
	WorkspaceTitle       string
	WorkspaceLookupError string
	TitleGeneration      bool
}

// enrichPaseoEvent 回填 Paseo 的 Workspace 标题。Agent 名称通常来自初始 prompt，
// 仅在 Workspace 标题不可用时作为回退。
func enrichPaseoEvent(hookSource string, ev *event.Event) paseoEventDetails {
	if (hookSource != "codex" && hookSource != "claude") || strings.TrimSpace(ev.SessionName) != "" {
		return paseoEventDetails{}
	}
	agentID := strings.TrimSpace(os.Getenv("PASEO_AGENT_ID"))
	if agentID == "" {
		return paseoEventDetails{}
	}
	workspaceTitle, workspaceErr := paseo.WorkspaceTitle(agentID)
	details := paseoEventDetails{WorkspaceTitle: workspaceTitle}
	if workspaceErr != nil {
		details.WorkspaceLookupError = workspaceErr.Error()
	}
	if workspaceTitle != "" {
		ev.SessionName = workspaceTitle
		return details
	}
	agentName, err := paseo.AgentName(agentID)
	details.AgentName = agentName
	if err != nil {
		details.AgentLookupError = err.Error()
	} else if agentName != "" {
		ev.SessionName = agentName
		return details
	}
	if hookSource == "codex" && paseo.IsTitleGenerationMessage(ev.Message) {
		details.TitleGeneration = true
	}
	return details
}
