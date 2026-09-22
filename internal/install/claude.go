package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeHookEvent 是任务完成事件；其余事件用于等待介入与 API 错误终止提醒。
// 它们都挂在用户级 settings.json 的 hooks 下。
const claudeHookEvent = "Stop"

const (
	claudeEventPreToolUse   = "PreToolUse"
	claudeEventNotification = "Notification"
	claudeEventStopFailure  = "StopFailure"
)

// askUserMatcher 让 PreToolUse 只在 Claude 发起选择（AskUserQuestion）时触发，
// 不拦截其它工具调用。
const askUserMatcher = "AskUserQuestion"

// claudeHookTimeout 是 hook 的执行上限（秒）。hook 只写一次 socket 就退出，
// 给 10 秒纯属兜底——真被卡住时 Claude Code 会自行放弃，不至于挂死 CLI。
const claudeHookTimeout = 10

// claudeEvents 是 acn 管理的全部事件。
var claudeEvents = []string{claudeHookEvent, claudeEventPreToolUse, claudeEventNotification, claudeEventStopFailure}

// claudeEventLabel 返回事件在状态输出里的短标签。
func claudeEventLabel(eventName string) string {
	switch eventName {
	case claudeHookEvent:
		return "完成"
	case claudeEventPreToolUse:
		return "选择"
	case claudeEventNotification:
		return "通知"
	case claudeEventStopFailure:
		return "异常"
	}
	return eventName
}

// claudeHookSource 返回事件对应的 acn hook 子命令名。
func claudeHookSource(eventName string) string {
	switch eventName {
	case claudeHookEvent:
		return "claude"
	case claudeEventPreToolUse:
		return "claude-question"
	case claudeEventNotification:
		return "claude-notification"
	case claudeEventStopFailure:
		return "claude-failure"
	}
	return "claude"
}

// hookEntry 是一条具体的 hook 命令。
type hookEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Timeout int      `json:"timeout,omitempty"`
	// Async 为 true 时后台执行、不阻塞回合。等待介入与异常终止事件均异步，
	// Stop 沿用同步语义，保持既有行为不变。
	Async bool `json:"async,omitempty"`
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

// installClaude 幂等地把 acn 挂到完成、等待介入与异常终止事件上。
func installClaude(exe string) error {
	return editClaudeSettings(claudeEvents, func(eventName string, groups []json.RawMessage) ([]json.RawMessage, error) {
		// 先摘掉旧的 acn 条目再追加，重复安装才不会堆积。
		kept, _ := stripACNGroups(groups)
		entry := hookGroup{Hooks: []hookEntry{claudeHookEntry(exe, eventName)}}
		if eventName == claudeEventPreToolUse {
			entry.Matcher = askUserMatcher
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		return append(kept, raw), nil
	})
}

// claudeHookEntry 渲染一条 acn hook 命令条目。
func claudeHookEntry(exe, eventName string) hookEntry {
	hook := hookEntry{Type: "command", Timeout: claudeHookTimeout}
	if eventName != claudeHookEvent {
		hook.Async = true
	}
	source := claudeHookSource(eventName)
	if isWindowsPath(exe) {
		// Claude Code 的 exec form 会直接 CreateProcess，不经过 Git Bash 或
		// PowerShell，因此路径含空格时也不需要任何 shell 引号。
		hook.Command = exe
		hook.Args = []string{"hook", source}
	} else {
		hook.Command = hookCommand(exe, source)
	}
	return hook
}

// uninstallClaude 摘除 acn 在全部事件上的条目，保留用户其余 hook。
func uninstallClaude() error {
	return editClaudeSettings(claudeEvents, func(_ string, groups []json.RawMessage) ([]json.RawMessage, error) {
		kept, _ := stripACNGroups(groups)
		return kept, nil
	})
}

// queryClaude 探测安装状态。三类事件独立挂载，detail 里说明各自是否在位。
func queryClaude() TargetStatus {
	st := TargetStatus{Name: "Claude Code", Path: claudeSettingsPath()}

	hooks, err := readClaudeHooks(st.Path)
	if err != nil {
		if os.IsNotExist(err) {
			st.Detail = "未安装（配置文件不存在）"
		} else {
			st.Detail = "读取失败: " + err.Error()
		}
		return st
	}

	var labels []string
	exe := ""
	for _, eventName := range claudeEvents {
		groups, err := readGroups(hooks, eventName)
		if err != nil {
			continue // 单个字段类型异常按未安装处理，不打断整体探测
		}
		if _, found := stripACNGroups(groups); !found {
			continue
		}
		labels = append(labels, claudeEventLabel(eventName))
		if exe == "" {
			exe = recordedExe(groups)
		}
	}
	if len(labels) == 0 {
		st.Detail = "未安装"
		return st
	}
	st.Installed = true
	st.Detail = strings.Join(labels, "/") + " hook 已安装"
	st.Exe = exe
	return st
}

// recordedExe 取出已安装条目里记录的 acn 路径。
func recordedExe(groups []json.RawMessage) string {
	for _, raw := range groups {
		var g hookGroup
		if err := json.Unmarshal(raw, &g); err != nil {
			continue
		}
		for _, h := range g.Hooks {
			if isACNHook(h) {
				if isACNExecForm(h) {
					return h.Command
				}
				// 三类事件的命令串后缀不同，逐个尝试还原路径。
				for _, eventName := range claudeEvents {
					if exe := extractExe(h.Command, claudeHookSource(eventName)); exe != "" {
						return exe
					}
				}
			}
		}
	}
	return ""
}

// editClaudeSettings 读取 settings.json，把 events 列出的每个事件数组交由
// mutate 改写后写回。
//
// 除这些事件之外的所有内容都以 json.RawMessage 原样保留：既不会丢掉未知字段，
// 也不会改动用户其它 hook 的写法。
func editClaudeSettings(events []string, mutate func(eventName string, groups []json.RawMessage) ([]json.RawMessage, error)) error {
	path := claudeSettingsPath()

	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	hooks, err := readNestedObject(root, "hooks")
	if err != nil {
		return err
	}

	for _, eventName := range events {
		groups, err := readGroups(hooks, eventName)
		if err != nil {
			return err
		}
		next, err := mutate(eventName, groups)
		if err != nil {
			return err
		}
		// 空数组和空对象都清理掉，卸载后应当恢复成安装前的样子。
		if len(next) == 0 {
			delete(hooks, eventName)
		} else {
			raw, err := json.Marshal(next)
			if err != nil {
				return err
			}
			hooks[eventName] = raw
		}
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

// readClaudeHooks 只读地取出整个 hooks 对象；文件不存在时错误上抛，
// 让调用方能区分「没装过」和「装了但读坏了」。
func readClaudeHooks(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return readNestedObject(root, "hooks")
}

// readClaudeGroups 只读地取出 Stop 分组。
func readClaudeGroups(path string) ([]json.RawMessage, error) {
	hooks, err := readClaudeHooks(path)
	if err != nil {
		return nil, err
	}
	return readGroups(hooks, claudeHookEvent)
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
			if isACNHook(h) {
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
// 因此命令串必然同时含有可执行名与子命令前缀。
func isACNCommand(cmd string) bool {
	return strings.Contains(cmd, "acn") && strings.Contains(cmd, "hook claude")
}

func isACNHook(h hookEntry) bool {
	return isACNExecForm(h) || isACNCommand(h.Command)
}

// isACNExecForm 匹配 exec form 条目。Args[1] 覆盖 claude 与
// claude-question / claude-notification 三个子命令。
func isACNExecForm(h hookEntry) bool {
	if len(h.Args) != 2 || h.Args[0] != "hook" || !strings.HasPrefix(h.Args[1], "claude") {
		return false
	}
	command := strings.ReplaceAll(strings.TrimSpace(h.Command), `\`, "/")
	name := strings.ToLower(filepath.Base(command))
	return name == "acn" || name == "acn.exe"
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

// readGroups 取出指定事件的分组数组，缺失时返回空切片。
func readGroups(hooks map[string]json.RawMessage, eventName string) ([]json.RawMessage, error) {
	raw, ok := hooks[eventName]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("字段 hooks.%s 不是数组: %w", eventName, err)
	}
	return groups, nil
}
