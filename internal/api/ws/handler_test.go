package ws_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// testServer bundles everything needed to dial the WebSocket handler under test.
type testServer struct {
	hub    *ws.Hub
	signer *auth.JWTSigner
	users  *memory.UserStore
	srv    *httptest.Server
	wsURL  string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	hub := ws.NewHub()
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	users := memory.NewUserStore()

	srv := httptest.NewServer(ws.NewHandler(hub, signer, users))
	t.Cleanup(srv.Close)

	return &testServer{
		hub:    hub,
		signer: signer,
		users:  users,
		srv:    srv,
		wsURL:  "ws" + strings.TrimPrefix(srv.URL, "http"),
	}
}

func (ts *testServer) seedActiveUser(t *testing.T, id string) string {
	t.Helper()
	now := time.Now()
	u := &store.User{
		ID:        id,
		Email:     id + "@example.com",
		Name:      id,
		OrgRole:   store.OrgRoleViewer,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := ts.users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := ts.signer.Sign(u.ID, u.Email, string(u.OrgRole))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

// dial connects a WebSocket client with the given bearer token and optional
// initial sev_id subscriptions.
func (ts *testServer) dial(t *testing.T, token string, sevIDs ...string) *gorillaws.Conn {
	t.Helper()
	url := ts.wsURL
	if len(sevIDs) > 0 {
		url += "?sev_id=" + strings.Join(sevIDs, "&sev_id=")
	}
	header := make(map[string][]string)
	if token != "" {
		header["Authorization"] = []string{"Bearer " + token}
	}
	conn, resp, err := gorillaws.DefaultDialer.Dial(url, header)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readEvent(t *testing.T, conn *gorillaws.Conn) ws.Event {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var evt ws.Event
	if err := conn.ReadJSON(&evt); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	return evt
}

func assertNoMessage(t *testing.T, conn *gorillaws.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	var evt ws.Event
	err := conn.ReadJSON(&evt)
	if err == nil {
		t.Fatalf("unexpected event delivered: %+v", evt)
	}
	if !gorillaws.IsCloseError(err) && !isTimeout(err) {
		t.Fatalf("unexpected read error: %v", err)
	}
}

func isTimeout(err error) bool {
	type timeouter interface{ Timeout() bool }
	te, ok := err.(timeouter)
	return ok && te.Timeout()
}

func TestHandler_MissingToken_Rejected(t *testing.T) {
	ts := newTestServer(t)
	_, resp, err := gorillaws.DefaultDialer.Dial(ts.wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without a token")
	}
	if resp == nil || resp.StatusCode != 401 {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Errorf("status = %d, want 401", status)
	}
}

func TestHandler_InvalidToken_Rejected(t *testing.T) {
	ts := newTestServer(t)
	header := map[string][]string{"Authorization": {"Bearer not-a-real-token"}}
	_, resp, err := gorillaws.DefaultDialer.Dial(ts.wsURL, header)
	if err == nil {
		t.Fatal("expected dial to fail with an invalid token")
	}
	if resp == nil || resp.StatusCode != 401 {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestHandler_InactiveUser_Rejected(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now()
	u := &store.User{
		ID: "deactivated-1", Email: "d@example.com", Name: "D",
		OrgRole: store.OrgRoleViewer, Active: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	token, err := ts.signer.Sign(u.ID, u.Email, string(u.OrgRole))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	header := map[string][]string{"Authorization": {"Bearer " + token}}
	_, resp, err := gorillaws.DefaultDialer.Dial(ts.wsURL, header)
	if err == nil {
		t.Fatal("expected dial to fail for a deactivated user")
	}
	if resp == nil || resp.StatusCode != 401 {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestHandler_TwoClientsSameSEV_BothReceiveEvent(t *testing.T) {
	ts := newTestServer(t)
	token := ts.seedActiveUser(t, "user-1")

	connA := ts.dial(t, token, "SEV-2026-0001")
	connB := ts.dial(t, token, "SEV-2026-0001")

	// Give the read pumps time to register the initial subscription before publishing.
	time.Sleep(50 * time.Millisecond)
	ts.hub.Publish("SEV-2026-0001", "sev.updated", []byte(`{"id":"SEV-2026-0001"}`))

	evtA := readEvent(t, connA)
	evtB := readEvent(t, connB)
	if evtA.Type != "sev.updated" || evtA.SEVID != "SEV-2026-0001" {
		t.Errorf("client A got %+v", evtA)
	}
	if evtB.Type != "sev.updated" || evtB.SEVID != "SEV-2026-0001" {
		t.Errorf("client B got %+v", evtB)
	}
}

func TestHandler_DifferentSEV_NoEvent(t *testing.T) {
	ts := newTestServer(t)
	token := ts.seedActiveUser(t, "user-1")

	subscribed := ts.dial(t, token, "SEV-2026-0001")
	other := ts.dial(t, token, "SEV-2026-0002")

	time.Sleep(50 * time.Millisecond)
	ts.hub.Publish("SEV-2026-0001", "sev.updated", []byte(`{}`))

	evt := readEvent(t, subscribed)
	if evt.SEVID != "SEV-2026-0001" {
		t.Fatalf("subscribed client got wrong event: %+v", evt)
	}
	assertNoMessage(t, other)
}

func TestHandler_DynamicSubscribe_ReceivesAfterControlMessage(t *testing.T) {
	ts := newTestServer(t)
	token := ts.seedActiveUser(t, "user-1")

	conn := ts.dial(t, token) // no initial subscriptions

	if err := conn.WriteJSON(map[string]string{"action": "subscribe", "sev_id": "SEV-2026-0003"}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	ts.hub.Publish("SEV-2026-0003", "chat.created", []byte(`{}`))
	evt := readEvent(t, conn)
	if evt.SEVID != "SEV-2026-0003" {
		t.Errorf("got %+v, want sev_id=SEV-2026-0003", evt)
	}
}

func TestHandler_Unsubscribe_StopsReceiving(t *testing.T) {
	ts := newTestServer(t)
	token := ts.seedActiveUser(t, "user-1")

	conn := ts.dial(t, token, "SEV-2026-0004")
	if err := conn.WriteJSON(map[string]string{"action": "unsubscribe", "sev_id": "SEV-2026-0004"}); err != nil {
		t.Fatalf("write unsubscribe: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	ts.hub.Publish("SEV-2026-0004", "sev.updated", []byte(`{}`))
	assertNoMessage(t, conn)
}
