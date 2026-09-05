package email

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTPServer is a minimal single-connection SMTP responder — no
// STARTTLS/AUTH advertised, so Client.Send exercises the plain path without
// needing a TLS handshake in the test. rejectRcpt, when true, responds to
// RCPT TO with a permanent failure so Send's error-wrapping can be exercised.
type fakeSMTPServer struct {
	ln         net.Listener
	rejectRcpt bool
	gotBody    string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTPServer{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTPServer) addr() (string, int) {
	tcpAddr := s.ln.Addr().(*net.TCPAddr)
	return tcpAddr.IP.String(), tcpAddr.Port
}

// serveOne handles exactly one connection then returns. Run in a goroutine.
func (s *fakeSMTPServer) serveOne(t *testing.T) {
	t.Helper()
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	w := func(line string) {
		if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
			t.Errorf("write: %v", err)
		}
	}
	r := bufio.NewReader(conn)

	w("220 fake.smtp ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			w("250 fake.smtp")
		case strings.HasPrefix(upper, "MAIL FROM"):
			w("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			if s.rejectRcpt {
				w("550 no such user")
			} else {
				w("250 OK")
			}
		case strings.HasPrefix(upper, "DATA"):
			w("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				body.WriteString(dataLine)
			}
			s.gotBody = body.String()
			w("250 OK: queued")
		case strings.HasPrefix(upper, "QUIT"):
			w("221 Bye")
			return
		default:
			w("500 unrecognized command")
		}
	}
}

func TestClient_Send(t *testing.T) {
	srv := newFakeSMTPServer(t)
	done := make(chan struct{})
	go func() { defer close(done); srv.serveOne(t) }()

	host, port := srv.addr()
	c := NewClient(host, port, "", "", "sevitout@example.com")
	if err := c.Send(context.Background(), "oncall@example.com", "SEV-1 opened", "checkout is down"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-done

	if !strings.Contains(srv.gotBody, "Subject: SEV-1 opened") {
		t.Errorf("body missing subject header, got: %q", srv.gotBody)
	}
	if !strings.Contains(srv.gotBody, "checkout is down") {
		t.Errorf("body missing message content, got: %q", srv.gotBody)
	}
	if !strings.Contains(srv.gotBody, "To: oncall@example.com") {
		t.Errorf("body missing To header, got: %q", srv.gotBody)
	}
}

func TestClient_Send_RcptRejected(t *testing.T) {
	srv := newFakeSMTPServer(t)
	srv.rejectRcpt = true
	go srv.serveOne(t)

	host, port := srv.addr()
	c := NewClient(host, port, "", "", "sevitout@example.com")
	err := c.Send(context.Background(), "nobody@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected an error when the server rejects RCPT TO, got nil")
	}
	if !strings.Contains(err.Error(), "rcpt to") {
		t.Errorf("expected error to be wrapped with rcpt-to context, got: %v", err)
	}
}

func TestClient_Send_DialError(t *testing.T) {
	// Port 0 with no listener behind it (closed immediately) — a connection
	// attempt should fail fast rather than hang.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tcpAddr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()

	c := NewClient(tcpAddr.IP.String(), tcpAddr.Port, "", "", "sevitout@example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Send(ctx, "to@example.com", "subject", "body"); err == nil {
		t.Fatal("expected a dial error against a closed port, got nil")
	}
}
