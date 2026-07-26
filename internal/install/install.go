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

	"github.com/windyskr/acn/internal/config"
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
}

// Status 汇总全部集成点。
type Status struct {
	Claude TargetStatus
	Codex  TargetStatus
}

// Install 安装两处集成。任一失败都返回错误，但不回滚另一处已成功的改动
// （二者相互独立，且都可用 acn uninstall 单独摘除）。
func Install(exe string) (Status, error) {
	var st Status
	var errs []string

	if err := installClaude(exe); err != nil {
		errs = append(errs, "Claude Code: "+err.Error())
	}
	if err := installCodex(exe); err != nil {
		errs = append(errs, "Codex: "+err.Error())
	}

	st = Query()
	if len(errs) > 0 {
		return st, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return st, nil
}

// Uninstall 摘除两处集成，并还原 Codex 原有的 notify 程序。
func Uninstall() (Status, error) {
	var errs []string

	if err := uninstallClaude(); err != nil {
		errs = append(errs, "Claude Code: "+err.Error())
	}
	if err := uninstallCodex(); err != nil {
		errs = append(errs, "Codex: "+err.Error())
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

// shellQuote 以单引号包裹路径，供写入 shell 命令串（Claude 的 hook 是 shell 命令）。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
