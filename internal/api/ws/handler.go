package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// upgrader has no origin restriction: sevitout is a self-hosted, single-org
// tool served behind its own reverse proxy/ingress, matching the REST
// gateway's lack of CORS restriction elsewhere in this repo.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Keepalive timers. Declared as vars (not consts) so package-internal tests
// can shrink them to exercise the timeout path without waiting a minute.
var (
	// pongWait is how long a connection may go without any read-side
	// activity (a client pong, or any other frame) before readPump gives up
	// on it and tears the connection down. Without this, a peer that
	// vanishes without a clean WebSocket close (lost network, laptop sleep,
	// a NAT/LB silently dropping the mapping) would block readPump forever,
	// leaking the client's hub registration and goroutines indefinitely.
	pongWait = 60 * time.Second
	// pingPeriod must be well under pongWait so at least one ping/pong
	// round-trip completes inside each read-deadline window.
	pingPeriod = (pongWait * 9) / 10
	// writeWait bounds how long a single outbound write may block, so a
	// stalled write can't hang writePump indefinitely either.
	writeWait = 10 * time.Second
)

// wsSubscribeMethod is a pseudo gRPC-method path used only to look up the
// minimum org role required to open a WebSocket connection, via the same
// rbac.go table every real RPC is checked against — WebSocket connections
// bypass the gRPC interceptor entirely, so this is how they still get an
// RBAC floor instead of skipping the check altogether.
const wsSubscribeMethod = "/sevitout.v1.WebSocket/Subscribe"

// tokenValidator validates a signed JWT and returns its claims. Implemented
// by *auth.JWTSigner; declared here (the consumer) so this package depends
// only on the behavior it needs.
type tokenValidator interface {
	Validate(tokenStr string) (*auth.Claims, error)
}

// Handler upgrades authenticated HTTP requests to WebSocket connections and
// bridges them to a Hub.
type Handler struct {
	hub    *Hub
	signer tokenValidator
	users  store.UserStore
}

// NewHandler returns a Handler serving connections against hub. Token
// validation and the active-user check mirror internal/auth's gRPC
// interceptor so a WebSocket connection has the same authentication bar as
// any API call.
func NewHandler(hub *Hub, signer tokenValidator, users store.UserStore) *Handler {
	return &Handler{hub: hub, signer: signer, users: users}
}

// controlMessage is a client->server frame used to change room subscriptions
// after the connection is established (initial rooms come from the ?sev_id=
// query parameter).
type controlMessage struct {
	Action string `json:"action"` // "subscribe" | "unsubscribe"
	SEVID  string `json:"sev_id"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := auth.ExtractBearerToken(r)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	claims, err := h.signer.Validate(token)
	if err != nil {
		slog.WarnContext(r.Context(), "websocket connection rejected: invalid token", "err", err)
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	user, err := h.users.Get(r.Context(), claims.Subject)
	if err != nil || !user.Active {
		slog.WarnContext(r.Context(), "websocket connection rejected: unknown or inactive user", "user_id", claims.Subject)
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	if !auth.HasPermission(user.OrgRole, wsSubscribeMethod) {
		slog.WarnContext(r.Context(), "websocket connection rejected: insufficient permissions", "user_id", user.ID, "org_role", string(user.OrgRole))
		http.Error(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.WarnContext(r.Context(), "websocket upgrade failed", "user_id", user.ID, "err", err)
		return
	}

	client := h.hub.NewClient()
	sevIDs := r.URL.Query()["sev_id"]
	for _, sevID := range sevIDs {
		if sevID != "" {
			h.hub.Subscribe(client, sevID)
		}
	}
	slog.InfoContext(r.Context(), "websocket connected", "user_id", user.ID, "sev_ids", sevIDs)
	telemetry.WSConnections.Inc()

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		writePump(conn, client)
	}()

	readPump(conn, h.hub, client)

	_ = conn.Close()
	h.hub.Close(client)
	<-writeDone
	telemetry.WSConnections.Dec()
	slog.InfoContext(r.Context(), "websocket disconnected", "user_id", user.ID)
}

// writePump delivers queued events to conn, interleaved with periodic pings
// to drive readPump's read-deadline extension on the other end of this same
// connection. It returns when the client's channel is closed (by Hub.Close)
// or a write fails — and on a write failure it closes conn itself, which is
// what unblocks a readPump that's parked on a read with no data arriving
// (a write-side-only failure would otherwise never surface on the read side).
func writePump(conn *websocket.Conn, client *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case evt, ok := <-client.Events():
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteJSON(evt); err != nil {
				_ = conn.Close()
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

// readPump processes subscribe/unsubscribe control frames until the
// connection errors, times out (see pongWait), or closes. A malformed
// frame only fails to decode — the connection itself is still healthy — so
// it's logged as ignored rather than tearing down every other subscription
// this client holds.
func readPump(conn *websocket.Conn, hub *Hub, client *Client) {
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg controlMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Debug("websocket control frame ignored: malformed", "err", err)
			continue
		}
		if msg.SEVID == "" {
			continue
		}
		switch msg.Action {
		case "subscribe":
			hub.Subscribe(client, msg.SEVID)
		case "unsubscribe":
			hub.Unsubscribe(client, msg.SEVID)
		}
	}
}
