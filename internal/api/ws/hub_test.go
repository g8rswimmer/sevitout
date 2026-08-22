package ws_test

import (
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
)

// recv waits briefly for an event on c and fails the test if none arrives.
func recv(t *testing.T, c *ws.Client) ws.Event {
	t.Helper()
	select {
	case evt, ok := <-c.Events():
		if !ok {
			t.Fatal("client channel closed before an event arrived")
		}
		return evt
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return ws.Event{}
	}
}

// assertNoEvent fails the test if an event arrives on c within a short window.
func assertNoEvent(t *testing.T, c *ws.Client) {
	t.Helper()
	select {
	case evt, ok := <-c.Events():
		if ok {
			t.Fatalf("unexpected event delivered: %+v", evt)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHub_SubscribeAndPublish_DeliversToSubscriber(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")

	hub.Publish("SEV-1", "sev.updated", []byte(`{"id":"SEV-1"}`))

	evt := recv(t, c)
	if evt.Type != "sev.updated" || evt.SEVID != "SEV-1" {
		t.Errorf("got %+v, want type=sev.updated sev_id=SEV-1", evt)
	}
	if string(evt.Payload) != `{"id":"SEV-1"}` {
		t.Errorf("payload = %s, want raw JSON passthrough", evt.Payload)
	}
}

func TestHub_Unsubscribe_StopsDelivery(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")
	hub.Unsubscribe(c, "SEV-1")

	hub.Publish("SEV-1", "sev.updated", nil)

	assertNoEvent(t, c)
}

func TestHub_MultipleRooms_Isolated(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")

	hub.Publish("SEV-2", "sev.updated", nil)

	assertNoEvent(t, c)
}

func TestHub_MultipleClientsSameRoom_AllReceive(t *testing.T) {
	hub := ws.NewHub()
	a := hub.NewClient()
	b := hub.NewClient()
	hub.Subscribe(a, "SEV-1")
	hub.Subscribe(b, "SEV-1")

	hub.Publish("SEV-1", "chat.created", []byte(`{}`))

	evtA := recv(t, a)
	evtB := recv(t, b)
	if evtA.Type != "chat.created" || evtB.Type != "chat.created" {
		t.Errorf("both subscribers should receive the event, got a=%+v b=%+v", evtA, evtB)
	}
}

func TestHub_ClientInMultipleRooms_ReceivesFromBoth(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")
	hub.Subscribe(c, "SEV-2")

	hub.Publish("SEV-1", "sev.updated", nil)
	hub.Publish("SEV-2", "sev.status_changed", nil)

	first := recv(t, c)
	second := recv(t, c)
	got := map[string]bool{first.Type: true, second.Type: true}
	if !got["sev.updated"] || !got["sev.status_changed"] {
		t.Errorf("expected events from both rooms, got %+v and %+v", first, second)
	}
}

func TestHub_Publish_NoSubscribers_NoOp(t *testing.T) {
	hub := ws.NewHub()
	// Must not panic or block when the room doesn't exist.
	hub.Publish("SEV-nonexistent", "sev.updated", nil)
}

func TestHub_Close_ClosesChannelAndRemovesFromAllRooms(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")
	hub.Subscribe(c, "SEV-2")

	hub.Close(c)

	if _, ok := <-c.Events(); ok {
		t.Error("channel should be closed after Close")
	}

	// Publishing to either former room must not panic (client already removed).
	hub.Publish("SEV-1", "sev.updated", nil)
	hub.Publish("SEV-2", "sev.updated", nil)
}

func TestHub_Close_OnlyAffectsClosedClientsOwnRooms(t *testing.T) {
	hub := ws.NewHub()
	a := hub.NewClient()
	b := hub.NewClient()
	hub.Subscribe(a, "SEV-1")
	hub.Subscribe(b, "SEV-1")
	hub.Subscribe(b, "SEV-2")

	hub.Close(b)

	if _, ok := <-b.Events(); ok {
		t.Error("b's channel should be closed")
	}

	// a is unrelated to b's Close and must still receive SEV-1 events.
	hub.Publish("SEV-1", "sev.updated", []byte(`{}`))
	evt := recv(t, a)
	if evt.SEVID != "SEV-1" {
		t.Errorf("a should be unaffected by b's Close, got %+v", evt)
	}

	// SEV-2 had only b subscribed; publishing there after b's Close must
	// not panic even though no one is listening anymore.
	hub.Publish("SEV-2", "sev.updated", nil)
}

func TestHub_SlowConsumer_PublishDoesNotBlock(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")

	// Flood well past the client buffer without ever draining it; Publish
	// must drop excess events rather than block the caller.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			hub.Publish("SEV-1", "sev.updated", nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow/undrained consumer")
	}
}

func TestHub_UnsubscribeUnknownRoom_NoOp(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	// Never subscribed anywhere; must not panic.
	hub.Unsubscribe(c, "SEV-1")
}

func TestHub_BroadcastRoom_ReceivesEventsFromEverySEV(t *testing.T) {
	hub := ws.NewHub()
	firehose := hub.NewClient()
	hub.Subscribe(firehose, ws.BroadcastRoom)

	hub.Publish("SEV-1", "sev.created", []byte(`{"id":"SEV-1"}`))
	hub.Publish("SEV-2", "sev.status_changed", []byte(`{"id":"SEV-2"}`))

	first := recv(t, firehose)
	second := recv(t, firehose)
	got := map[string]string{first.SEVID: first.Type, second.SEVID: second.Type}
	if got["SEV-1"] != "sev.created" || got["SEV-2"] != "sev.status_changed" {
		t.Errorf("broadcast subscriber should see events from both SEVs, got %+v", got)
	}
}

func TestHub_BroadcastRoom_DoesNotSuppressRoomSubscribers(t *testing.T) {
	hub := ws.NewHub()
	roomOnly := hub.NewClient()
	firehose := hub.NewClient()
	hub.Subscribe(roomOnly, "SEV-1")
	hub.Subscribe(firehose, ws.BroadcastRoom)

	hub.Publish("SEV-1", "sev.updated", []byte(`{}`))

	if evt := recv(t, roomOnly); evt.SEVID != "SEV-1" {
		t.Errorf("room subscriber got %+v, want SEV-1 event", evt)
	}
	if evt := recv(t, firehose); evt.SEVID != "SEV-1" {
		t.Errorf("broadcast subscriber got %+v, want SEV-1 event", evt)
	}
}

func TestHub_ClientSubscribedToBothRoomAndBroadcast_ReceivesEventOnce(t *testing.T) {
	hub := ws.NewHub()
	c := hub.NewClient()
	hub.Subscribe(c, "SEV-1")
	hub.Subscribe(c, ws.BroadcastRoom)

	hub.Publish("SEV-1", "sev.updated", []byte(`{}`))

	recv(t, c)          // the one delivery we expect
	assertNoEvent(t, c) // a second delivery would indicate a duplicate
}
