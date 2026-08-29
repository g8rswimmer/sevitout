package ws_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// waitForGaugeValue polls g every 10ms for up to 2s until it reads want —
// Handler.ServeHTTP updates telemetry.WSConnections from its own goroutine,
// asynchronously relative to a test's dial/close calls, so a bare assertion
// immediately after either would be racy.
func waitForGaugeValue(t *testing.T, g prometheus.Gauge, want float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := testutil.ToFloat64(g); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gauge did not reach %v within timeout (last value %v)", want, testutil.ToFloat64(g))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForGaugeStable polls g until it reads the same value on 5 consecutive
// polls (10ms apart) and returns that value. telemetry.WSConnections is a
// process-wide metric shared with every other test in this file — a prior
// test's t.Cleanup(func() { conn.Close() }) closes the client side
// immediately but doesn't wait for the server to notice and run its own
// Dec() (which only fires once readPump's blocking read finally errors), so
// capturing a "before" baseline without waiting for that straggler to
// settle first can under- or over-count it, making an exact before+1/before
// assertion flaky. This is that wait.
func waitForGaugeStable(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	const stableReadsRequired = 5
	deadline := time.Now().Add(3 * time.Second)
	last := testutil.ToFloat64(g)
	stableReads := 1
	for {
		time.Sleep(10 * time.Millisecond)
		v := testutil.ToFloat64(g)
		if v == last {
			stableReads++
			if stableReads >= stableReadsRequired {
				return v
			}
		} else {
			last = v
			stableReads = 1
		}
		if time.Now().After(deadline) {
			t.Fatalf("gauge never stabilized within timeout (last value %v)", last)
		}
	}
}

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

func TestHandler_InsufficientRole_Rejected(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now()
	// An OrgRole with no entry in rbac.go's roleLevel map is treated as
	// below Viewer by HasPermission — this is the only way to exercise the
	// RBAC-denial branch today, since every *known* OrgRole is at least
	// Viewer and would otherwise be allowed to subscribe.
	u := &store.User{
		ID: "no-role-1", Email: "no-role@example.com", Name: "No Role",
		OrgRole: store.OrgRole("unknown"), Active: true, CreatedAt: now, UpdatedAt: now,
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
		t.Fatal("expected dial to fail for a role below Viewer")
	}
	if resp == nil || resp.StatusCode != 403 {
		t.Errorf("status = %v, want 403", resp)
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

func TestHandler_MalformedControlFrame_ConnectionSurvives(t *testing.T) {
	ts := newTestServer(t)
	token := ts.seedActiveUser(t, "user-1")

	conn := ts.dial(t, token, "SEV-2026-0005")

	// sev_id should be a string; this fails to decode into controlMessage,
	// but the underlying connection is still healthy and must not be torn
	// down over it.
	if err := conn.WriteJSON(map[string]any{"action": "subscribe", "sev_id": 123}); err != nil {
		t.Fatalf("write malformed frame: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	ts.hub.Publish("SEV-2026-0005", "sev.updated", []byte(`{}`))
	evt := readEvent(t, conn)
	if evt.SEVID != "SEV-2026-0005" {
		t.Errorf("connection should have survived the malformed frame and kept its original subscription, got %+v", evt)
	}
}

func TestHandler_WSConnectionsGauge_IncDecOnConnectDisconnect(t *testing.T) {
	ts := newTestServer(t)
	token := ts.seedActiveUser(t, "user-1")
	before := waitForGaugeStable(t, telemetry.WSConnections)

	conn := ts.dial(t, token)
	waitForGaugeValue(t, telemetry.WSConnections, before+1)

	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitForGaugeValue(t, telemetry.WSConnections, before)
}

func TestHandler_WSConnectionsGauge_RejectedConnectionNotCounted(t *testing.T) {
	ts := newTestServer(t)
	before := waitForGaugeStable(t, telemetry.WSConnections)

	req := httptest.NewRequest("GET", ts.srv.URL, nil) // no bearer token
	w := httptest.NewRecorder()
	ws.NewHandler(ts.hub, ts.signer, ts.users).ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}

	if got := testutil.ToFloat64(telemetry.WSConnections); got != before {
		t.Errorf("WSConnections = %v, want %v (a rejected connection should never increment it)", got, before)
	}
}
