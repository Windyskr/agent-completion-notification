package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizeDirectoryPath 将配置和事件中的目录统一为绝对、清理过的路径。
// 不要求目录当前存在，便于提前配置尚未创建的项目目录。
func NormalizeDirectoryPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("目录路径不能为空")
	}
	path = expandHome(path)
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析目录路径 %q 失败: %w", path, err)
	}
	return filepath.Clean(abs), nil
}

func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
}

// pathWithinDirectory 判断 candidate 是否为 root 本身或其子目录。
// filepath.Rel 可以避免 /work/app 错误匹配 /work/application。
func pathWithinDirectory(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
