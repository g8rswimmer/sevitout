package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// lastLogLine returns the fields of the last JSON log line written to buf,
// or fails the test if buf holds no valid JSON line.
func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
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

func TestLoggingMiddleware_MintsRequestIDWhenNoneSupplied(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(log, "ws", next).ServeHTTP(w, req)

	respID := w.Header().Get("X-Request-Id")
	if respID == "" {
		t.Fatal("response X-Request-Id header is empty — middleware didn't mint or echo one")
	}

	fields := lastLogLine(t, &buf)
	if fields["request_id"] != respID {
		t.Errorf("logged request_id = %v, want %v (should match the echoed response header)", fields["request_id"], respID)
	}
}

func TestLoggingMiddleware_ReusesCallerSuppliedRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("X-Request-Id", "caller-supplied-id")
	w := httptest.NewRecorder()
	loggingMiddleware(log, "ws", next).ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-Id"); got != "caller-supplied-id" {
		t.Errorf("response X-Request-Id = %q, want %q (caller-supplied value should be reused, not overwritten)", got, "caller-supplied-id")
	}

	fields := lastLogLine(t, &buf)
	if fields["request_id"] != "caller-supplied-id" {
		t.Errorf("logged request_id = %v, want caller-supplied-id", fields["request_id"])
	}
}

func TestLoggingMiddleware_BindsRetrievableLoggerIntoContext(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	var gotFromCtx bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		telemetry.LoggerFromContext(r.Context()).Info("handler-internal event")
		gotFromCtx = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/s/some-token", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(log, "share-view", next).ServeHTTP(w, req)

	if !gotFromCtx {
		t.Fatal("handler never ran")
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2 (handler's own + the middleware's access-log line): %q", len(lines), buf.String())
	}
	var handlerLine map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &handlerLine); err != nil {
		t.Fatalf("handler's log line is not valid JSON: %v", err)
	}
	if handlerLine["msg"] != "handler-internal event" {
		t.Fatalf("first log line msg = %v, want %q", handlerLine["msg"], "handler-internal event")
	}
	if _, ok := handlerLine["request_id"]; !ok {
		t.Error("handler's logger has no request_id bound — telemetry.LoggerFromContext isn't seeing loggingMiddleware's bound logger")
	}
}

func TestLoggingMiddleware_LogsMethodPathStatusDuration(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })

	req := httptest.NewRequest("GET", "/admin/integrations/health", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(log, "integrations-health", next).ServeHTTP(w, req)

	fields := lastLogLine(t, &buf)
	if fields["handler"] != "integrations-health" {
		t.Errorf("handler = %v, want integrations-health", fields["handler"])
	}
	if fields["method"] != "GET" {
		t.Errorf("method = %v, want GET", fields["method"])
	}
	if fields["path"] != "/admin/integrations/health" {
		t.Errorf("path = %v, want /admin/integrations/health", fields["path"])
	}
	if fields["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want %d", fields["status"], http.StatusNotFound)
	}
	if _, ok := fields["duration_ms"]; !ok {
		t.Error("duration_ms field missing")
	}
}
