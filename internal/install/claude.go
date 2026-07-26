package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeHookEvent 是我们挂载的事件名。Claude Code 在一轮对话结束时触发它。
const claudeHookEvent = "Stop"

// claudeHookTimeout 是 hook 的执行上限（秒）。hook 只写一次 socket 就退出，
// 给 10 秒纯属兜底——真被卡住时 Claude Code 会自行放弃，不至于挂死 CLI。
const claudeHookTimeout = 10

// hookEntry 是一条具体的 hook 命令。
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// hookGroup 是 settings.json 中 hooks.<Event> 数组的一项。
type hookGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

// claudeSettingsPath 返回 Claude Code 的用户级配置路径。
func claudeSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// installClaude 幂等地把 acn 挂到 Stop 事件上。
func installClaude(exe string) error {
	return editClaudeSettings(func(groups []json.RawMessage) ([]json.RawMessage, error) {
		// 先摘掉旧的 acn 条目再追加，重复安装才不会堆积。
		kept, _ := stripACNGroups(groups)
		entry := hookGroup{
			Hooks: []hookEntry{{
				Type:    "command",
				Command: shellQuote(exe) + " hook claude",
				Timeout: claudeHookTimeout,
			}},
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		return append(kept, raw), nil
	})
}

// uninstallClaude 摘除 acn 的 Stop hook，保留用户其余 hook。
func uninstallClaude() error {
	return editClaudeSettings(func(groups []json.RawMessage) ([]json.RawMessage, error) {
		kept, _ := stripACNGroups(groups)
		return kept, nil
	})
}

// queryClaude 探测安装状态。
func queryClaude() TargetStatus {
	st := TargetStatus{Name: "Claude Code", Path: claudeSettingsPath()}

	groups, err := readClaudeGroups(st.Path)
	if err != nil {
		if os.IsNotExist(err) {
			st.Detail = "未安装（配置文件不存在）"
		} else {
			st.Detail = "读取失败: " + err.Error()
		}
		return st
	}
	if _, found := stripACNGroups(groups); found {
		st.Installed = true
		st.Detail = claudeHookEvent + " hook 已安装"
	} else {
		st.Detail = "未安装"
	}
	return st
}

// editClaudeSettings 读取 settings.json，交由 mutate 改写 Stop 分组后写回。
//
// 除 hooks.Stop 之外的所有内容都以 json.RawMessage 原样保留：既不会丢掉未知字段，
// 也不会改动用户其它 hook 的写法。
func editClaudeSettings(mutate func([]json.RawMessage) ([]json.RawMessage, error)) error {
	path := claudeSettingsPath()

	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	hooks, err := readNestedObject(root, "hooks")
	if err != nil {
		return err
	}
	groups, err := readGroups(hooks)
	if err != nil {
		return err
	}

	next, err := mutate(groups)
	if err != nil {
		return err
	}

	// 空数组和空对象都清理掉，卸载后应当恢复成安装前的样子。
	if len(next) == 0 {
		delete(hooks, claudeHookEvent)
	} else {
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		hooks[claudeHookEvent] = raw
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		raw, err := json.Marshal(hooks)
		if err != nil {
			return err
		}
		root["hooks"] = raw
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := backup(path); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	return writeFileAtomicPreservingMode(path, append(data, '\n'), 0o644)
}

// readClaudeGroups 只读地取出 Stop 分组。
func readClaudeGroups(path string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	hooks, err := readNestedObject(root, "hooks")
	if err != nil {
		return nil, err
	}
	return readGroups(hooks)
}

// stripACNGroups 剔除所有属于 acn 的 hook 条目，返回保留下来的分组以及
// 「原本是否装过 acn」。只含 acn 条目的分组会被整组丢弃；混合分组则重写，
// 仅去掉 acn 那一条。未被触及的分组保持原始字节，不做任何格式化。
func stripACNGroups(groups []json.RawMessage) ([]json.RawMessage, bool) {
	kept := make([]json.RawMessage, 0, len(groups))
	found := false

	for _, raw := range groups {
		var g hookGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			kept = append(kept, raw) // 看不懂的分组一律原样保留
			continue
		}
		remaining := make([]hookEntry, 0, len(g.Hooks))
		hit := false
		for _, h := range g.Hooks {
			if isACNCommand(h.Command) {
				hit = true
				continue
			}
			remaining = append(remaining, h)
		}
		if !hit {
			kept = append(kept, raw)
			continue
		}
		found = true
		if len(remaining) == 0 {
			continue
		}
		g.Hooks = remaining
		if rewritten, err := json.Marshal(g); err == nil {
			kept = append(kept, rewritten)
		}
	}
	return kept, found
}

// isACNCommand 判断一条 hook 命令是否由 acn 安装。二进制固定名为 acn，
// 因此命令串必然同时含有可执行名与子命令。
func isACNCommand(cmd string) bool {
	return strings.Contains(cmd, "acn") && strings.Contains(cmd, "hook claude")
}

// readJSONObject 读取 JSON 对象；文件不存在时返回空对象，让首次安装能直接创建。
func readJSONObject(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("解析 %s 失败（请先修复该文件）: %w", path, err)
	}
	return root, nil
}

// readNestedObject 取出对象型字段，缺失时返回空对象。
func readNestedObject(root map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	raw, ok := root[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("字段 %q 不是对象: %w", key, err)
	}
	return out, nil
}

// readGroups 取出 hooks.Stop 数组，缺失时返回空切片。
func readGroups(hooks map[string]json.RawMessage) ([]json.RawMessage, error) {
	raw, ok := hooks[claudeHookEvent]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("字段 hooks.%s 不是数组: %w", claudeHookEvent, err)
	}
	return groups, nil
}
