// Package daemon 提供常驻服务与其客户端。
//
// hook 端只需把事件写入 Unix socket 便可立即退出（Claude Code 的 Stop hook 会
// 阻塞 CLI 返回，必须尽快让出），网络请求交由 daemon 异步完成。
// daemon 未运行时客户端返回错误，由调用方降级为同步直发。
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/windyskr/acn/internal/config"
	"github.com/windyskr/acn/internal/event"
	"github.com/windyskr/acn/internal/notify"
)

const (
	// dedupeWindow 内指纹相同的事件只推送一次。daemon 路径与直发路径可能
	// 先后上报同一次完成，靠它兜住。
	dedupeWindow = 2 * time.Minute
	// dialTimeout 是客户端连接 daemon 的等待上限，超时即判定 daemon 未运行。
	dialTimeout = 300 * time.Millisecond
	// readTimeout 防止异常连接长期占用 goroutine。
	readTimeout = 5 * time.Second
	// maxPayload 限制单个事件体积。
	maxPayload = 1 << 20
)

// Send 把事件投递给 daemon。返回错误表示 daemon 不可用。
func Send(ev event.Event) error {
	conn, err := net.DialTimeout("unix", config.SocketPath(), dialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(dialTimeout)); err != nil {
		return err
	}
	_, err = conn.Write(append(data, '\n'))
	return err
}

// Running 探测 daemon 是否在运行。以能否连通 socket 为准，而非 socket 文件
// 是否存在——异常退出会留下无人监听的残留文件。
func Running() bool {
	conn, err := net.DialTimeout("unix", config.SocketPath(), dialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Run 启动 daemon 并阻塞直至 ctx 取消。
func Run(ctx context.Context) error {
	sock := config.SocketPath()
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := clearStaleSocket(sock); err != nil {
		return err
	}

	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", sock, err)
	}
	defer ln.Close()
	defer os.Remove(sock)

	if err := os.Chmod(sock, 0o600); err != nil {
		return fmt.Errorf("设置 socket 权限失败: %w", err)
	}

	log.Printf("acn daemon 已启动，监听 %s", sock)
	if cfg, err := config.Load(); err != nil {
		log.Printf("警告：配置加载失败，将在收到事件时重试：%v", err)
	} else if !cfg.FeishuReady() {
		log.Printf("警告：尚未配置飞书 webhook，事件将被记录但不会推送")
	}

	// ctx 取消时关闭监听，使 Accept 立即返回。
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	dd := newDeduper(dedupeWindow)
	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("接受连接失败: %v", err)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle(ctx, conn, dd)
		}()
	}

	wg.Wait() // 等待在途推送完成后再退出
	log.Printf("acn daemon 已停止")
	return nil
}

// handle 读取单个事件并处理。每个连接只承载一个事件。
func handle(ctx context.Context, conn net.Conn, dd *Deduper) {
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return
	}
	var ev event.Event
	if err := json.NewDecoder(io.LimitReader(conn, maxPayload)).Decode(&ev); err != nil {
		// 连上就断、一个字节没发的是探活（Running），不是错误。
		if !errors.Is(err, io.EOF) {
			log.Printf("解析事件失败: %v", err)
		}
		return
	}
	Process(ctx, ev, dd)
}

// Process 执行去重与投递。配置在每个事件到达时重新加载，
// 因此修改 config.json 后无需重启 daemon。
func Process(ctx context.Context, ev event.Event, dd *Deduper) {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("加载配置失败: %v", err)
		return
	}
	if dd != nil && dd.seenRecently(ev.Fingerprint(), time.Now()) {
		log.Printf("[%s] %s 重复事件，已忽略", ev.Source, ev.Project())
		return
	}

	ctx, cancel := context.WithTimeout(ctx, notify.SendTimeout)
	defer cancel()

	for _, r := range notify.Dispatch(ctx, cfg, notify.Build(cfg), ev) {
		log.Printf("[%s] %s → %s", ev.Source, ev.Project(), r)
	}
}

// NewDeduper 供非 daemon 路径复用同一去重窗口。
func NewDeduper() *Deduper { return newDeduper(dedupeWindow) }

// clearStaleSocket 处理上次异常退出残留的 socket 文件：
// 能连通说明已有 daemon 在运行，直接报错；连不通则视为残留并删除。
func clearStaleSocket(sock string) error {
	if _, err := os.Stat(sock); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if conn, err := net.DialTimeout("unix", sock, dialTimeout); err == nil {
		conn.Close()
		return fmt.Errorf("daemon 已在运行（%s）", sock)
	}
	return os.Remove(sock)
}

// Deduper 在时间窗口内抑制指纹相同的事件。
type Deduper struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	window time.Duration
}

func newDeduper(window time.Duration) *Deduper {
	return &Deduper{seen: make(map[string]time.Time), window: window}
}

// seenRecently 判断指纹是否在窗口内出现过，并顺带清理过期项。
func (d *Deduper) seenRecently(fingerprint string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for k, t := range d.seen {
		if now.Sub(t) > d.window {
			delete(d.seen, k)
		}
	}
	if t, ok := d.seen[fingerprint]; ok && now.Sub(t) <= d.window {
		return true
	}
	d.seen[fingerprint] = now
	return false
}
