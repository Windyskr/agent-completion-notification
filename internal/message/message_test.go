package message

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestBody(t *testing.T) {
	msg := Message{
		Title:   "修复登录问题",
		Reply:   "改好了\n共两处",
		Details: []string{"完成时间：2026-07-26 15:04:05", "目录：/work/acn"},
	}
	cases := []struct {
		name   string
		msg    Message
		format Format
		want   string
	}{
		{"纯文本", msg, Format{}, "改好了\n共两处\n\n完成时间：2026-07-26 15:04:05\n目录：/work/acn"},
		{"带标题头", msg, Format{Header: "### 修复登录问题\n"}, "### 修复登录问题\n改好了\n共两处\n\n完成时间：2026-07-26 15:04:05\n目录：/work/acn"},
		// 段落之间的空行保留，段落内的单换行改成 Markdown 强制换行。
		{"强制换行", msg, Format{HardLineBreaks: true}, "改好了  \n共两处\n\n完成时间：2026-07-26 15:04:05  \n目录：/work/acn"},
		{"无回复", Message{Details: msg.Details}, Format{}, "完成时间：2026-07-26 15:04:05\n目录：/work/acn"},
		{"无详情", Message{Reply: "改好了"}, Format{}, "改好了"},
	}
	for _, c := range cases {
		if got := c.msg.Body(c.format); got != c.want {
			t.Errorf("%s:\n得到：%q\n期望：%q", c.name, got, c.want)
		}
	}
}

// 已经带有两个尾随空格的行不应重复追加。
func TestBodyHardLineBreaksIsIdempotent(t *testing.T) {
	msg := Message{Reply: "第一行  \n第二行"}
	if got, want := msg.Body(Format{HardLineBreaks: true}), "第一行  \n第二行"; got != want {
		t.Errorf("得到 %q, 期望 %q", got, want)
	}
}

// 超出渠道上限时只截断回复原文，标题头与详情行完整保留。
func TestBodyFitsChannelLimit(t *testing.T) {
	msg := Message{
		Reply:   strings.Repeat("中", 3000),
		Details: []string{"完成时间：2026-07-26 15:04:05", "目录：/work/acn"},
	}
	f := Format{Header: "### 标题\n", MaxBytes: 4096}
	body := msg.Body(f)

	if len(body) > 4096 {
		t.Fatalf("正文 %d 字节，超过上限 4096", len(body))
	}
	if !utf8.ValidString(body) {
		t.Error("截断切坏了多字节字符")
	}
	if !strings.HasPrefix(body, "### 标题\n中") || !strings.HasSuffix(body, "…\n\n完成时间：2026-07-26 15:04:05\n目录：/work/acn") {
		t.Errorf("截断后应保留标题头与详情行:\n%s", body)
	}
	if 4096-len(body) >= len("中") {
		t.Errorf("截断过多：仍剩 %d 字节可用", 4096-len(body))
	}
}

// Telegram 按 UTF-16 计数，emoji 等增补平面字符占两个码元。
func TestBodyFitsUTF16Limit(t *testing.T) {
	msg := Message{Reply: strings.Repeat("🎉", 100), Details: []string{"目录：/w"}}
	body := msg.Body(Format{MaxUTF16: 50})

	if n := len(utf16.Encode([]rune(body))); n > 50 {
		t.Errorf("正文 %d 个 UTF-16 码元，超过上限 50: %q", n, body)
	}
	if !strings.HasSuffix(body, "…\n\n目录：/w") {
		t.Errorf("详情行应保留: %q", body)
	}
}

func TestBodyWithinLimitIsUnchanged(t *testing.T) {
	msg := Message{Reply: "改好了", Details: []string{"目录：/w"}}
	if got, want := msg.Body(Format{MaxBytes: 4096, MaxUTF16: 4096}), msg.Body(Format{}); got != want {
		t.Errorf("未超限的正文被改动: %q, 期望 %q", got, want)
	}
}

// 详情本身就超限时退化为硬截断，但结果仍不能超过上限。
func TestBodyHardCutsWhenDetailsExceedLimit(t *testing.T) {
	msg := Message{Reply: "改好了", Details: []string{"目录：/" + strings.Repeat("a", 100)}}
	if body := msg.Body(Format{MaxBytes: 20}); len(body) > 20 || !utf8.ValidString(body) {
		t.Errorf("硬截断结果 = %q（%d 字节）", body, len(body))
	}
}
