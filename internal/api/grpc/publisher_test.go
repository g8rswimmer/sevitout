package grpc_test

import "sync"

// publishedEvent records one Publisher.Publish call, captured verbatim so
// tests can assert on both the routing (sevID/eventType) and the payload
// bytes actually sent to WebSocket subscribers.
type publishedEvent struct {
	sevID     string
	eventType string
	payload   []byte
}

// fakePublisher is a grpchandler.Publisher that records every call instead
// of fanning out over a real Hub, so handler tests can assert exactly what
// would have been broadcast.
type fakePublisher struct {
	mu     sync.Mutex
	events []publishedEvent
}

func (f *fakePublisher) Publish(sevID, eventType string, payload []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, publishedEvent{sevID: sevID, eventType: eventType, payload: payload})
}

// All returns a snapshot of every event published so far.
func (f *fakePublisher) All() []publishedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]publishedEvent, len(f.events))
	copy(out, f.events)
	return out
}
