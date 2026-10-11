package notify

import (
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/message"
)

const timeLayout = "2006-01-02 15:04:05"

// render 按配置把事件渲染成可投递的通知。now 为投递时刻，已换算到通知时区。
func render(cfg config.Config, ev event.Event, now time.Time) message.Message {
	reply := strings.TrimSpace(ev.Message)
	if cfg.MaxMessageLength > 0 {
		reply = truncate(reply, cfg.MaxMessageLength)
	}
	return message.Message{
		Source:    ev.Source,
		SessionID: strings.TrimSpace(ev.SessionID),
		Title:     title(cfg, ev),
		Reply:     reply,
		Details:   details(ev, now),
	}
}

// title 默认使用会话名；设备、Agent 与项目名可按配置作为前缀。
// 取不到会话名时使用已启用的前缀，全部为空则退回通用标题。
func title(cfg config.Config, ev event.Event) string {
	parts := make([]string, 0, 4)
	if cfg.ShowDeviceName {
		parts = append(parts, cfg.EffectiveDeviceName())
	}
	if cfg.ShowAgentName {
		if agent := cfg.EffectiveAgentName(ev.Source); agent != "" {
			parts = append(parts, agent)
		}
	}
	if cfg.ShowProjectName {
		if project := ev.Project(); project != "" {
			parts = append(parts, project)
		}
	}
	if session := strings.TrimSpace(ev.SessionName); session != "" {
		return strings.Join(append(parts, session), "-")
	}
	if len(parts) == 0 {
		return "任务完成"
	}
	return strings.Join(parts, "-") + " 任务完成"
}

// details 返回正文末尾的详情行。等待介入事件发生在回合中间，不显示完成时间与耗时。
func details(ev event.Event, now time.Time) []string {
	var lines []string
	switch ev.Kind {
	case event.KindCompletion:
		lines = append(lines, "完成时间："+now.Format(timeLayout))
		if d := event.FormatDuration(ev.DurationMS); d != "" {
			lines = append(lines, "耗时："+d)
		}
	case event.KindFailure:
		lines = append(lines, "发生时间："+now.Format(timeLayout))
	}
	if cwd := strings.TrimSpace(ev.Cwd); cwd != "" {
		lines = append(lines, "目录："+cwd)
	}
	return lines
}

// truncate 按字符（而非字节）截断，避免切断中文。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}
