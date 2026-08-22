package ws

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// TestServeHTTP_IdlePeerIsRemovedFromHub is a white-box (package ws) test of
// the fix for the "no read deadline / keepalive" finding: a peer that stops
// responding (never reads, so it never auto-replies to the server's pings —
// simulating a hung client or a silently-dropped connection) must eventually
// be torn down and removed from the hub rather than leaking forever.
//
// Timers are shrunk package-wide for the duration of this test so it
// completes in under a second instead of the real ~60s pongWait.
func TestServeHTTP_IdlePeerIsRemovedFromHub(t *testing.T) {
	origPong, origPing, origWrite := pongWait, pingPeriod, writeWait
	pongWait, pingPeriod, writeWait = 300*time.Millisecond, 80*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { pongWait, pingPeriod, writeWait = origPong, origPing, origWrite })

	hub := NewHub()
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	users := memory.NewUserStore()

	now := time.Now()
	u := &store.User{
		ID: "user-1", Email: "user-1@example.com", Name: "user-1",
		OrgRole: store.OrgRoleViewer, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := signer.Sign(u.ID, u.Email, string(u.OrgRole))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	srv := httptest.NewServer(NewHandler(hub, signer, users))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?sev_id=SEV-1"

	header := map[string][]string{"Authorization": {"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Deliberately never call conn.ReadMessage() again from here on: gorilla
	// only auto-replies to a Ping with a Pong while a read is in progress, so
	// a client that stops reading behaves, from the server's perspective,
	// exactly like a connection that silently died mid-session.

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		_, stillSubscribed := hub.rooms["SEV-1"]
		hub.mu.RUnlock()
		if !stillSubscribed {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("idle client was never removed from the hub — readPump appears to have blocked forever")
}
