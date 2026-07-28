package main

import (
	"context"
	"fmt"

	selfupdate "github.com/windyskr/agent-completion-notification/internal/update"
)

func cmdUpdate(args []string) error {
	checkOnly := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--check":
		checkOnly = true
	default:
		return fmt.Errorf("用法：acn update [--check]")
	}

	result, err := selfupdate.Run(context.Background(), version, checkOnly)
	if err != nil {
		return err
	}
	if result.Updated {
		fmt.Printf("已更新：%s → %s\n", result.CurrentVersion, result.LatestVersion)
		fmt.Println("新版本位置：" + result.Executable)
		return nil
	}

	if result.CurrentVersion == "dev" {
		if checkOnly {
			fmt.Printf("当前为开发版；最新正式版为 %s\n", result.LatestVersion)
		} else {
			fmt.Printf("已安装最新正式版 %s\n", result.LatestVersion)
		}
		return nil
	}
	if result.CurrentIsNewer {
		fmt.Printf("当前版本 %s 高于最新正式版 %s，未降级\n", result.CurrentVersion, result.LatestVersion)
		return nil
	}
	fmt.Printf("已是最新版本 %s\n", result.LatestVersion)
	return nil
}
