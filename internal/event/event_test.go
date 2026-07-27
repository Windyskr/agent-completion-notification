package event

import (
	"strings"
	"testing"
	"time"
)

func TestProject(t *testing.T) {
	cases := map[string]string{
		"/Users/me/Codespace/acn": "acn",
		"/work/proj/":             "proj",
		"":                        "",
	}
	for cwd, want := range cases {
		if got := (Event{Cwd: cwd}).Project(); got != want {
			t.Errorf("Project(%q) = %q, 期望 %q", cwd, got, want)
		}
	}
}

func TestTitle(t *testing.T) {
	// 默认隐藏设备名、显示项目名。
	ev := Event{DeviceName: "MacBookPro", Source: SourceClaude, Cwd: "/work/acn"}
	if got, want := ev.Title(), "claude-acn 任务完成"; got != want {
		t.Errorf("Title = %q, 期望 %q", got, want)
	}
	// 显式开启设备名。
	ev.ShowDeviceName = true
	if got, want := ev.Title(), "MacBookPro-claude-acn 任务完成"; got != want {
		t.Errorf("Title = %q, 期望 %q", got, want)
	}
	// 项目名可以独立隐藏。
	ev.HideProjectName = true
	if got, want := ev.Title(), "MacBookPro-claude 任务完成"; got != want {
		t.Errorf("Title = %q, 期望 %q", got, want)
	}
	// 配置名称优先于来源默认名称。
	if got := (Event{AgentName: "opus", Source: SourceClaude, Cwd: "/work/acn"}).Title(); got != "opus-acn 任务完成" {
		t.Errorf("Title = %q", got)
	}
	// 无 cwd 时不应出现空项目名。
	if got := (Event{DeviceName: "workstation", ShowDeviceName: true, Source: SourceCodex}).Title(); got != "workstation-Codex 任务完成" {
		t.Errorf("Title = %q", got)
	}
	// 默认设备名不参与标题。
	if got := (Event{Source: SourceCodex}).Title(); got != "Codex 任务完成" {
		t.Errorf("Title = %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{
		0:         "",
		-1:        "",
		5_000:     "5s",
		65_000:    "1m5s",
		3_600_000: "1h0m",
		7_320_000: "2h2m",
	}
	for ms, want := range cases {
		if got := FormatDuration(ms); got != want {
			t.Errorf("FormatDuration(%d) = %q, 期望 %q", ms, got, want)
		}
	}
}

// 耗时未知时正文里不该出现「耗时」行。
func TestBodyOmitsUnknownDuration(t *testing.T) {
	now := time.Date(2026, 7, 26, 15, 4, 5, 0, time.UTC)
	body := Event{Source: SourceCodex, Cwd: "/work/acn", Message: "完成"}.Body(now)

	if strings.Contains(body, "耗时") {
		t.Errorf("耗时未知却出现了耗时行:\n%s", body)
	}
	for _, want := range []string{"2026-07-26 15:04:05", "/work/acn", "完成"} {
		if !strings.Contains(body, want) {
			t.Errorf("正文缺少 %q:\n%s", want, body)
		}
	}
}

func TestBodyIncludesKnownDuration(t *testing.T) {
	now := time.Date(2026, 7, 26, 15, 4, 5, 0, time.UTC)
	body := Event{
		Source: SourceClaude, Cwd: "/work/acn", Message: "任务已经完成", DurationMS: 90_000,
	}.Body(now)

	if !strings.Contains(body, "耗时：1m30s") {
		t.Errorf("正文缺少耗时:\n%s", body)
	}
	want := "任务已经完成\n\n完成时间：2026-07-26 15:04:05\n耗时：1m30s\n目录：/work/acn"
	if body != want {
		t.Errorf("正文顺序错误:\n得到：%q\n期望：%q", body, want)
	}
}

func TestBodyWithoutMessageStartsWithDetails(t *testing.T) {
	now := time.Date(2026, 7, 26, 15, 4, 5, 0, time.UTC)
	body := Event{Source: SourceClaude, Cwd: "/work/acn"}.Body(now)

	if strings.HasPrefix(body, "\n") || body != "完成时间：2026-07-26 15:04:05\n目录：/work/acn" {
		t.Errorf("无回复时正文格式错误：%q", body)
	}
}

// 超长回复要按字符截断（不能切断中文），并加省略号。
func TestBodyTruncatesLongMessage(t *testing.T) {
	long := strings.Repeat("中", 800)
	body := Event{Source: SourceClaude, Message: long}.Body(time.Now())

	if !strings.Contains(body, "…") {
		t.Error("超长正文未被截断")
	}
	// 截断必须落在字符边界上，不能产生乱码。
	if !strings.Contains(body, strings.Repeat("中", 100)) {
		t.Error("截断破坏了多字节字符")
	}
	if strings.Count(body, "中") > messagePreviewRunes {
		t.Errorf("截断后仍有 %d 个字符，超过上限 %d", strings.Count(body, "中"), messagePreviewRunes)
	}
}
