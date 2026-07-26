// Package install 负责把 acn 挂到 Claude Code 与 Codex 的原生回调上，
// 以及安全地摘除。
//
// 两处配置都属于用户既有文件，改写遵循三条原则：
//  1. 写入前备份；
//  2. 只增删 acn 自己的条目，其余内容原样保留；
//  3. 幂等——重复安装不会产生重复条目。
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/windyskr/agent-completion-notification/internal/config"
)

// TargetStatus 描述单个集成点的安装状态。
type TargetStatus struct {
	Name      string
	Path      string
	Installed bool
	Detail    string
	// Warning 非空表示：配置写进去了，但对方运行时不会真的触发。
	// 这类失败是无声的，必须显式说出来。
	Warning string
	// Exe 是对方配置里实际记录的 acn 路径。二进制换了位置（重装到别处、
	// 删掉软链）之后它会指向不存在的文件，hook 随之静默失效。
	Exe string
}

// Status 汇总全部集成点。
type Status struct {
	Claude TargetStatus
	Codex  TargetStatus
}

// 可选的集成点名称。与 acn hook 的来源名、配置里的来源开关保持一致。
const (
	TargetClaude = "claude"
	TargetCodex  = "codex"
)

// Targets 返回全部集成点名称。
func Targets() []string { return []string{TargetClaude, TargetCodex} }

// ValidateTargets 校验用户给的目标名；空切片视为全部。
func ValidateTargets(targets []string) error {
	for _, t := range targets {
		if t != TargetClaude && t != TargetCodex {
			return fmt.Errorf("未知目标 %q，可选：%s", t, strings.Join(Targets(), " / "))
		}
	}
	return nil
}

// selected 判断某集成点是否在本次操作范围内。空切片表示全部。
func selected(targets []string, name string) bool {
	if len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		if t == name {
			return true
		}
	}
	return false
}

// Install 安装指定集成点，targets 为空表示全部。任一失败都返回错误，但不回滚
// 另一处已成功的改动——二者相互独立，且都能单独摘除。
func Install(exe string, targets ...string) (Status, error) {
	var errs []string

	if selected(targets, TargetClaude) {
		if err := installClaude(exe); err != nil {
			errs = append(errs, "Claude Code: "+err.Error())
		}
	}
	if selected(targets, TargetCodex) {
		if err := installCodex(exe); err != nil {
			errs = append(errs, "Codex: "+err.Error())
		}
	}

	st := Query()
	if len(errs) > 0 {
		return st, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return st, nil
}

// Uninstall 摘除指定集成点，targets 为空表示全部。
func Uninstall(targets ...string) (Status, error) {
	var errs []string

	if selected(targets, TargetClaude) {
		if err := uninstallClaude(); err != nil {
			errs = append(errs, "Claude Code: "+err.Error())
		}
	}
	if selected(targets, TargetCodex) {
		if err := uninstallCodex(); err != nil {
			errs = append(errs, "Codex: "+err.Error())
		}
	}

	st := Query()
	if len(errs) > 0 {
		return st, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return st, nil
}

// Query 探测当前安装状态，不做任何修改。
func Query() Status {
	return Status{
		Claude: queryClaude(),
		Codex:  queryCodex(),
	}
}

// Executable 返回用于写入配置的 acn 绝对路径，并解析符号链接
// （Homebrew 的 bin 目录是软链，写入真实路径更稳）。
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// backup 在改写前留一份副本，文件不存在时视为无需备份。
func backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(path+".acn.bak", data, 0o600)
}

// shellQuote 以单引号包裹路径，供写入 shell 命令串（两边的 hook 都是 shell 命令）。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hookCommand 渲染写入对方配置的命令串。
func hookCommand(exe, source string) string {
	return shellQuote(exe) + " hook " + source
}

// extractExe 从命令串里还原出 acn 路径，即 hookCommand 的逆操作。
// 认不出来时返回空串。
func extractExe(command, source string) string {
	quoted, ok := strings.CutSuffix(command, " hook "+source)
	if !ok {
		return ""
	}
	if len(quoted) >= 2 && quoted[0] == '\'' && quoted[len(quoted)-1] == '\'' {
		return strings.ReplaceAll(quoted[1:len(quoted)-1], `'\''`, "'")
	}
	return quoted
}

// writeFileAtomicPreservingMode 原子写回用户既有文件，并沿用它原本的权限位。
// 这些文件（settings.json / config.toml）含密钥，不能因为我们改写就放宽权限。
func writeFileAtomicPreservingMode(path string, data []byte, fallback os.FileMode) error {
	perm := fallback
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return config.WriteFileAtomic(path, data, perm)
}

// loadConfig 读取 acn 配置，失败时退回默认值，避免安装流程被配置问题卡住。
func loadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		return config.Default()
	}
	return cfg
}
