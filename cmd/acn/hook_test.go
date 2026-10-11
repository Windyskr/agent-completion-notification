package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/eventlog"
	"github.com/windyskr/agent-completion-notification/internal/hookdedup"
)

// hookEnv 隔离 hook 依赖的全部外部状态：配置目录、临时目录、各 CLI 的数据目录与
// 全部 ACN_* 环境变量，避免测试读到开发者本机配置，更不能真的推送出去。
func hookEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfigDir, filepath.Join(dir, "acn"))
	t.Setenv("TMPDIR", tmp)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	t.Setenv("PASEO_HOME", filepath.Join(dir, "paseo"))
	t.Setenv("PASEO_HOOK_CLI", filepath.Join(dir, "missing-paseo"))
	for _, key := range []string{
		"PASEO_AGENT_ID", "PASEO_TERMINAL_ID",
		config.EnvWebhook, config.EnvSecret, config.EnvDeviceName, config.EnvBarkURL,
		config.EnvDingTalkWebhook, config.EnvDingTalkSecret, config.EnvWeComWebhook,
		config.EnvTelegramToken, config.EnvTelegramChatID,
		config.EnvEmailSMTP, config.EnvEmailUsername, config.EnvEmailPassword, config.EnvEmailFrom, config.EnvEmailTo,
		config.EnvSlackWebhook, config.EnvTeamsWebhook,
	} {
		t.Setenv(key, "")
	}
	return dir
}

// slackCapture 起一个假 Slack webhook，记录收到的每条文本。
type slackCapture struct {
	mu     sync.Mutex
	texts  []string
	status int
}

func (c *slackCapture) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		c.mu.Lock()
		c.texts = append(c.texts, payload.Text)
		status := c.status
		c.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *slackCapture) received() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

// saveHookConfig 写入只启用 Slack 且开启事件日志的配置。
func saveHookConfig(t *testing.T, slackURL string) {
	t.Helper()
	cfg := config.Default()
	cfg.Slack.WebhookURL = slackURL
	cfg.EventLogEnabled = true
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// runHookForTest 执行一次 hook，返回 stderr；hook 写 stdout 视为失败。
func runHookForTest(t *testing.T, source, payload string) string {
	t.Helper()
	var stderr bytes.Buffer
	stdout := captureStdout(t, func() error {
		runHook(source, strings.NewReader(payload), &stderr)
		return nil
	})
	if stdout != "" {
		t.Errorf("hook 向 stdout 写了内容（Codex 会把它当作控制数据）: %q", stdout)
	}
	return stderr.String()
}

func readEventLog(t *testing.T) []eventlog.Entry {
	t.Helper()
	f, err := os.Open(config.EventLogPath())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []eventlog.Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var entry eventlog.Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("事件日志不是合法 JSONL: %v\n%s", err, scanner.Text())
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return entries
}

func onlyLogEntry(t *testing.T) eventlog.Entry {
	t.Helper()
	entries := readEventLog(t)
	if len(entries) != 1 {
		t.Fatalf("事件日志条数 = %d, 期望 1", len(entries))
	}
	return entries[0]
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writePaseoWorkspace 伪造 Paseo 的本地记录：agentID 属于标题为 title 的 Workspace。
func writePaseoWorkspace(t *testing.T, dir, agentID, title string) {
	t.Helper()
	t.Setenv("PASEO_AGENT_ID", agentID)
	writeFile(t, filepath.Join(dir, "paseo", "agents", "project", agentID+".json"), `{"workspaceId":"w1"}`)
	writeFile(t, filepath.Join(dir, "paseo", "projects", "workspaces.json"),
		fmt.Sprintf(`[{"workspaceId":"w1","title":%q}]`, title))
}

func TestRunHookDeliversClaudeStop(t *testing.T) {
	dir := hookEnv(t)
	var slack slackCapture
	saveHookConfig(t, slack.serve(t).URL)
	transcript := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, transcript, strings.Join([]string{
		`{"type":"ai-title","aiTitle":"修复登录问题"}`,
		`{"type":"user","timestamp":"2026-07-26T10:00:00Z","message":{"role":"user","content":"帮我改个 bug"}}`,
		`{"type":"assistant","timestamp":"2026-07-26T10:01:30Z","message":{"role":"assistant","content":[{"type":"text","text":"改好了"}]}}`,
	}, "\n")+"\n")

	stderr := runHookForTest(t, "claude", fmt.Sprintf(
		`{"session_id":"s1","transcript_path":%q,"cwd":"/work/acn","hook_event_name":"Stop"}`, transcript))

	if stderr != "" {
		t.Errorf("成功投递不应输出诊断信息: %s", stderr)
	}
	texts := slack.received()
	if len(texts) != 1 {
		t.Fatalf("推送次数 = %d, 期望 1", len(texts))
	}
	for _, want := range []string{"修复登录问题", "改好了", "耗时：1m30s", "目录：/work/acn"} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("推送内容缺少 %q:\n%s", want, texts[0])
		}
	}
	entry := onlyLogEntry(t)
	if entry.HookSource != "claude" || entry.Skip || len(entry.Delivery) != 1 || !entry.Delivery[0].Sent {
		t.Errorf("事件日志 = %+v", entry)
	}
}

func TestRunHookRecordsParseError(t *testing.T) {
	hookEnv(t)
	var slack slackCapture
	saveHookConfig(t, slack.serve(t).URL)

	stderr := runHookForTest(t, "claude", `{broken`)

	if !strings.Contains(stderr, "解析载荷失败") {
		t.Errorf("stderr = %q, 期望报告解析失败", stderr)
	}
	if len(slack.received()) != 0 {
		t.Error("载荷损坏时不应推送")
	}
	if entry := onlyLogEntry(t); entry.ParseError == "" || entry.Event != nil {
		t.Errorf("事件日志应记录解析错误: %+v", entry)
	}
}

func TestRunHookSkipsUnactionableNotification(t *testing.T) {
	hookEnv(t)
	var slack slackCapture
	saveHookConfig(t, slack.serve(t).URL)

	runHookForTest(t, "claude-notification", `{"session_id":"s1","notification_type":"auth_success"}`)

	if len(slack.received()) != 0 {
		t.Error("无需行动的通知类型不应推送")
	}
	if entry := onlyLogEntry(t); !entry.Skip || entry.SkippedReason != "上游事件无需推送" {
		t.Errorf("事件日志 = %+v", entry)
	}
}

func TestRunHookSkipsConcurrentDuplicate(t *testing.T) {
	hookEnv(t)
	var slack slackCapture
	saveHookConfig(t, slack.serve(t).URL)
	claimed, release, err := hookdedup.Claim("codex", "s1")
	if err != nil || !claimed {
		t.Fatalf("预先占位失败: claimed=%v err=%v", claimed, err)
	}
	defer release()

	runHookForTest(t, "codex", `{"session_id":"s1","cwd":"/work/acn","last_assistant_message":"完成"}`)

	if len(slack.received()) != 0 {
		t.Error("同一会话并发到达的重复事件不应推送")
	}
	if entry := onlyLogEntry(t); entry.SkippedReason != "重复 hook 事件" {
		t.Errorf("事件日志 = %+v", entry)
	}
}

func TestRunHookUsesPaseoWorkspaceTitle(t *testing.T) {
	dir := hookEnv(t)
	var slack slackCapture
	saveHookConfig(t, slack.serve(t).URL)
	writePaseoWorkspace(t, dir, "agent-1", "重构推送逻辑")

	runHookForTest(t, "codex", `{"session_id":"s1","cwd":"/work/acn","last_assistant_message":"完成"}`)

	texts := slack.received()
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "重构推送逻辑\n") {
		t.Errorf("推送内容 = %q, 期望以 Workspace 标题开头", texts)
	}
	if entry := onlyLogEntry(t); entry.PaseoWorkspaceTitle != "重构推送逻辑" {
		t.Errorf("事件日志 = %+v", entry)
	}
}

func TestRunHookSkipsPaseoTitleGeneration(t *testing.T) {
	hookEnv(t)
	var slack slackCapture
	saveHookConfig(t, slack.serve(t).URL)
	t.Setenv("PASEO_AGENT_ID", "title-agent")

	runHookForTest(t, "codex", `{"session_id":"s1","last_assistant_message":"{\"title\":\"Run tests\"}"}`)

	if len(slack.received()) != 0 {
		t.Error("Paseo 标题生成任务不应推送")
	}
	if entry := onlyLogEntry(t); !entry.PaseoTitleGeneration || entry.SkippedReason != "Paseo 标题生成任务" {
		t.Errorf("事件日志 = %+v", entry)
	}
}

func TestRunHookReportsDeliveryFailure(t *testing.T) {
	hookEnv(t)
	slack := slackCapture{status: http.StatusInternalServerError}
	saveHookConfig(t, slack.serve(t).URL)

	stderr := runHookForTest(t, "codex", `{"session_id":"s1","last_assistant_message":"完成"}`)

	if !strings.Contains(stderr, "推送失败") {
		t.Errorf("stderr = %q, 期望报告推送失败", stderr)
	}
	if entry := onlyLogEntry(t); entry.DeliveryError == "" || len(entry.Delivery) != 1 || entry.Delivery[0].Sent {
		t.Errorf("事件日志应记录失败渠道: %+v", entry)
	}
}

// 配置文件损坏时 hook 仍要静默返回，只在 stderr 说明原因。
func TestRunHookReportsBrokenConfig(t *testing.T) {
	hookEnv(t)
	writeFile(t, config.Path(), `{broken`)

	stderr := runHookForTest(t, "codex", `{"session_id":"s1","last_assistant_message":"完成"}`)

	if !strings.Contains(stderr, "加载配置失败") {
		t.Errorf("stderr = %q, 期望报告配置加载失败", stderr)
	}
}
