package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/windyskr/acn/internal/config"
	"github.com/windyskr/acn/internal/event"
)

// shortTempDir 返回一个短路径临时目录。Unix socket 的路径长度上限约 104 字节，
// t.TempDir() 拼出的长名字会直接把 bind 撑爆。
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "acn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestDeduperSuppressesWithinWindow(t *testing.T) {
	d := newDeduper(2 * time.Minute)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)

	if d.seenRecently("fp", base) {
		t.Error("首次出现不应判定为重复")
	}
	if !d.seenRecently("fp", base.Add(30*time.Second)) {
		t.Error("窗口内重复未被抑制")
	}
	if d.seenRecently("other", base.Add(30*time.Second)) {
		t.Error("不同指纹被误判为重复")
	}
	// 窗口外应放行，否则同一项目的下一次任务永远发不出去。
	if d.seenRecently("fp", base.Add(3*time.Minute)) {
		t.Error("窗口外仍被抑制")
	}
}

// 过期项要被清理，避免长时间运行的 daemon 无限占用内存。
func TestDeduperEvictsExpired(t *testing.T) {
	d := newDeduper(time.Minute)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 50; i++ {
		d.seenRecently(string(rune('a'+i%26))+string(rune('0'+i/26)), base)
	}
	d.seenRecently("trigger", base.Add(2*time.Minute)) // 触发清理

	if len(d.seen) != 1 {
		t.Errorf("过期项未被清理，残留 %d 条", len(d.seen))
	}
}

// 端到端：hook 侧 Send → daemon 收下 → 推送到（假）飞书。
func TestRunDeliversEventToChannel(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv(config.EnvConfigDir, dir)

	received := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		select {
		case received <- parsed:
		default:
		}
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	if err := config.Save(config.Config{Feishu: config.Feishu{WebhookURL: srv.URL}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx) }()

	waitForDaemon(t)

	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", Message: "改好了", DurationMS: 5_000}
	if err := Send(ev); err != nil {
		t.Fatalf("投递失败: %v", err)
	}

	select {
	case got := <-received:
		if got["msg_type"] != "post" {
			t.Errorf("渠道收到的载荷有误: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超时：事件未送达通知渠道")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("daemon 退出报错: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon 未能优雅退出")
	}

	// socket 文件应随退出一并清理。
	if _, err := os.Stat(filepath.Join(dir, "acn.sock")); !os.IsNotExist(err) {
		t.Error("退出后 socket 文件未清理")
	}
}

// 同一事件重复投递只应触发一次推送。
func TestRunDeduplicatesRepeatedEvents(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv(config.EnvConfigDir, dir)

	var hits atomic.Int32
	done := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{"code":0}`)
		done <- struct{}{}
	}))
	defer srv.Close()

	if err := config.Save(config.Config{Feishu: config.Feishu{WebhookURL: srv.URL}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx)
	waitForDaemon(t)

	ev := event.Event{Source: event.SourceClaude, Cwd: "/work/acn", Message: "同一次完成"}
	for i := 0; i < 3; i++ {
		if err := Send(ev); err != nil {
			t.Fatalf("第 %d 次投递失败: %v", i+1, err)
		}
		// 串行投递，确保去重表在下一次之前已更新。
		if i == 0 {
			<-done
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	time.Sleep(200 * time.Millisecond)

	if got := hits.Load(); got != 1 {
		t.Errorf("推送 %d 次，期望去重后只推 1 次", got)
	}
}

// daemon 未运行时 Send 必须报错，让 hook 侧能降级为直发。
func TestSendFailsWithoutDaemon(t *testing.T) {
	t.Setenv(config.EnvConfigDir, shortTempDir(t))
	if err := Send(event.Event{Source: event.SourceClaude}); err == nil {
		t.Error("daemon 未运行时 Send 应当报错")
	}
	if Running() {
		t.Error("daemon 未运行时 Running 应为 false")
	}
}

// 上次异常退出残留的 socket 文件不能挡住下次启动。
func TestRunClearsStaleSocket(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv(config.EnvConfigDir, dir)

	stale := filepath.Join(dir, "acn.sock")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx) }()

	waitForDaemon(t)
	if !Running() {
		t.Error("残留 socket 文件导致启动失败")
	}
	cancel()
	<-errc
}

// waitForDaemon 轮询等待 daemon 就绪。
func waitForDaemon(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if Running() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon 启动超时")
}
