package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/integrations/slack"
)

// newTestServer registers one Slack Web API method (e.g. "conversations.create")
// on a fresh mux, returning statusCode and body as its JSON response, and
// returns a Client pointed at it. Slack's client appends the method name
// directly onto its configured API URL, so the base URL must end in "/".
func newTestServer(t *testing.T, method string, statusCode int, body any) *slack.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+method, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return slack.NewClientWithBaseURL("test-token", srv.URL+"/")
}

func TestCreateChannel_Success(t *testing.T) {
	c := newTestServer(t, "conversations.create", http.StatusOK, map[string]any{
		"ok":      true,
		"channel": map[string]any{"id": "C123", "name": "inc-sev1-2026-0001"},
	})
	id, err := c.CreateChannel(context.Background(), "inc-sev1-2026-0001")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if id != "C123" {
		t.Errorf("id = %q, want C123", id)
	}
}

func TestCreateChannel_Error(t *testing.T) {
	c := newTestServer(t, "conversations.create", http.StatusOK, map[string]any{
		"ok": false, "error": "name_taken",
	})
	if _, err := c.CreateChannel(context.Background(), "dup"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestInviteUsers_NoopOnEmpty(t *testing.T) {
	// No server registered for conversations.invite — if InviteUsers made a
	// call, the request would fail with connection refused.
	c := slack.NewClientWithBaseURL("test-token", "http://127.0.0.1:1/")
	if err := c.InviteUsers(context.Background(), "C123", nil); err != nil {
		t.Errorf("InviteUsers with no users = %v, want nil (no-op)", err)
	}
}

func TestInviteUsers_Success(t *testing.T) {
	c := newTestServer(t, "conversations.invite", http.StatusOK, map[string]any{
		"ok":      true,
		"channel": map[string]any{"id": "C123"},
	})
	if err := c.InviteUsers(context.Background(), "C123", []string{"U1", "U2"}); err != nil {
		t.Errorf("InviteUsers: %v", err)
	}
}

func TestInviteUsers_Error(t *testing.T) {
	c := newTestServer(t, "conversations.invite", http.StatusOK, map[string]any{
		"ok": false, "error": "channel_not_found",
	})
	if err := c.InviteUsers(context.Background(), "C123", []string{"U1"}); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPostMessage_Success(t *testing.T) {
	c := newTestServer(t, "chat.postMessage", http.StatusOK, map[string]any{
		"ok": true, "channel": "C123", "ts": "1111.2222",
	})
	if err := c.PostMessage(context.Background(), "C123", "hello"); err != nil {
		t.Errorf("PostMessage: %v", err)
	}
}

func TestPostMessage_Error(t *testing.T) {
	c := newTestServer(t, "chat.postMessage", http.StatusOK, map[string]any{
		"ok": false, "error": "not_in_channel",
	})
	if err := c.PostMessage(context.Background(), "C123", "hello"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestFetchHistory_ReturnsChronologicalOrder(t *testing.T) {
	// Slack returns newest-first; FetchHistory must reverse it.
	c := newTestServer(t, "conversations.history", http.StatusOK, map[string]any{
		"ok": true,
		"messages": []map[string]any{
			{"user": "U2", "text": "second message", "ts": "222.0"},
			{"user": "U1", "text": "first message", "ts": "111.0"},
		},
	})
	msgs, err := c.FetchHistory(context.Background(), "C123", 10)
	if err != nil {
		t.Fatalf("FetchHistory: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Text != "first message" || msgs[1].Text != "second message" {
		t.Errorf("messages not in chronological order: %+v", msgs)
	}
}

func TestLookupUserIDByEmail_Found(t *testing.T) {
	c := newTestServer(t, "users.lookupByEmail", http.StatusOK, map[string]any{
		"ok":   true,
		"user": map[string]any{"id": "U1", "name": "alice"},
	})
	id, err := c.LookupUserIDByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("LookupUserIDByEmail: %v", err)
	}
	if id != "U1" {
		t.Errorf("id = %q, want U1", id)
	}
}

func TestLookupUserIDByEmail_NotFoundIsNotAnError(t *testing.T) {
	c := newTestServer(t, "users.lookupByEmail", http.StatusOK, map[string]any{
		"ok": false, "error": "users_not_found",
	})
	id, err := c.LookupUserIDByEmail(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("LookupUserIDByEmail: %v, want nil error for no match", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty", id)
	}
}

func TestLookupUserIDByEmail_OtherErrorPropagates(t *testing.T) {
	c := newTestServer(t, "users.lookupByEmail", http.StatusOK, map[string]any{
		"ok": false, "error": "invalid_auth",
	})
	if _, err := c.LookupUserIDByEmail(context.Background(), "alice@example.com"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPing_Success(t *testing.T) {
	c := newTestServer(t, "auth.test", http.StatusOK, map[string]any{
		"ok": true, "user_id": "U1", "team": "T1",
	})
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestPing_Error(t *testing.T) {
	c := newTestServer(t, "auth.test", http.StatusOK, map[string]any{
		"ok": false, "error": "invalid_auth",
	})
	if err := c.Ping(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}
