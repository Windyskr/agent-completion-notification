//go:build !windows

package install

import "path/filepath"

func canonicalCodexConfigPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}
