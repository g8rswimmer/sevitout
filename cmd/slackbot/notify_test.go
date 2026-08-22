package main

import (
	"context"
	"strings"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/api/ws"
)

func TestShouldPushAnnouncement(t *testing.T) {
	cases := []struct {
		audience string
		want     bool
	}{
		{"internal", false},
		{"external", true},
		{"status-page", true},
		{"", false},
		{"bogus", false},
	}
	for _, c := range cases {
		if got := shouldPushAnnouncement(c.audience); got != c.want {
			t.Errorf("shouldPushAnnouncement(%q) = %v, want %v", c.audience, got, c.want)
		}
	}
}

func TestHandleEvent_SEVCreated_SEV1AutoCreatesChannelAndNotifies(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, &fakeRoleAPI{resp: nil}, nil, nil, "general", "")

	evt := ws.Event{
		Type:    "sev.created",
		SEVID:   "SEV-1",
		Payload: []byte(`{"id":"SEV-1","title":"checkout down","severity_level":1,"status":"open"}`),
	}
	b.handleEvent(context.Background(), evt)

	if b.channelFor("SEV-1") == "" {
		t.Error("expected an incident channel to be auto-created for a SEV-1")
	}
	// One post for the incident channel's own intro message, one for the
	// sev.created notification (which now goes to the new channel, not the
	// default one, since notifyChannel prefers it).
	if len(fs.posted) != 2 {
		t.Fatalf("posted %d messages, want 2: %+v", len(fs.posted), fs.posted)
	}
	for _, p := range fs.posted {
		if p.channel == "general" {
			t.Errorf("expected the sev.created notification to go to the new incident channel, not %q", p.channel)
		}
	}
}

func TestHandleEvent_SEVCreated_SEV3DoesNotCreateChannel(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, nil, nil, nil, "general", "")

	evt := ws.Event{
		Type:    "sev.created",
		Payload: []byte(`{"id":"SEV-1","title":"minor blip","severity_level":3,"status":"open"}`),
	}
	b.handleEvent(context.Background(), evt)

	if b.channelFor("SEV-1") != "" {
		t.Error("expected no incident channel for a SEV-3")
	}
	if len(fs.posted) != 1 || fs.posted[0].channel != "general" {
		t.Errorf("posted = %+v, want one notification to the default channel", fs.posted)
	}
}

func TestHandleEvent_SEVCreated_NoDefaultChannelIsANoop(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, nil, nil, nil, "", "")

	evt := ws.Event{
		Type:    "sev.created",
		Payload: []byte(`{"id":"SEV-1","title":"minor blip","severity_level":4,"status":"open"}`),
	}
	b.handleEvent(context.Background(), evt)

	if len(fs.posted) != 0 {
		t.Errorf("posted = %+v, want none (no default channel configured)", fs.posted)
	}
}

func TestHandleEvent_SEVStatusChanged_NotifiesConfiguredChannel(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, nil, nil, nil, "general", "")

	evt := ws.Event{
		Type:    "sev.status_changed",
		Payload: []byte(`{"id":"SEV-1","title":"checkout down","severity_level":1,"status":"resolved"}`),
	}
	b.handleEvent(context.Background(), evt)

	if len(fs.posted) != 1 || fs.posted[0].channel != "general" {
		t.Fatalf("posted = %+v", fs.posted)
	}
	if !strings.Contains(fs.posted[0].text, "resolved") {
		t.Errorf("text = %q, want it to mention the new status", fs.posted[0].text)
	}
}

func TestHandleEvent_AnnouncementCreated_InternalIsNotPushed(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, nil, nil, nil, "general", "")

	evt := ws.Event{
		Type:    "announcement.created",
		Payload: []byte(`{"sev_id":"SEV-1","message":"internal note","audience":"internal"}`),
	}
	b.handleEvent(context.Background(), evt)

	if len(fs.posted) != 0 {
		t.Errorf("posted = %+v, want none for an internal announcement", fs.posted)
	}
}

func TestHandleEvent_AnnouncementCreated_ExternalIsPushed(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, nil, nil, nil, "general", "")

	evt := ws.Event{
		Type:    "announcement.created",
		Payload: []byte(`{"sev_id":"SEV-1","message":"we are aware of the issue","audience":"external"}`),
	}
	b.handleEvent(context.Background(), evt)

	if len(fs.posted) != 1 || !strings.Contains(fs.posted[0].text, "we are aware of the issue") {
		t.Errorf("posted = %+v", fs.posted)
	}
}

func TestHandleEvent_UnknownTypeIsIgnored(t *testing.T) {
	fs := &fakeSlack{}
	b := newTestBot(fs, nil, nil, nil, nil, "general", "")

	b.handleEvent(context.Background(), ws.Event{Type: "task.updated", Payload: []byte(`{}`)})

	if len(fs.posted) != 0 {
		t.Errorf("posted = %+v, want none for an event type this bot doesn't act on", fs.posted)
	}
}

func TestHandleEvent_MalformedPayloadDoesNotPanic(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "general", "")
	b.handleEvent(context.Background(), ws.Event{Type: "sev.created", Payload: []byte(`not json`)})
}
