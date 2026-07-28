package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// acn 写入 config.toml 的块由这对哨兵界定。用注释标记而非解析 TOML 结构，
// 摘除时就能精确到行，用户手写的表、注释与格式一概不受影响。
const (
	codexBegin = "# >>> acn begin (由 acn install 生成，请勿手改) >>>"
	codexEnd   = "# <<< acn end <<<"
)

// codexHookTimeout 是 hook 的执行上限（秒）。
const codexHookTimeout = 10

// codexConfigPath 返回 Codex CLI 的配置路径，遵循 CODEX_HOME。
func codexConfigPath() string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return filepath.Join(v, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex", "config.toml")
	}
	return filepath.Join(home, ".codex", "config.toml")
}

// installCodex 幂等地追加 [[hooks.Stop]] 块。
//
// 用 hooks 而非顶层 notify：notify 是遗留路径（二进制里就叫 legacy_notify），
// 且全局只有一个槽位——抢占它会破坏用户已有的集成（如 Codex Computer Use），
// 还会被对方的下次安装静默顶掉。hooks 可以多个并存，互不干扰。
func installCodex(exe string) error {
	path := codexConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("未找到 %s，请先运行一次 Codex", path)
		}
		return err
	}

	// 先摘旧块再追加，重复安装才不会堆积。
	body := stripACNBlock(string(data))
	if !strings.HasSuffix(body, "\n") && body != "" {
		body += "\n"
	}
	groupIndex := codexStopGroupCount(body)
	return writeCodexConfig(path, body+codexHookBlock(path, exe, groupIndex))
}

// uninstallCodex 摘除 acn 的块。
func uninstallCodex() error {
	path := codexConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	body := string(data)
	next := stripACNBlock(body)
	if next == body {
		return nil // 没装过，不动它
	}
	return writeCodexConfig(path, next)
}

// queryCodex 探测安装状态。
func queryCodex() TargetStatus {
	st := TargetStatus{Name: "Codex", Path: codexConfigPath()}

	data, err := os.ReadFile(st.Path)
	if err != nil {
		if os.IsNotExist(err) {
			st.Detail = "未安装（配置文件不存在）"
		} else {
			st.Detail = "读取失败: " + err.Error()
		}
		return st
	}
	if strings.Contains(string(data), codexBegin) {
		st.Installed = true
		st.Exe = recordedCodexExe(string(data))
		st.TrustKnown = true
		st.Trusted = codexHookTrusted(
			string(data), st.Path, st.Exe, codexACNStopGroupIndex(string(data)),
		)
		if st.Trusted {
			st.Detail = "Stop hook 已安装并信任"
		} else {
			st.Detail = "Stop hook 已安装，信任状态不匹配"
		}
		// 只在装了之后才提醒——没装的话这个前提无关紧要。
		if hooksDisabled(string(data)) {
			st.Warning = "config.toml 里 [features] hooks = false，Stop hook 不会触发"
		}
	} else {
		st.Detail = "未安装"
	}
	return st
}

// recordedCodexExe 从 acn 块里取出记录的 acn 路径。
func recordedCodexExe(content string) string {
	for _, line := range splitLines(content) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "command" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			unescaped := strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(value[1 : len(value)-1])
			if exe := extractExe(unescaped, "codex"); exe != "" {
				return exe
			}
		}
	}
	return ""
}

// codexHookBlock 渲染要写入的块。
//
// 注意 hook 绝不能往 stdout 写东西：Stop hook 输出 {"decision":"block"} 会让
// Codex 自动续跑一轮。acn 的日志一律走 stderr。
func codexHookBlock(configPath, exe string, groupIndex int) string {
	command := hookCommand(exe, "codex")
	return strings.Join([]string{
		codexBegin,
		"[[hooks.Stop]]",
		"",
		"[[hooks.Stop.hooks]]",
		`type = "command"`,
		"command = " + quoteTOMLString(command),
		fmt.Sprintf("timeout = %d", codexHookTimeout),
		`statusMessage = "acn 通知"`,
		"",
		"[hooks.state." + quoteTOMLKey(codexHookStateKey(configPath, groupIndex)) + "]",
		"trusted_hash = " + quoteTOMLString(codexHookHash(command)),
		codexEnd,
		"",
	}, "\n")
}

type codexNormalizedHookIdentity struct {
	EventName string                       `json:"event_name"`
	Hooks     []codexNormalizedCommandHook `json:"hooks"`
}

type codexNormalizedCommandHook struct {
	Async         bool   `json:"async"`
	Command       string `json:"command"`
	StatusMessage string `json:"statusMessage"`
	Timeout       int    `json:"timeout"`
	Type          string `json:"type"`
}

// codexHookHash 复现 Codex 对规范化 command hook 身份的版本计算。Codex 会
// 先把结构转为 TOML（因此省略 None 字段），再对按键排序的 JSON 做 SHA-256。
// 这里只生成 ACN 固定的 Stop hook，字段顺序就是 canonical JSON 的键顺序。
func codexHookHash(command string) string {
	identity := codexNormalizedHookIdentity{
		EventName: "stop",
		Hooks: []codexNormalizedCommandHook{
			{
				Command:       command,
				StatusMessage: "acn 通知",
				Timeout:       codexHookTimeout,
				Type:          "command",
			},
		},
	}

	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(identity); err != nil {
		panic("固定的 Codex hook 身份无法序列化: " + err.Error())
	}
	sum := sha256.Sum256(bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}))
	return fmt.Sprintf("sha256:%x", sum)
}

func codexHookStateKey(configPath string, groupIndex int) string {
	return fmt.Sprintf("%s:stop:%d:0", canonicalCodexConfigPath(configPath), groupIndex)
}

func codexHookTrusted(content, configPath, exe string, groupIndex int) bool {
	if exe == "" || groupIndex < 0 {
		return false
	}
	header := "[hooks.state." + quoteTOMLKey(codexHookStateKey(configPath, groupIndex)) + "]"
	expected := "trusted_hash = " + quoteTOMLString(codexHookHash(hookCommand(exe, "codex")))
	inState := false
	for _, line := range splitLines(content) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inState = trimmed == header
			continue
		}
		if inState && trimmed == expected {
			return true
		}
	}
	return false
}

func codexStopGroupCount(content string) int {
	count := 0
	for _, line := range splitLines(content) {
		if strings.TrimSpace(line) == "[[hooks.Stop]]" {
			count++
		}
	}
	return count
}

func codexACNStopGroupIndex(content string) int {
	index := 0
	for _, line := range splitLines(content) {
		switch strings.TrimSpace(line) {
		case codexBegin:
			return index
		case "[[hooks.Stop]]":
			index++
		}
	}
	return -1
}

// hooksDisabled 判断用户是否在 [features] 里显式关掉了 hooks。
// 关掉之后块照样写得进去，但永远不会触发——这种无声失败必须说出来。
func hooksDisabled(content string) bool {
	inFeatures := false
	for _, line := range splitLines(content) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inFeatures = trimmed == "[features]"
			continue
		}
		if !inFeatures {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) != "hooks" {
			continue
		}
		value, _, _ = strings.Cut(value, "#") // 去掉行内注释
		return strings.TrimSpace(value) == "false"
	}
	return false
}

// stripACNBlock 删除哨兵之间的内容（含哨兵本身）与其前导空行。
// 找不到成对哨兵时原样返回。
func stripACNBlock(content string) string {
	lines := splitLines(content)

	start, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case codexBegin:
			start = i
		case codexEnd:
			if start >= 0 && end < 0 {
				end = i
			}
		}
	}
	if start < 0 || end < start {
		return content
	}

	// 连同块前的空行一并回收，避免反复装卸堆出一片空白。
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}

	kept := append([]string{}, lines[:start]...)
	kept = append(kept, lines[end+1:]...)
	return joinLines(kept)
}

// writeCodexConfig 备份后原子写回。
func writeCodexConfig(path, content string) error {
	if err := backup(path); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	return writeFileAtomicPreservingMode(path, []byte(content), 0o600)
}

// quoteTOMLString 转义并加引号。路径里可能含空格，必须带引号。
func quoteTOMLString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

// quoteTOMLKey 优先用 literal key 保持 Codex 自身写出的格式；遇到单引号时
// 回退到 basic string，避免用户名或目录名让 TOML 失效。
func quoteTOMLKey(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	return quoteTOMLString(s)
}

// splitLines 按 \n 切分，并剥掉行尾的 \r 以兼容 CRLF。
func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	// 末尾换行会切出一个空元素；去掉它，由 joinLines 统一补回。
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// joinLines 拼回文本并保证以换行结尾。
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
