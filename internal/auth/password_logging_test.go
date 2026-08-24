package auth_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// withCapturedDefaultLog temporarily installs a JSON slog.Logger as the
// package-level default (what password.go's slog.InfoContext/WarnContext
// calls actually write to in production, via main.go's slog.SetDefault),
// restoring the previous default when the test ends.
func withCapturedDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func lastLogFields(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatalf("no log output, buf=%q", buf.String())
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &fields); err != nil {
		t.Fatalf("log line is not valid JSON: %v, line=%q", err, lines[len(lines)-1])
	}
	return fields
}

func TestLogin_Success_LogsInfo(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	h, users := newTestHandler(t)
	mux := registerMux(h)
	doPost(t, mux, "/auth/register", map[string]string{
		"email": "alice@example.com", "name": "Alice", "password": "password123",
	})
	_ = users

	doPost(t, mux, "/auth/login", map[string]string{
		"email": "alice@example.com", "password": "password123",
	})

	fields := lastLogFields(t, buf)
	if fields["msg"] != "user logged in" {
		t.Errorf("msg = %v, want %q", fields["msg"], "user logged in")
	}
	if fields["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", fields["level"])
	}
	if fields["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", fields["email"])
	}
}

func TestLogin_WrongPassword_LogsWarnWithoutPassword(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	h, _ := newTestHandler(t)
	mux := registerMux(h)
	doPost(t, mux, "/auth/register", map[string]string{
		"email": "bob@example.com", "name": "Bob", "password": "correct-password",
	})

	doPost(t, mux, "/auth/login", map[string]string{
		"email": "bob@example.com", "password": "wrong-password",
	})

	fields := lastLogFields(t, buf)
	if fields["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", fields["level"])
	}
	if strings.Contains(buf.String(), "wrong-password") {
		t.Error("log output must never contain the attempted password")
	}
}

func TestLogin_UnknownEmail_LogsWarn(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	doPost(t, mux, "/auth/login", map[string]string{
		"email": "nobody@example.com", "password": "whatever123",
	})

	fields := lastLogFields(t, buf)
	if fields["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", fields["level"])
	}
	if fields["msg"] != "login failed: unknown email" {
		t.Errorf("msg = %v, want %q", fields["msg"], "login failed: unknown email")
	}
}

func TestRegister_Success_LogsInfoWithOrgRole(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	h, _ := newTestHandler(t)
	mux := registerMux(h)

	doPost(t, mux, "/auth/register", map[string]string{
		"email": "first@example.com", "name": "First", "password": "password123",
	})

	fields := lastLogFields(t, buf)
	if fields["msg"] != "user registered" {
		t.Errorf("msg = %v, want %q", fields["msg"], "user registered")
	}
	if fields["org_role"] != "admin" {
		t.Errorf("org_role = %v, want admin (first registered user)", fields["org_role"])
	}
}
