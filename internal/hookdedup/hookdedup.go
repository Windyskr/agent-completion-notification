// Package hookdedup 防止同一会话的并发 Stop hook 重复投递。
package hookdedup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const staleAfter = 15 * time.Second

// Claim 为正在处理的事件创建临时占位文件。返回的 release 必须在处理结束时调用。
// 同一会话同时到达的第二个 hook 会返回 claimed=false。占位文件位于系统临时目录，
// 仅在当前 hook 处理期间存在。
func Claim(source, sessionID string) (claimed bool, release func(), err error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return true, func() {}, nil
	}
	path := filepath.Join(os.TempDir(), "acn-hook-"+safeName(source)+"-"+safeName(sessionID)+".lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if openErr == nil {
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(path)
				return false, nil, fmt.Errorf("关闭事件占位文件失败: %w", closeErr)
			}
			return true, func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(openErr) {
			return false, nil, fmt.Errorf("创建事件占位文件失败: %w", openErr)
		}
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) <= staleAfter || attempt == 1 {
			return false, func() {}, nil
		}
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return false, nil, fmt.Errorf("清理过期事件占位文件失败: %w", removeErr)
		}
	}
	return false, func() {}, nil
}

func safeName(value string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
}
