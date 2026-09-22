// Package paseo 补充 Paseo 启动的 Codex 进程缺失的会话信息。
package paseo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const inspectTimeout = time.Second

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
