// Package mailer sends plain-text mail through an authenticated SMTP server.
package mailer

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/domehahn/housekeeping/internal/config"
)

// Sender is deliberately small so the CLI can be tested without a live SMTP
// server.
type Sender interface {
	Send(ctx context.Context, cfg config.SMTPConfig, password string, recipients []string, subject, body string) error
}

type SMTP struct{}

// Send sends one message with all recipients in BCC. The SMTP configuration
// uses STARTTLS; the server must advertise STARTTLS when it is not localhost.
func (SMTP) Send(ctx context.Context, cfg config.SMTPConfig, password string, recipients []string, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(recipients) == 0 {
		return fmt.Errorf("mailer: no recipients")
	}
	from, err := address(cfg.From)
	if err != nil {
		return fmt.Errorf("mailer: invalid from address: %w", err)
	}
	for _, recipient := range recipients {
		if _, err := address(recipient); err != nil {
			return fmt.Errorf("mailer: invalid recipient %q: %w", recipient, err)
		}
	}
	if strings.TrimSpace(subject) == "" {
		return fmt.Errorf("mailer: subject must not be empty")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("mailer: subject must not contain line breaks")
	}

	message := []byte("From: " + from + "\r\n" +
		"To: undisclosed-recipients:;\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + body + "\r\n")

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mailer: connect SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	client := &connection{conn: conn, reader: textproto.NewReader(reader), writer: textproto.NewWriter(writer), buffer: writer}
	if err := client.expect(220); err != nil {
		return fmt.Errorf("mailer: SMTP greeting: %w", err)
	}
	if err := client.command("EHLO housekeeping", 250); err != nil {
		return fmt.Errorf("mailer: EHLO: %w", err)
	}
	if err := client.command("STARTTLS", 220); err != nil {
		return fmt.Errorf("mailer: STARTTLS: %w", err)
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("mailer: start TLS: %w", err)
	}
	reader = bufio.NewReader(tlsConn)
	writer = bufio.NewWriter(tlsConn)
	client = &connection{conn: tlsConn, reader: textproto.NewReader(reader), writer: textproto.NewWriter(writer), buffer: writer}
	if err := client.command("EHLO housekeeping", 250); err != nil {
		return fmt.Errorf("mailer: EHLO after STARTTLS: %w", err)
	}
	if cfg.Username != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte("\x00" + cfg.Username + "\x00" + password))
		if err := client.command("AUTH PLAIN "+credentials, 235); err != nil {
			return fmt.Errorf("mailer: authenticate: %w", err)
		}
	}
	if err := client.command("MAIL FROM:<"+from+">", 250); err != nil {
		return fmt.Errorf("mailer: set sender: %w", err)
	}
	for _, recipient := range recipients {
		parsed, _ := address(recipient)
		if err := client.command("RCPT TO:<"+parsed+">", 250); err != nil {
			return fmt.Errorf("mailer: set recipient %s: %w", parsed, err)
		}
	}
	if err := client.command("DATA", 354); err != nil {
		return fmt.Errorf("mailer: open message: %w", err)
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(message), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		if err := client.writer.PrintfLine("%s", line); err != nil {
			return fmt.Errorf("mailer: write message: %w", err)
		}
	}
	if err := client.writer.PrintfLine("."); err != nil {
		return fmt.Errorf("mailer: write message: %w", err)
	}
	if err := client.buffer.Flush(); err != nil {
		return fmt.Errorf("mailer: flush message: %w", err)
	}
	if err := client.expect(250); err != nil {
		return fmt.Errorf("mailer: finalize message: %w", err)
	}
	_ = client.command("QUIT", 221)
	return nil
}

// Small SMTP client kept local because net/smtp was removed from the Go
// standard library in Go 1.26.
type connection struct {
	conn   net.Conn
	reader *textproto.Reader
	writer *textproto.Writer
	buffer *bufio.Writer
}

func (c *connection) command(command string, expected int) error {
	if err := c.writer.PrintfLine("%s", command); err != nil {
		return err
	}
	if err := c.buffer.Flush(); err != nil {
		return err
	}
	return c.expect(expected)
}

func (c *connection) expect(expected int) error {
	code, message, err := c.reader.ReadResponse(expected)
	if err != nil {
		return err
	}
	if code != expected {
		return fmt.Errorf("unexpected SMTP response %d: %s", code, message)
	}
	return nil
}

func address(raw string) (string, error) {
	a, err := mail.ParseAddress(raw)
	if err != nil || a.Address == "" {
		if err == nil {
			err = fmt.Errorf("address is empty")
		}
		return "", err
	}
	return a.Address, nil
}
