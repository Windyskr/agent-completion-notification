package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	openCodePluginFile = "acn.js"
	openCodeMarker     = "// acn:opencode-plugin"
)

func openCodeConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG_DIR")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

func openCodePluginPath() string {
	return filepath.Join(openCodeConfigDir(), "plugins", openCodePluginFile)
}

func installOpenCode(exe string) error {
	path := openCodePluginPath()
	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), openCodeMarker) {
		return fmt.Errorf("%s 已存在且不是 ACN 生成的文件，请先改名或移走", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := backup(path); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	return writeFileAtomicPreservingMode(path, []byte(openCodePlugin(exe)), 0o644)
}

func uninstallOpenCode() error {
	path := openCodePluginPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !strings.Contains(string(data), openCodeMarker) {
		return fmt.Errorf("拒绝删除非 ACN 生成的文件 %s", path)
	}
	return os.Remove(path)
}

func queryOpenCode() TargetStatus {
	st := TargetStatus{Name: "OpenCode", Path: openCodePluginPath()}
	data, err := os.ReadFile(st.Path)
	if err != nil {
		if os.IsNotExist(err) {
			st.Detail = "未安装"
		} else {
			st.Detail = "读取失败: " + err.Error()
		}
		return st
	}
	content := string(data)
	if !strings.Contains(content, openCodeMarker) {
		st.Detail = "存在同名非 ACN 插件"
		return st
	}
	st.Installed = true
	st.Exe = recordedOpenCodeExe(content)
	st.Detail = "session.idle 插件已安装"
	return st
}

func recordedOpenCodeExe(content string) string {
	const prefix = "const ACN_COMMAND = "
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		var command []string
		if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(line, prefix), ";")), &command); err == nil && len(command) > 0 {
			return command[0]
		}
	}
	return ""
}

func openCodePlugin(exe string) string {
	command, _ := json.Marshal([]string{exe, "hook", "opencode"})
	return fmt.Sprintf(`%s
const ACN_COMMAND = %s;
const recent = new Map();

function text(value) {
  return typeof value === "string" ? value.trim() : "";
}

function responseData(response) {
  return response && response.data !== undefined ? response.data : response;
}

async function loadCompletion(client, sessionID) {
  let messages = [];
  try {
    messages = responseData(await client.session.messages({ path: { id: sessionID } })) || [];
  } catch (_) {}

  let latest;
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const candidate = messages[index];
    if (candidate && candidate.info && candidate.info.role === "assistant") {
      latest = candidate;
      break;
    }
  }

  let session;
  try {
    session = responseData(await client.session.get({ path: { id: sessionID } }));
  } catch (_) {}

  const parts = latest && Array.isArray(latest.parts) ? latest.parts : [];
  const message = parts
    .filter((part) => part && part.type === "text" && part.ignored !== true)
    .map((part) => text(part.text))
    .filter(Boolean)
    .join("\n\n");
  const info = latest && latest.info ? latest.info : {};
  const started = info.time && Number(info.time.created);
  const completed = info.time && Number(info.time.completed);

  return {
    session_id: sessionID,
    session_name: text(session && session.title),
    cwd: text((session && session.directory) || (info.path && info.path.cwd)),
    message,
    duration_ms: Number.isFinite(started) && Number.isFinite(completed) && completed > started
      ? completed - started
      : 0,
  };
}

function dispatch(payload) {
  try {
    Bun.spawn({
      cmd: ACN_COMMAND,
      cwd: payload.cwd || undefined,
      stdin: new Response(JSON.stringify(payload)).body,
      stdout: "ignore",
      stderr: "ignore",
    });
  } catch (_) {}
}

export const ACNPlugin = async ({ client, directory, worktree }) => ({
  event: async ({ event }) => {
    if (!event || event.type !== "session.idle") return;
    const sessionID = text(event.properties && event.properties.sessionID);
    if (!sessionID) return;

    const now = Date.now();
    if (now - (recent.get(sessionID) || 0) < 1500) return;
    recent.set(sessionID, now);

    const payload = await loadCompletion(client, sessionID);
    if (!payload.cwd) payload.cwd = text(worktree || directory);
    dispatch(payload);
  },
});
`, openCodeMarker, command)
}
