package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
)

// wsReconnectDelay is how long runEventListener waits before retrying a
// dropped or failed WebSocket connection.
const wsReconnectDelay = 5 * time.Second

// runEventListener connects to the API server's WebSocket endpoint
// (subscribed to ws.BroadcastRoom, so it hears about every SEV — including
// ones it has no prior reason to know the ID of, like a brand new SEV-1
// open) and dispatches each event to b.handleEvent. It reconnects on any
// error until ctx is canceled — this is the bot's only source of the
// event-driven triggers M11 depends on M09 for (docs/project-plan.md).
func (b *bot) runEventListener(ctx context.Context, wsURL, token string) {
	for ctx.Err() == nil {
		if err := b.connectAndListen(ctx, wsURL, token); err != nil && ctx.Err() == nil {
			b.log.ErrorContext(ctx, "event stream connection lost, reconnecting", "err", err, "retry_in", wsReconnectDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wsReconnectDelay):
		}
	}
}

// connectAndListen opens one WebSocket connection and reads from it until
// the connection errors or ctx is canceled.
func (b *bot) connectAndListen(ctx context.Context, wsURL, token string) error {
	header := http.Header{"Authorization": {"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	b.log.InfoContext(ctx, "connected to event stream", "url", wsURL)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		var evt ws.Event
		if err := json.Unmarshal(data, &evt); err != nil {
			b.log.ErrorContext(ctx, "decode event failed", "err", err)
			continue
		}
		b.handleEvent(ctx, evt)
	}
}
