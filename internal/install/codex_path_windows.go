//go:build windows

package install

import (
	"path/filepath"
	"syscall"
)

// canonicalCodexConfigPath 展开 Windows 8.3 短路径。Codex 会用长路径生成
// hook state key；若这里保留 ADMINI~1 一类短名称，trusted_hash 将无法命中。
func canonicalCodexConfigPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)

	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buffer := make([]uint16, 32768)
	n, err := syscall.GetLongPathName(ptr, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || int(n) >= len(buffer) {
		return path
	}
	return syscall.UTF16ToString(buffer[:n])
}
