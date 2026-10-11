package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
	"github.com/windyskr/agent-completion-notification/internal/install"
	"github.com/windyskr/agent-completion-notification/internal/notify"
)

// check 是一条检查结果。
type check struct {
	name   string
	level  level
	detail string
	// hint 是可执行的补救动作，仅在非 ok 时展示。
	hint string
}

type level int

const (
	levelOK level = iota
	levelUnknown
	levelWarn
	levelFail
)

func (l level) mark() string {
	switch l {
	case levelOK:
		return "✓"
	case levelUnknown:
		return "?"
	case levelWarn:
		return "⚠"
	default:
		return "✗"
	}
}

// cmdDoctor 主动验证整条链路，而不只是回显配置。
//
// 与 status 的区别：status 是快照，doctor 会去核对路径是否仍然有效、
// 并真的推一条通知走完整链路。
func cmdDoctor() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st := install.Query()

	checks := []check{
		checkExecutable(),
		checkTarget(st.Claude),
		checkTarget(st.Codex),
		checkTarget(st.OpenCode),
		checkCodexTrust(st.Codex),
	}
	for _, channel := range notify.Channels {
		checks = append(checks, checkChannel(cfg, channel))
	}
	checks = append(checks, checkDelivery(cfg))

	var failed int
	for _, c := range checks {
		fmt.Printf("  %s %s %s\n", c.level.mark(), padDisplay(c.name, 14), c.detail)
		if c.level != levelOK && c.hint != "" {
			fmt.Printf("      → %s\n", c.hint)
		}
		if c.level == levelFail {
			failed++
		}
	}

	fmt.Println()
	if failed > 0 {
		return fmt.Errorf("%d 项检查未通过", failed)
	}
	fmt.Println("一切正常。")
	return nil
}

// padDisplay 按终端列宽右填充。不能用 %-14s：中日韩字符占两列却只算一个
// rune，混排时会对不齐。
func padDisplay(s string, width int) string {
	if n := displayWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// displayWidth 估算终端列宽，落在全角区间的字符按两列计。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && r <= 0x115F, // 韩文字母
			r >= 0x2E80 && r <= 0x303E, // 部首、中日韩符号
			r >= 0x3041 && r <= 0x33FF, // 假名、兼容字符
			r >= 0x3400 && r <= 0x4DBF, // 扩展 A
			r >= 0x4E00 && r <= 0x9FFF, // 基本汉字
			r >= 0xAC00 && r <= 0xD7A3, // 韩文音节
			r >= 0xF900 && r <= 0xFAFF, // 兼容表意
			r >= 0xFF00 && r <= 0xFF60, // 全角形式
			r >= 0xFFE0 && r <= 0xFFE6:
			w += 2
		default:
			w++
		}
	}
	return w
}

// checkExecutable 确认当前二进制自身可定位。
func checkExecutable() check {
	exe, err := install.Executable()
	if err != nil {
		return check{"二进制", levelFail, "无法定位自身路径: " + err.Error(), ""}
	}
	return check{"二进制", levelOK, exe, ""}
}

// checkTarget 核对某个集成点：是否装了、记录的路径是否还有效。
//
// 路径失效是最隐蔽的故障：二进制换了位置之后配置里仍指向旧路径，
// CLI 静默地什么都不做。
func checkTarget(t install.TargetStatus) check {
	c := check{name: t.Name}

	if !t.Installed {
		c.level = levelWarn
		c.detail = "未安装"
		c.hint = "acn install"
		return c
	}
	if t.Warning != "" {
		c.level = levelWarn
		c.detail = t.Warning
		return c
	}
	if t.Exe == "" {
		c.level = levelWarn
		c.detail = "已安装，但无法解析其中记录的 acn 路径"
		c.hint = "acn install（重写为当前路径）"
		return c
	}
	info, err := os.Stat(t.Exe)
	if err != nil {
		c.level = levelFail
		c.detail = "记录的 acn 路径已失效：" + t.Exe
		c.hint = "acn install（重写为当前路径）"
		return c
	}
	if info.IsDir() {
		c.level = levelFail
		c.detail = "记录的 acn 路径是目录而非可执行文件：" + t.Exe
		c.hint = "acn install（重写为当前路径）"
		return c
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		c.level = levelFail
		c.detail = "记录的 acn 没有执行权限：" + t.Exe
		c.hint = "chmod +x " + t.Exe
		return c
	}

	c.level = levelOK
	c.detail = "hook 已安装 → " + t.Exe
	if cur, err := install.Executable(); err == nil && cur != t.Exe {
		curInfo, statErr := os.Stat(cur)
		if statErr != nil || !os.SameFile(info, curInfo) {
			// 不算错：两个路径可能都能跑。但版本可能不一致，值得说一声。
			c.level = levelWarn
			c.detail = fmt.Sprintf("hook 指向 %s，与当前运行的 %s 不同", t.Exe, cur)
			c.hint = "acn install（统一为当前路径）"
		}
	}
	return c
}

// checkCodexTrust 核对安装器生成的 trusted_hash 是否仍与当前 hook 定义一致。
func checkCodexTrust(t install.TargetStatus) check {
	if !t.Installed {
		return check{"Codex 信任", levelUnknown, "跳过（Codex 未安装）", ""}
	}
	if !t.TrustKnown {
		return check{"Codex 信任", levelUnknown, "无法确认", "acn install codex"}
	}
	if !t.Trusted {
		return check{
			"Codex 信任", levelWarn,
			"信任哈希缺失或与当前 hook 不匹配",
			"acn install codex（重新生成信任状态）",
		}
	}
	return check{"Codex 信任", levelOK, "已信任当前 ACN hook", ""}
}

// checkChannel 核对单个渠道的开关与配置。渠道都是可选的，未配置不算失败。
func checkChannel(cfg config.Config, channel notify.Channel) check {
	switch {
	case !cfg.ChannelEnabled(channel.Name):
		return check{channel.Label, levelUnknown, "已禁用", ""}
	case !channel.Ready(cfg):
		return check{channel.Label, levelUnknown, channel.Describe(cfg) + "（可选）", ""}
	}
	return check{channel.Label, levelOK, channel.Describe(cfg), ""}
}

// checkDelivery 真的推一条，走与 hook 相同的渠道与文案；但跳过推送规则，
// 否则 skip-untitled、来源开关等合法配置会让自检永远无法验证渠道。
func checkDelivery(cfg config.Config) check {
	if !notify.DeliveryReady(cfg) {
		return check{
			"实际推送", levelFail, "跳过（未配置已启用的通知渠道）",
			"配置至少一个通知渠道",
		}
	}

	cwd, _ := os.Getwd()
	ev := event.Event{
		Source:     event.SourceClaude,
		Cwd:        cwd,
		Message:    "acn doctor 自检通知。收到即表示整条链路可用。",
		DurationMS: 42_000,
	}

	report, err := notify.Deliver(context.Background(), cfg, ev)
	switch {
	case err != nil:
		return check{"实际推送", levelFail, err.Error(), "检查通知渠道地址与网络"}
	case report.SkippedReason != "":
		return check{"实际推送", levelWarn, "未推送（" + report.SkippedReason + "）", ""}
	case len(report.Warnings) > 0:
		return check{
			"实际推送", levelWarn, "已发出，但" + strings.Join(report.Warnings, "；"),
			"acn config notification-timezone <IANA 时区|local>",
		}
	}
	return check{"实际推送", levelOK, "已发出，请确认已配置渠道是否收到", ""}
}
