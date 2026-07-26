package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/windyskr/acn/internal/config"
)

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

// installCodex 接管 notify，并把原有程序存入 acn 配置以便链式转发。
//
// Codex 的 notify 只能配一个程序。用户这里往往已经装了别的集成
// （如 Codex Computer Use），直接覆盖会静默弄坏它，所以必须先存后转。
func installCodex(exe string) error {
	path := codexConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("未找到 %s，请先运行一次 Codex", path)
		}
		return err
	}

	doc := parseTOMLLines(string(data))
	current, found := doc.notifyValue()

	// 幂等：重复安装时不能把 acn 自己记成上游，否则转发会自我递归。
	if !isACNChain(current) {
		cfg := loadConfig()
		if found {
			cfg.CodexNotifyChain = current
		} else {
			cfg.CodexNotifyChain = nil
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("保存原 notify 配置失败: %w", err)
		}
	}

	next := doc.setNotify([]string{exe, "hook", "codex"})
	return writeCodexConfig(path, next)
}

// uninstallCodex 摘除 acn 并还原安装前的 notify。
func uninstallCodex() error {
	path := codexConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	doc := parseTOMLLines(string(data))
	current, found := doc.notifyValue()
	if !found || !isACNChain(current) {
		return nil // 不是我们装的，不动它
	}

	cfg := loadConfig()
	var next string
	if len(cfg.CodexNotifyChain) > 0 {
		next = doc.setNotify(cfg.CodexNotifyChain)
	} else {
		next = doc.removeNotify()
	}
	if err := writeCodexConfig(path, next); err != nil {
		return err
	}

	// 还原完成，链信息不再需要，清掉以免下次安装读到过期值。
	cfg.CodexNotifyChain = nil
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("清理 notify 备份失败: %w", err)
	}
	return nil
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

	current, found := parseTOMLLines(string(data)).notifyValue()
	switch {
	case !found:
		st.Detail = "未安装"
	case isACNChain(current):
		st.Installed = true
		st.Detail = "notify 已接管"
		if chain := loadConfig().CodexNotifyChain; len(chain) > 0 {
			st.Detail += "，转发至 " + filepath.Base(chain[0])
		}
	default:
		st.Detail = "未安装（notify 已被 " + filepath.Base(current[0]) + " 占用，安装后将链式转发）"
	}
	return st
}

// writeCodexConfig 备份后原子写回。
func writeCodexConfig(path, content string) error {
	if err := backup(path); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	return writeFileAtomicPreservingMode(path, []byte(content), 0o600)
}

// isACNChain 判断一条 notify 配置是否指向 acn 自己。
func isACNChain(chain []string) bool {
	if len(chain) == 0 {
		return false
	}
	return strings.Contains(chain[0], "acn")
}

// tomlDoc 是按行切分的 config.toml。
//
// 这里刻意不用 TOML 库做「反序列化 → 修改 → 序列化」：那样会抹掉用户的注释、
// 空行与 [mcp_servers] 的排布。我们只需改一个顶层键，行级编辑足够且无损。
type tomlDoc struct {
	lines []string
	// start/end 是 notify 定义所占的行区间（左闭右开）；start 为 -1 表示不存在。
	start, end int
	// insertAt 是新增 notify 时的插入位置——首个表头之前，
	// 因为 TOML 顶层键一旦落到表头之后就会被解析成该表的字段。
	insertAt int
}

// parseTOMLLines 定位顶层的 notify 键。
func parseTOMLLines(content string) tomlDoc {
	doc := tomlDoc{lines: splitLines(content), start: -1}
	doc.insertAt = len(doc.lines)

	for i, line := range doc.lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			doc.insertAt = i // 顶层区域到此为止
			break
		}
		if doc.start >= 0 {
			continue
		}
		if key, value, ok := splitKeyValue(trimmed); ok && key == "notify" {
			doc.start = i
			doc.end = i + 1
			// 数组可能跨行，一直吃到方括号闭合为止。
			for !bracketsBalanced(value) && doc.end < len(doc.lines) {
				value += "\n" + doc.lines[doc.end]
				doc.end++
			}
		}
	}
	// 已有 notify 时，插入点就是它原来的位置，保持行序稳定。
	if doc.start >= 0 {
		doc.insertAt = doc.start
	}
	return doc
}

// notifyValue 返回当前 notify 的字符串数组。
func (d tomlDoc) notifyValue() ([]string, bool) {
	if d.start < 0 {
		return nil, false
	}
	joined := strings.Join(d.lines[d.start:d.end], "\n")
	_, value, ok := splitKeyValue(strings.TrimSpace(joined))
	if !ok {
		return nil, false
	}
	return parseTOMLStringArray(value), true
}

// setNotify 返回替换/插入 notify 之后的完整文本。
func (d tomlDoc) setNotify(chain []string) string {
	line := "notify = " + formatTOMLStringArray(chain)

	out := make([]string, 0, len(d.lines)+1)
	if d.start >= 0 {
		out = append(out, d.lines[:d.start]...)
		out = append(out, line)
		out = append(out, d.lines[d.end:]...)
	} else {
		out = append(out, d.lines[:d.insertAt]...)
		out = append(out, line)
		out = append(out, d.lines[d.insertAt:]...)
	}
	return joinLines(out)
}

// removeNotify 删除 notify 定义行。
func (d tomlDoc) removeNotify() string {
	if d.start < 0 {
		return joinLines(d.lines)
	}
	out := append([]string{}, d.lines[:d.start]...)
	out = append(out, d.lines[d.end:]...)
	return joinLines(out)
}

// splitKeyValue 拆出 `key = value`，跳过注释行。
func splitKeyValue(line string) (string, string, bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	key = strings.Trim(key, `"'`)
	return key, strings.TrimSpace(line[idx+1:]), true
}

// bracketsBalanced 判断方括号是否闭合，字符串字面量内的括号不计。
func bracketsBalanced(s string) bool {
	depth := 0
	var quote rune
	escaped := false
	for _, r := range s {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case r == '\\' && quote == '"':
				escaped = true
			case r == quote:
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
		case '#':
			return depth == 0 // 行内注释之后不再有值
		case '[':
			depth++
		case ']':
			depth--
		}
	}
	return depth <= 0
}

// parseTOMLStringArray 解析字符串数组字面量。只需支持基本串与字面串，
// 这是 notify 在实际配置里的全部形态。
func parseTOMLStringArray(value string) []string {
	var out []string
	var buf strings.Builder
	var quote rune
	escaped := false

	for _, r := range value {
		if quote == 0 {
			if r == '"' || r == '\'' {
				quote = r
				buf.Reset()
			}
			continue
		}
		switch {
		case escaped:
			buf.WriteRune(unescape(r))
			escaped = false
		case r == '\\' && quote == '"':
			escaped = true
		case r == quote:
			out = append(out, buf.String())
			quote = 0
		default:
			buf.WriteRune(r)
		}
	}
	return out
}

// unescape 还原 TOML 基本串中的转义字符。
func unescape(r rune) rune {
	switch r {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	default:
		return r // \" \\ 及其余情况取字符本身
	}
}

// formatTOMLStringArray 输出基本串数组。
func formatTOMLStringArray(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = quoteTOMLString(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// quoteTOMLString 转义并加引号。路径里可能含空格，必须带引号。
func quoteTOMLString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
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
