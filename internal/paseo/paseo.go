// Package paseo 补充 Paseo 启动的 Agent 缺失的会话信息。
package paseo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const inspectTimeout = time.Second

type storedAgent struct {
	WorkspaceID string `json:"workspaceId"`
}

type storedWorkspace struct {
	ID    string `json:"workspaceId"`
	Title string `json:"title"`
}

// WorkspaceTitle 根据 Paseo Agent 的本地记录，读取它所属 Workspace 的标题。
func WorkspaceTitle(agentID string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", nil
	}
	home, err := paseoHome()
	if err != nil {
		return "", err
	}
	agentFiles, err := filepath.Glob(filepath.Join(home, "agents", "*", agentID+".json"))
	if err != nil {
		return "", fmt.Errorf("查找 Paseo Agent 记录失败: %w", err)
	}
	if len(agentFiles) == 0 {
		return "", fmt.Errorf("找不到 Paseo Agent %s 的本地记录", agentID)
	}

	var workspaceID string
	for _, path := range agentFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var agent storedAgent
		if json.Unmarshal(data, &agent) == nil && strings.TrimSpace(agent.WorkspaceID) != "" {
			workspaceID = strings.TrimSpace(agent.WorkspaceID)
			break
		}
	}
	if workspaceID == "" {
		return "", fmt.Errorf("Paseo Agent %s 没有关联 Workspace", agentID)
	}

	data, err := os.ReadFile(filepath.Join(home, "projects", "workspaces.json"))
	if err != nil {
		return "", fmt.Errorf("读取 Paseo Workspace 记录失败: %w", err)
	}
	var workspaces []storedWorkspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return "", fmt.Errorf("解析 Paseo Workspace 记录失败: %w", err)
	}
	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			return strings.TrimSpace(workspace.Title), nil
		}
	}
	return "", fmt.Errorf("找不到 Paseo Workspace %s", workspaceID)
}

func paseoHome() (string, error) {
	home := strings.TrimSpace(os.Getenv("PASEO_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("获取用户目录失败: %w", err)
		}
		home = filepath.Join(userHome, ".paseo")
	} else if home == "~" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("获取用户目录失败: %w", err)
		}
		home = userHome
	} else if strings.HasPrefix(home, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("获取用户目录失败: %w", err)
		}
		home = filepath.Join(userHome, home[2:])
	}
	resolved, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("解析 Paseo 数据目录失败: %w", err)
	}
	return resolved, nil
}

// AgentName 从 Paseo 的 Agent 记录读取用户可见的会话名称。
func AgentName(agentID string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", nil
	}

	cli := strings.TrimSpace(os.Getenv("PASEO_HOOK_CLI"))
	if cli == "" {
		cli = "paseo"
	}
	ctx, cancel := context.WithTimeout(context.Background(), inspectTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, cli, "inspect", agentID, "--json").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("读取 Paseo 会话名称超时")
		}
		return "", fmt.Errorf("读取 Paseo 会话名称失败: %w", err)
	}
	var result struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("解析 Paseo 会话名称失败: %w", err)
	}
	return strings.TrimSpace(result.Name), nil
}

// IsTitleGenerationMessage 判断是否为 Paseo 的临时标题生成 Agent 所输出的 JSON。
// 该 Agent 没有可查询的 Paseo Agent 记录，且回复固定为仅包含 title 的对象。
func IsTitleGenerationMessage(message string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(message)), &fields); err != nil || len(fields) != 1 {
		return false
	}
	rawTitle, ok := fields["title"]
	if !ok {
		return false
	}
	var title string
	return json.Unmarshal(rawTitle, &title) == nil && strings.TrimSpace(title) != ""
}
