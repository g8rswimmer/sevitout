// Package ws implements the real-time WebSocket layer: a room-per-SEV
// publish/subscribe hub and the HTTP handler that upgrades authenticated
// connections and bridges them to it.
package ws

import (
	"encoding/json"
	"sync"
)

// Event is the envelope broadcast to every client subscribed to a SEV's
// room. Payload is pre-marshaled JSON (see internal/api/grpc's publishProto/
// publishJSON) so this package never needs to know about protobuf types.
type Event struct {
	Type    string          `json:"type"`
	SEVID   string          `json:"sev_id"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// clientBufferSize bounds how many undelivered events a single client can
// queue before Publish starts dropping events for it. A slow or stalled
// consumer must never block delivery to other subscribers.
const clientBufferSize = 16

// BroadcastRoom is a well-known room ID a client can Subscribe to (e.g. via
// the WebSocket handler's ordinary ?sev_id= parameter) to receive every
// event published to any SEV's room, not just one. It exists for consumers
// that don't know SEV IDs in advance — chiefly the Slack bot (M11), which
// needs to react to "a SEV was just opened" before it has anything to
// subscribe to by ID.
const BroadcastRoom = "*"

// Client is a single WebSocket connection's outbound event queue. It is not
// bound to any particular room until passed to Hub.Subscribe. rooms is
// mutated only by Hub's methods, all of which hold Hub.mu — it never needs
// its own lock.
type Client struct {
	send  chan Event
	rooms map[string]struct{}
}

// Events returns the channel of events queued for delivery to this client.
// It is closed once the client is removed from the hub via Hub.Close.
func (c *Client) Events() <-chan Event {
	return c.send
}

// send enqueues evt for delivery, dropping it if the client's buffer is full
// rather than blocking the publisher.
func (c *Client) enqueue(evt Event) {
	select {
	case c.send <- evt:
	default:
	}
}

// Hub tracks which clients are subscribed to which SEV "rooms" and fans out
// published events to them. The zero value is not usable; use NewHub.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*Client]struct{}
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*Client]struct{})}
}

// NewClient returns a Client with its own outbound event queue, not yet
// subscribed to any room.
func (h *Hub) NewClient() *Client {
	return &Client{send: make(chan Event, clientBufferSize), rooms: make(map[string]struct{})}
}

// Subscribe adds c to sevID's room. Safe to call multiple times for
// different rooms with the same client.
func (h *Hub) Subscribe(c *Client, sevID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[sevID] == nil {
		h.rooms[sevID] = make(map[*Client]struct{})
	}
	h.rooms[sevID][c] = struct{}{}
	c.rooms[sevID] = struct{}{}
}

// Unsubscribe removes c from sevID's room. A no-op if c was not subscribed.
func (h *Hub) Unsubscribe(c *Client, sevID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeFromRoomLocked(c, sevID)
	delete(c.rooms, sevID)
}

// Close removes c from every room it belongs to and closes its event
// channel. Call exactly once, when the underlying connection is done.
//
// It only visits the rooms c.rooms records — not every room in the hub —
// so a single disconnect costs O(that client's subscriptions), not
// O(total active rooms).
func (h *Hub) Close(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sevID := range c.rooms {
		h.removeFromRoomLocked(c, sevID)
	}
	close(c.send)
}

// removeFromRoomLocked deletes c from sevID's room (not from c.rooms),
// pruning the room entirely once empty. Callers must hold h.mu.
func (h *Hub) removeFromRoomLocked(c *Client, sevID string) {
	room, ok := h.rooms[sevID]
	if !ok {
		return
	}
	delete(room, c)
	if len(room) == 0 {
		delete(h.rooms, sevID)
	}
}

// Publish fans evt out to every client currently subscribed to sevID, plus
// every client subscribed to BroadcastRoom. Publish never blocks on a slow
// client (see Client.enqueue) and is a no-op if sevID has no subscribers (and
// nobody is on the broadcast room).
//
// A client subscribed to both sevID and BroadcastRoom is only delivered the
// event once — the two room lookups aren't assumed disjoint, since a client
// is free to subscribe to both.
func (h *Hub) Publish(sevID, eventType string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	evt := Event{Type: eventType, SEVID: sevID, Payload: payload}
	for c := range h.rooms[sevID] {
		c.enqueue(evt)
	}
	if sevID == BroadcastRoom {
		return
	}
	for c := range h.rooms[BroadcastRoom] {
		if _, dup := h.rooms[sevID][c]; dup {
			continue
		}
		c.enqueue(evt)
	}
}
