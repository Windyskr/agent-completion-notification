// Package email 通过加密 SMTP 发送任务完成通知。
package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/windyskr/agent-completion-notification/internal/config"
	"github.com/windyskr/agent-completion-notification/internal/event"
)

type Notifier struct {
	cfg config.Email
	now func() time.Time
}

func New(cfg config.Email) *Notifier {
	return &Notifier{cfg: cfg, now: time.Now}
}

func (n *Notifier) Name() string { return "email" }

func (n *Notifier) Send(ctx context.Context, ev event.Event) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(n.cfg.SMTPAddress))
	if err != nil || host == "" {
		return fmt.Errorf("SMTP 地址格式无效，应为 host:port")
	}
	from, err := mail.ParseAddress(strings.TrimSpace(n.cfg.From))
	if err != nil {
		return fmt.Errorf("发件人地址无效")
	}
	recipients, err := mail.ParseAddressList(strings.TrimSpace(n.cfg.To))
	if err != nil || len(recipients) == 0 {
		return fmt.Errorf("收件人地址无效")
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	var conn net.Conn
	if port == "465" {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", n.cfg.SMTPAddress)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", n.cfg.SMTPAddress)
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("建立 SMTP 会话失败: %w", err)
	}
	defer client.Close()
	if port != "465" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP 服务器不支持 STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP STARTTLS 失败: %w", err)
		}
	}

	username := strings.TrimSpace(n.cfg.Username)
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, n.cfg.Password, host)); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient.Address); err != nil {
			return fmt.Errorf("设置收件人失败: %w", err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("创建邮件正文失败: %w", err)
	}
	message := buildMessage(from, recipients, ev.Title(), ev.Body(n.now()))
	if _, err := io.Copy(w, strings.NewReader(message)); err != nil {
		_ = w.Close()
		return fmt.Errorf("发送邮件正文失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("结束 SMTP 会话失败: %w", err)
	}
	return nil
}

func buildMessage(from *mail.Address, recipients []*mail.Address, subject, body string) string {
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(subject)
	to := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		to = append(to, recipient.String())
	}
	var b strings.Builder
	w := bufio.NewWriter(&b)
	fmt.Fprintf(w, "From: %s\r\n", from.String())
	fmt.Fprintf(w, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(w, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprint(w, "MIME-Version: 1.0\r\n")
	fmt.Fprint(w, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprint(w, "Content-Transfer-Encoding: 8bit\r\n\r\n")
	fmt.Fprint(w, strings.ReplaceAll(body, "\n", "\r\n"))
	_ = w.Flush()
	return b.String()
}
