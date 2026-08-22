package ws

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// upgrader has no origin restriction: sevitout is a self-hosted, single-org
// tool served behind its own reverse proxy/ingress, matching the REST
// gateway's lack of CORS restriction elsewhere in this repo.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

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
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	claims, err := h.signer.Validate(token)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}
	user, err := h.users.Get(r.Context(), claims.Subject)
	if err != nil || !user.Active {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := h.hub.NewClient()
	for _, sevID := range r.URL.Query()["sev_id"] {
		if sevID != "" {
			h.hub.Subscribe(client, sevID)
		}
	}

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		writePump(conn, client)
	}()

	readPump(conn, h.hub, client)

	_ = conn.Close()
	h.hub.Close(client)
	<-writeDone
}

// writePump delivers queued events to conn until the client's channel is
// closed (by Hub.Close) or a write fails.
func writePump(conn *websocket.Conn, client *Client) {
	for evt := range client.Events() {
		if err := conn.WriteJSON(evt); err != nil {
			return
		}
	}
}

// readPump processes subscribe/unsubscribe control frames until the
// connection errors or closes.
func readPump(conn *websocket.Conn, hub *Hub, client *Client) {
	for {
		var msg controlMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
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

// extractToken mirrors the REST gateway's precedence (see cmd/server/main.go's
// runtime.WithMetadata): an Authorization header takes priority over the
// "token" httpOnly cookie set on login.
func extractToken(r *http.Request) string {
	if v := r.Header.Get("Authorization"); v != "" {
		after, ok := strings.CutPrefix(v, "Bearer ")
		if !ok {
			return ""
		}
		return after
	}
	if c, err := r.Cookie("token"); err == nil {
		return c.Value
	}
	return ""
}
