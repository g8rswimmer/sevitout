package ws_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
)

func TestHub_SlowConsumer_LogsDroppedEventAsWarn(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")

	// The client's buffer is small (see hub.go's clientBufferSize) and never
	// drained here, so this must overflow it and trigger at least one drop.
	for i := 0; i < 100; i++ {
		hub.Publish("SEV-1", "sev.updated", nil)
	}

	found := false
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			continue
		}
		if fields["msg"] == "websocket event dropped: client buffer full" && fields["level"] == "WARN" {
			found = true
			if fields["sev_id"] != "SEV-1" {
				t.Errorf("sev_id = %v, want SEV-1", fields["sev_id"])
			}
			break
		}
	}
	if !found {
		t.Errorf("expected a Warn log line for a dropped event, got:\n%s", buf.String())
	}
}
