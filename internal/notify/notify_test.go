package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
)

func TestGateBlocksDisabledSource(t *testing.T) {
	cfg := config.Config{Sources: map[string]bool{"codex": false}}
	if reason := Gate(cfg, event.Event{Source: event.SourceCodex}); reason == "" {
		t.Error("已禁用的来源仍被放行")
	}
	// 未列出的来源默认开启。
	if reason := Gate(cfg, event.Event{Source: event.SourceClaude}); reason != "" {
		t.Errorf("未显式配置的来源应默认开启，却被拦截：%s", reason)
	}
}

func TestGateAppliesDurationThreshold(t *testing.T) {
	cfg := config.Config{MinDurationSeconds: 30}

	if reason := Gate(cfg, event.Event{Source: event.SourceClaude, DurationMS: 5_000}); reason == "" {
		t.Error("低于阈值的任务应被拦截")
	}
	if reason := Gate(cfg, event.Event{Source: event.SourceClaude, DurationMS: 60_000}); reason != "" {
		t.Errorf("高于阈值的任务被误拦：%s", reason)
	}
}

// 耗时未知时阈值不能生效，否则会把取不到起点的通知全部挡掉。
func TestGateSkipsThresholdWhenDurationUnknown(t *testing.T) {
	cfg := config.Config{MinDurationSeconds: 30}
	if reason := Gate(cfg, event.Event{Source: event.SourceCodex, DurationMS: 0}); reason != "" {
		t.Errorf("耗时未知时不应套用阈值，却被拦截：%s", reason)
	}
}

// 被 Gate 拦下时不得发出任何请求。
func TestSendSkipsWithoutHTTPCall(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := config.Config{
		Feishu:  config.Feishu{WebhookURL: srv.URL},
		Sources: map[string]bool{"claude": false},
	}
	skipped, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude})

	if err != nil {
		t.Fatal(err)
	}
	if skipped == "" {
		t.Error("应报告跳过原因")
	}
	if hit {
		t.Error("被拦截的事件仍发出了 HTTP 请求")
	}
}

func TestSendSkipsIgnoredDirectoryWithoutHTTPCall(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	root := t.TempDir()
	cfg := config.Config{
		Feishu:             config.Feishu{WebhookURL: srv.URL},
		IgnoredDirectories: []string{root},
	}
	skipped, err := Send(context.Background(), cfg, event.Event{
		Source: event.SourceClaude,
		Cwd:    filepath.Join(root, "nested"),
	})

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skipped, "目录已忽略") {
		t.Errorf("跳过原因 = %q, 期望报告目录已忽略", skipped)
	}
	if hit {
		t.Error("被忽略目录中的事件仍发出了 HTTP 请求")
	}
}

func TestSendReportsMissingChannel(t *testing.T) {
	skipped, err := Send(context.Background(), config.Config{}, event.Event{Source: event.SourceClaude})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skipped, "未配置") {
		t.Errorf("跳过原因 = %q, 期望提示未配置", skipped)
	}
}

func TestSendDeliversAllConfiguredChannels(t *testing.T) {
	var feishuBody, barkBody string
	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		feishuBody = string(body)
		io.WriteString(w, `{"code":0}`)
	}))
	defer feishuSrv.Close()
	barkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		barkBody = string(body)
		io.WriteString(w, `{"code":200,"message":"success"}`)
	}))
	defer barkSrv.Close()

	cfg := config.Config{
		Feishu:          config.Feishu{WebhookURL: feishuSrv.URL},
		Bark:            config.Bark{URL: barkSrv.URL + "/device-key"},
		DeviceName:      "devbox",
		ShowDeviceName:  true,
		ShowAgentName:   true,
		ShowProjectName: true,
		AgentNames:      map[string]string{"claude": "opus"},
	}
	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", SessionName: "修复通知", Message: "改好了"}

	skipped, err := Send(context.Background(), cfg, ev)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != "" {
		t.Errorf("不应跳过：%s", skipped)
	}
	for channel, body := range map[string]string{"feishu": feishuBody, "bark": barkBody} {
		if !strings.Contains(body, "改好了") {
			t.Errorf("%s 请求体未包含正文：%s", channel, body)
		}
		if !strings.Contains(body, "devbox-opus-acn-修复通知") {
			t.Errorf("%s 请求体未包含标题：%s", channel, body)
		}
	}
}

func TestSendDeliversSlackAndTeams(t *testing.T) {
	var slackBody, teamsBody string
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		slackBody = string(body)
		io.WriteString(w, "ok")
	}))
	t.Cleanup(slackSrv.Close)
	teamsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		teamsBody = string(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(teamsSrv.Close)

	cfg := config.Config{
		Slack: config.Slack{WebhookURL: slackSrv.URL},
		Teams: config.Teams{WebhookURL: teamsSrv.URL},
	}
	_, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude, SessionName: "新渠道", Message: "完成"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(slackBody, "新渠道") || !strings.Contains(slackBody, "完成") {
		t.Errorf("Slack 请求体错误: %s", slackBody)
	}
	if !strings.Contains(teamsBody, "新渠道") || !strings.Contains(teamsBody, "完成") {
		t.Errorf("Teams 请求体错误: %s", teamsBody)
	}
}

func TestSendUsesOnlySessionNameByDefault(t *testing.T) {
	var payload struct {
		Content struct {
			Post map[string]struct {
				Title string `json:"title"`
			} `json:"post"`
		} `json:"content"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("解析请求失败: %v", err)
		}
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Feishu.WebhookURL = srv.URL
	cfg.Channels[config.ChannelBark] = false
	ev := event.Event{
		Source: event.SourceCodex, Cwd: "/work/acn",
		SessionName: "完善组件消融实验方案", Message: "完成",
	}

	if _, err := Send(context.Background(), cfg, ev); err != nil {
		t.Fatal(err)
	}
	if got := payload.Content.Post["zh_cn"].Title; got != "完善组件消融实验方案" {
		t.Errorf("默认标题 = %q, 期望只显示会话名", got)
	}
}

func TestSendAttemptsOtherChannelAfterFailure(t *testing.T) {
	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":19021,"msg":"sign match fail"}`)
	}))
	defer feishuSrv.Close()
	var barkHit bool
	barkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		barkHit = true
		io.WriteString(w, `{"code":200,"message":"success"}`)
	}))
	defer barkSrv.Close()

	cfg := config.Config{
		Feishu: config.Feishu{WebhookURL: feishuSrv.URL},
		Bark:   config.Bark{URL: barkSrv.URL},
	}
	_, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude})
	if err == nil || !strings.Contains(err.Error(), "feishu") {
		t.Errorf("应报告飞书失败并标注渠道，得到 %v", err)
	}
	if !barkHit {
		t.Error("飞书失败后 Bark 仍应被调用")
	}
}

func TestSendSkipsDisabledChannel(t *testing.T) {
	var barkHit bool
	barkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		barkHit = true
		io.WriteString(w, `{"code":200}`)
	}))
	defer barkSrv.Close()

	cfg := config.Default()
	cfg.Bark.URL = barkSrv.URL
	cfg.Channels["bark"] = false
	skipped, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude})
	if err != nil || skipped == "" {
		t.Errorf("禁用唯一渠道后应跳过，skipped=%q err=%v", skipped, err)
	}
	if barkHit {
		t.Error("已禁用 Bark 仍发出了请求")
	}
}

func TestSendPropagatesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":19021,"msg":"sign match fail"}`)
	}))
	defer srv.Close()

	cfg := config.Config{Feishu: config.Feishu{WebhookURL: srv.URL}}
	if _, err := Send(context.Background(), cfg, event.Event{Source: event.SourceClaude}); err == nil {
		t.Error("飞书业务失败应当上报")
	}
}
