package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/message"
)

var renderNow = time.Date(2026, 7, 26, 15, 4, 5, 0, time.UTC)

func TestTitle(t *testing.T) {
	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", SessionName: "修复登录问题"}
	cases := []struct {
		name string
		cfg  config.Config
		ev   event.Event
		want string
	}{
		// 产品默认关闭所有可选前缀，只显示会话名。
		{"默认只显示会话名", config.Config{DeviceName: "MacBookPro"}, ev, "修复登录问题"},
		// 设备、Agent 与项目名仍可作为前缀，且可独立开关。
		{"全部前缀", config.Config{DeviceName: "MacBookPro", ShowDeviceName: true, ShowAgentName: true, ShowProjectName: true}, ev, "MacBookPro-claude-acn-修复登录问题"},
		{"隐藏项目名", config.Config{DeviceName: "MacBookPro", ShowDeviceName: true, ShowAgentName: true}, ev, "MacBookPro-claude-修复登录问题"},
		{"只显示设备名", config.Config{DeviceName: "MacBookPro", ShowDeviceName: true}, ev, "MacBookPro-修复登录问题"},
		// 配置的 Agent 名优先于来源默认名称。
		{"自定义 Agent 名", config.Config{ShowAgentName: true, ShowProjectName: true, AgentNames: map[string]string{"claude": "opus"}},
			event.Event{Source: event.SourceClaude, Cwd: "/work/acn", SessionName: "排查构建"}, "opus-acn-排查构建"},
		// 无 cwd 时不应出现空项目名。
		{"无 cwd", config.Config{DeviceName: "workstation", ShowDeviceName: true, ShowAgentName: true, ShowProjectName: true},
			event.Event{Source: event.SourceCodex, SessionName: "更新文档"}, "workstation-Codex-更新文档"},
		// 取不到会话名时保持旧标题降级行为。
		{"无会话名有前缀", config.Config{ShowAgentName: true}, event.Event{Source: event.SourceCodex}, "Codex 任务完成"},
		{"无会话名无前缀", config.Config{}, event.Event{Source: event.SourceCodex}, "任务完成"},
	}
	for _, c := range cases {
		if got := render(c.cfg, c.ev, renderNow).Title; got != c.want {
			t.Errorf("%s: Title = %q, 期望 %q", c.name, got, c.want)
		}
	}
}

// 耗时未知时正文里不该出现「耗时」行。
func TestRenderOmitsUnknownDuration(t *testing.T) {
	msg := render(config.Config{}, event.Event{Source: event.SourceCodex, Cwd: "/work/acn", Message: "完成"}, renderNow)

	want := "完成\n\n完成时间：2026-07-26 15:04:05\n目录：/work/acn"
	if got := msg.Body(message.Format{}); got != want {
		t.Errorf("正文 = %q, 期望 %q", got, want)
	}
}

func TestRenderIncludesKnownDuration(t *testing.T) {
	msg := render(config.Config{}, event.Event{
		Source: event.SourceClaude, Cwd: "/work/acn", Message: "任务已经完成", DurationMS: 90_000,
	}, renderNow)

	want := "任务已经完成\n\n完成时间：2026-07-26 15:04:05\n耗时：1m30s\n目录：/work/acn"
	if got := msg.Body(message.Format{}); got != want {
		t.Errorf("正文顺序错误:\n得到：%q\n期望：%q", got, want)
	}
}

func TestRenderWithoutMessageStartsWithDetails(t *testing.T) {
	msg := render(config.Config{}, event.Event{Source: event.SourceClaude, Cwd: "/work/acn"}, renderNow)

	if got := msg.Body(message.Format{}); got != "完成时间：2026-07-26 15:04:05\n目录：/work/acn" {
		t.Errorf("无回复时正文格式错误：%q", got)
	}
}

// 等待介入事件发生在回合中间，不显示完成时间与耗时；异常终止显示发生时间。
func TestRenderDetailsByKind(t *testing.T) {
	cases := map[string][]string{
		event.KindPermission: {"目录：/work/acn"},
		event.KindFailure:    {"发生时间：2026-07-26 15:04:05", "目录：/work/acn"},
	}
	for kind, want := range cases {
		got := render(config.Config{}, event.Event{Kind: kind, Cwd: "/work/acn", DurationMS: 5000}, renderNow).Details
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: Details = %q, 期望 %q", kind, got, want)
		}
	}
}

func TestRenderUsesNotificationTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	msg := render(config.Config{}, event.Event{Source: event.SourceCodex}, renderNow.In(location))
	if got, want := msg.Body(message.Format{}), "完成时间：2026-07-26 23:04:05"; got != want {
		t.Errorf("配置上海时区后的正文 = %q，期望 %q", got, want)
	}
}

// 超长回复要按字符截断（不能切断中文），并加省略号；0 表示不截断。
func TestRenderTruncatesLongMessage(t *testing.T) {
	long := strings.Repeat("中", event.DefaultMaxMessageLength+100)
	reply := render(config.Config{MaxMessageLength: event.DefaultMaxMessageLength}, event.Event{Message: long}, renderNow).Reply

	if !strings.HasSuffix(reply, "…") {
		t.Error("超长正文未被截断")
	}
	if got := strings.Count(reply, "中"); got != event.DefaultMaxMessageLength {
		t.Errorf("截断后剩 %d 个字符，期望 %d", got, event.DefaultMaxMessageLength)
	}
	if got := render(config.Config{}, event.Event{Message: long}, renderNow).Reply; got != long {
		t.Error("max-message-length 为 0 时不应截断")
	}
}
