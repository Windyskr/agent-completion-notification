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
	ev := Event{Source: SourceClaude, Cwd: "/work/acn"}
	if got, want := ev.Title(), "[Claude Code] acn 任务完成"; got != want {
		t.Errorf("Title = %q, 期望 %q", got, want)
	}
	// 无 cwd 时不应出现空项目名。
	if got := (Event{Source: SourceCodex}).Title(); got != "[Codex] 任务完成" {
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
	body := Event{Source: SourceClaude, Cwd: "/work/acn", DurationMS: 90_000}.Body(now)

	if !strings.Contains(body, "耗时：1m30s") {
		t.Errorf("正文缺少耗时:\n%s", body)
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

// 同一次完成的两条上报（换行/空白差异）必须命中同一指纹。
func TestFingerprintIgnoresWhitespace(t *testing.T) {
	a := Event{Source: SourceClaude, Cwd: "/work/acn", Message: "改好了\n\n收工"}
	b := Event{Source: SourceClaude, Cwd: "/work/acn", Message: "改好了 收工"}

	if a.Fingerprint() != b.Fingerprint() {
		t.Error("空白差异导致指纹不同，去重会失效")
	}
}

// 不同来源/目录/内容必须是不同指纹，否则会误压掉真实通知。
func TestFingerprintDistinguishes(t *testing.T) {
	base := Event{Source: SourceClaude, Cwd: "/work/acn", Message: "done"}
	others := []Event{
		{Source: SourceCodex, Cwd: "/work/acn", Message: "done"},
		{Source: SourceClaude, Cwd: "/work/other", Message: "done"},
		{Source: SourceClaude, Cwd: "/work/acn", Message: "别的回复"},
	}
	for _, o := range others {
		if base.Fingerprint() == o.Fingerprint() {
			t.Errorf("指纹与 %+v 相同，会误判为重复", o)
		}
	}
}
