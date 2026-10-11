package message

import "testing"

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
