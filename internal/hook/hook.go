// Package hook 承载 Claude Code 与 Codex 共用的 Stop hook 契约。
//
// 两者的 Stop 载荷 schema 几乎一致（Codex 是 Claude 的超集），且都把本轮记录写在
// 一个 JSONL transcript 里，因此载荷解析与尾部扫描收敛到这里，来源适配层只保留
// 各自真正不同的部分。
package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxStdin 限制载荷体积，避免异常输入撑爆内存。
const maxStdin = 1 << 20

// MaxScanBytes 是 transcript 的默认扫描上限。只关心最后一轮，从尾部截取即可。
//
// 这是 hook 端最大的一笔开销：解析 1.7MB 的 transcript 约需 45ms，而 hook 会
// 阻塞 CLI 返回。512KB 足以覆盖最后一轮（含若干大 tool_result），再多就是白花时间。
const MaxScanBytes = 512 << 10

// StopPayload 是 Stop hook 经 stdin 传入的载荷。
//
// Claude Code 只提供前五个字段；Codex 额外给出 LastAssistantMessage 与 TurnID，
// 省去一次 transcript 扫描。
type StopPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`

	TurnID               string `json:"turn_id"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// ReadStop 从 stdin 读取并解析 Stop 载荷。
func ReadStop(r io.Reader) (StopPayload, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxStdin))
	if err != nil {
		return StopPayload{}, fmt.Errorf("读取 stdin 失败: %w", err)
	}
	var p StopPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return StopPayload{}, fmt.Errorf("解析 Stop 载荷失败: %w", err)
	}
	return p, nil
}

// ScanTail 逐行读取 JSONL transcript 的尾部，对每个非空行调用 fn。
//
// 用 Reader 而非 Scanner：单行可能远超 Scanner 的 64KB 上限（大 tool_result），
// Scanner 遇到超长行会中断并丢掉后续所有行。
func ScanTail(path string, maxBytes int64, fn func(line string)) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("transcript 路径为空")
	}
	f, err := os.Open(ExpandHome(path))
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	truncated := info.Size() > maxBytes
	if truncated {
		if _, err := f.Seek(info.Size()-maxBytes, io.SeekStart); err != nil {
			return err
		}
	}

	r := bufio.NewReaderSize(f, 256<<10)
	if truncated {
		// 截断点通常落在某行中间，丢弃这半行。
		if _, err := r.ReadString('\n'); err != nil && err != io.EOF {
			return err
		}
	}
	for {
		line, err := r.ReadString('\n')
		if s := strings.TrimSpace(line); s != "" {
			fn(s)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// ExpandHome 展开路径开头的 ~。
func ExpandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
