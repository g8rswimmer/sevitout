// Package email wraps a minimal SMTP client for sending plain-text
// notification emails (docs/requirements.md §16, docs/roadmap.md Phase 15).
// Standard library only (net/smtp + crypto/tls for STARTTLS) — no new go.mod
// dependency, matching this codebase's other integration clients
// (pagerduty/github/jira wrap plain net/http; slack wraps a single small
// SDK).
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Client sends email through one SMTP server using an optional
// username/password (PLAIN auth) and STARTTLS when the server advertises it.
type Client struct {
	host, username, password, from string
	port                           int
}

// NewClient returns a Client that authenticates as username/password (empty
// for an unauthenticated relay) against host:port, sending as from.
func NewClient(host string, port int, username, password, from string) *Client {
	return &Client{host: host, port: port, username: username, password: password, from: from}
}

// Send delivers a plain-text email to to. Dialing honors ctx's deadline/
// cancellation; the SMTP conversation itself (net/smtp has no context-aware
// API) does not, matching this package's minimal scope.
func (c *Client) Send(ctx context.Context, to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, c.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("email: new client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: c.host}); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}

	if c.username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", c.username, c.password, c.host)); err != nil {
				return fmt.Errorf("email: auth: %w", err)
			}
		}
	}

	if err := client.Mail(c.from); err != nil {
		return fmt.Errorf("email: mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	if _, err := w.Write(buildMessage(c.from, to, subject, body)); err != nil {
		_ = w.Close()
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}
	return client.Quit()
}

// buildMessage builds a minimal RFC 5322 plain-text message.
func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
