// 命令 acn 在 AI CLI（Claude Code / Codex）完成任务时推送通知。
//
// 事件流：
//
//	Claude Code ──Stop hook──┐
//	                         ├─→ acn hook ─→ unix socket ─→ acn daemon ─→ 飞书
//	Codex ───────Stop hook───┘              （daemon 不可用时 hook 自行直发）
//
// 两者用的是同一套 Stop hook 契约，载荷都从 stdin 进来。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/windyskr/acn/internal/config"
	"github.com/windyskr/acn/internal/daemon"
	"github.com/windyskr/acn/internal/event"
	"github.com/windyskr/acn/internal/install"
	"github.com/windyskr/acn/internal/notify"
)

// version 由构建时通过 -ldflags "-X main.version=..." 注入。
var version = "dev"

const usage = `acn — AI CLI 任务完成通知

用法：
  acn install            接入 Claude Code 与 Codex（写入各自配置，自动备份）
  acn uninstall          摘除接入
  acn status             查看接入状态、配置与 daemon 运行情况
  acn config <k> <v>     修改配置项
  acn test               发送一条测试通知
  acn daemon             前台运行常驻服务（由 brew services 调用）
  acn hook claude        Claude Code 的 Stop hook 入口（读 stdin）
  acn hook codex         Codex 的 Stop hook 入口（读 stdin）
  acn version            打印版本

配置项（acn config）：
  webhook <url>          飞书自定义机器人地址
  secret <str>           飞书签名密钥（未开启签名校验则留空）
  min-duration <秒>      低于该耗时不推送，0 为不限
  claude <on|off>        是否推送 Claude Code
  codex <on|off>         是否推送 Codex

快速开始：
  acn config webhook https://open.feishu.cn/open-apis/bot/v2/hook/xxxx
  acn install
  brew services start acn
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "acn: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch args[0] {
	case "install":
		return cmdInstall()
	case "uninstall":
		return cmdUninstall()
	case "status":
		return cmdStatus()
	case "config":
		return cmdConfig(args[1:])
	case "test":
		return cmdTest()
	case "daemon":
		return cmdDaemon()
	case "hook":
		return cmdHook(args[1:])
	case "version", "--version", "-v":
		fmt.Println("acn " + version)
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("未知命令 %q，运行 acn help 查看用法", args[0])
	}
}

// cmdHook 处理来自 AI CLI 的 Stop 回调。
//
// 无论内部发生什么都返回 nil，且**绝不往 stdout 写任何东西**：
//   - 非零退出码会在用户终端里显示报错；
//   - Codex 的 Stop hook 若在 stdout 收到 {"decision":"block"}，会自动续跑一轮。
//
// 通知失败远不如打断工作流严重，因此一切诊断信息只走 stderr。
func cmdHook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：acn hook <claude|codex>")
	}

	ev, err := buildHookEvent(args[0], os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "acn hook: "+err.Error())
		return nil
	}
	deliver(ev)
	return nil
}

// deliver 优先交给 daemon 异步发送；daemon 未运行时当场同步发送。
//
// 走 daemon 的意义在于 Claude Code 的 Stop hook 会阻塞 CLI 返回：写完 socket
// 立即退出，用户不必等一次 HTTP 往返。
func deliver(ev event.Event) {
	if err := daemon.Send(ev); err == nil {
		return
	}
	sendDirect(ev)
}

// sendDirect 在当前进程内完成投递。
func sendDirect(ev event.Event) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acn: 加载配置失败: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notify.SendTimeout)
	defer cancel()

	for _, r := range notify.Dispatch(ctx, cfg, notify.Build(cfg), ev) {
		if r.Err != nil {
			fmt.Fprintln(os.Stderr, "acn: "+r.String())
		}
	}
}

// cmdDaemon 前台运行服务，收到 SIGINT/SIGTERM 后优雅退出
// （launchd 与 brew services 都靠信号停止进程）。
func cmdDaemon() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return daemon.Run(ctx)
}

func cmdInstall() error {
	exe, err := install.Executable()
	if err != nil {
		return fmt.Errorf("获取自身路径失败: %w", err)
	}

	st, installErr := install.Install(exe)
	printTarget(st.Claude)
	printTarget(st.Codex)
	if installErr != nil {
		return installErr
	}

	cfg, _ := config.Load()
	fmt.Println()
	if !cfg.FeishuReady() {
		fmt.Println("下一步：acn config webhook <飞书机器人地址>")
	}
	fmt.Println("下一步：brew services start acn（不启动也能用，只是每次通知会稍慢）")
	if st.Codex.Installed {
		// Codex 要求用户逐条审阅并信任非托管的命令 hook，否则它不会执行。
		fmt.Println("下一步：在 Codex 里执行 /hooks，信任 acn 的 Stop hook")
	}
	fmt.Println("提示：Claude Code 与 Codex 需重启后生效")
	return nil
}

func cmdUninstall() error {
	st, err := install.Uninstall()
	printTarget(st.Claude)
	printTarget(st.Codex)
	return err
}

func cmdStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fmt.Println("接入状态：")
	st := install.Query()
	printTarget(st.Claude)
	printTarget(st.Codex)

	fmt.Println("\n配置（" + config.Path() + "）：")
	fmt.Println("  " + mark(cfg.FeishuReady()) + " 飞书 webhook：" + describeWebhook(cfg))
	if cfg.Feishu.Secret != "" {
		fmt.Println("  · 签名校验：已启用")
	}
	if cfg.MinDurationSeconds > 0 {
		fmt.Printf("  · 耗时阈值：%ds\n", cfg.MinDurationSeconds)
	}
	fmt.Printf("  · 来源开关：claude=%s codex=%s\n",
		onOff(cfg.SourceEnabled(event.SourceClaude)), onOff(cfg.SourceEnabled(event.SourceCodex)))

	running := daemon.Running()
	fmt.Println("\n服务：")
	fmt.Println("  " + mark(running) + " daemon " + daemonState(running))
	if !running {
		fmt.Println("    （未运行时 hook 会自行发送，功能不受影响）")
	}
	return nil
}

func cmdTest() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.FeishuReady() {
		return fmt.Errorf("尚未配置飞书 webhook，先运行：acn config webhook <地址>")
	}

	cwd, _ := os.Getwd()
	ev := event.Event{
		Source:     event.SourceClaude,
		Cwd:        cwd,
		Message:    "这是一条来自 acn 的测试通知。收到即表示配置正确。",
		DurationMS: 42_000,
	}

	ctx, cancel := context.WithTimeout(context.Background(), notify.SendTimeout)
	defer cancel()

	// 测试直发，不经 daemon——要验证的是渠道配置本身。
	var failed bool
	for _, r := range notify.Dispatch(ctx, cfg, notify.Build(cfg), ev) {
		fmt.Println("  " + r.String())
		failed = failed || r.Err != nil
	}
	if failed {
		return fmt.Errorf("测试通知发送失败")
	}
	return nil
}

func cmdConfig(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(args) < 2 {
		return fmt.Errorf("用法：acn config <配置项> <值>，运行 acn help 查看全部配置项")
	}

	key, value := args[0], strings.TrimSpace(args[1])
	switch key {
	case "webhook":
		cfg.Feishu.WebhookURL = value
	case "secret":
		cfg.Feishu.Secret = value
	case "min-duration":
		n, err := parseSeconds(value)
		if err != nil {
			return err
		}
		cfg.MinDurationSeconds = n
	case "claude", "codex":
		on, err := parseBool(value)
		if err != nil {
			return err
		}
		if cfg.Sources == nil {
			cfg.Sources = map[string]bool{}
		}
		cfg.Sources[key] = on
	default:
		return fmt.Errorf("未知配置项 %q，运行 acn help 查看全部配置项", key)
	}

	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("已更新 %s\n", key)

	// 环境变量优先级高于配置文件，静默失效会很难排查。
	if (key == "webhook" && os.Getenv(config.EnvWebhook) != "") ||
		(key == "secret" && os.Getenv(config.EnvSecret) != "") {
		fmt.Println("注意：同名环境变量已设置，其值会覆盖此处配置")
	}
	return nil
}

func printTarget(t install.TargetStatus) {
	fmt.Printf("  %s %s：%s\n", mark(t.Installed), t.Name, t.Detail)
	if t.Warning != "" {
		fmt.Printf("    ⚠ %s\n", t.Warning)
	}
}

func describeWebhook(cfg config.Config) string {
	if !cfg.FeishuReady() {
		return "未配置"
	}
	return maskURL(cfg.Feishu.WebhookURL)
}

// maskURL 隐藏 webhook 末段的 token——status 的输出常被贴进聊天记录。
func maskURL(u string) string {
	idx := strings.LastIndex(u, "/")
	if idx < 0 || idx == len(u)-1 {
		return u
	}
	token := u[idx+1:]
	if len(token) <= 8 {
		return u[:idx+1] + "****"
	}
	return u[:idx+1] + token[:4] + "…" + token[len(token)-4:]
}

func parseSeconds(v string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 {
		return 0, fmt.Errorf("min-duration 需为非负整数秒，收到 %q", v)
	}
	return n, nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("需为 on 或 off，收到 %q", v)
	}
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "×"
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func daemonState(running bool) string {
	if running {
		return "运行中（" + config.SocketPath() + "）"
	}
	return "未运行"
}
