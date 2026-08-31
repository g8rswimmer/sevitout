package main

import (
	"context"
	"sync"
	"testing"
)

// builtSlackClients records, safe for concurrent use, which fakeSlack
// instance withFakeSlackAPIClient built for each bot token — a test's
// background refresher goroutine writes to it while the test's own
// goroutine polls it, so a bare map would race.
type builtSlackClients struct {
	mu      sync.Mutex
	clients map[string]*fakeSlack
}

func (b *builtSlackClients) get(botToken string) (*fakeSlack, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, ok := b.clients[botToken]
	return f, ok
}

func (b *builtSlackClients) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

func (b *builtSlackClients) set(botToken string, f *fakeSlack) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[botToken] = f
}

// withFakeSlackAPIClient substitutes newSlackAPIClient for the duration of a
// test, so slackClientResolver.apply builds fakeSlack instances (recording
// which bot token each was built with) instead of real Slack API clients.
func withFakeSlackAPIClient(t *testing.T) *builtSlackClients {
	t.Helper()
	built := &builtSlackClients{clients: make(map[string]*fakeSlack)}
	orig := newSlackAPIClient
	newSlackAPIClient = func(botToken string) slackClient {
		f := &fakeSlack{}
		built.set(botToken, f)
		return f
	}
	t.Cleanup(func() { newSlackAPIClient = orig })
	return built
}

func TestSlackClientResolver_DelegatesToCurrentClient(t *testing.T) {
	initial := &fakeSlack{createChannelID: "C-INITIAL"}
	r := newSlackClientResolver(initial, "")

	id, err := r.CreateChannel(context.Background(), "inc-1")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if id != "C-INITIAL" {
		t.Errorf("CreateChannel = %q, want the initial client's response", id)
	}
}

func TestSlackClientResolver_Apply_SwapsInNewClient(t *testing.T) {
	built := withFakeSlackAPIClient(t)
	r := newSlackClientResolver(&fakeSlack{createChannelID: "C-OLD"}, "")

	r.apply("xoxb-new")

	f, ok := built.get("xoxb-new")
	if !ok {
		t.Fatalf("apply did not build a client for the new token")
	}
	f.createChannelID = "C-NEW"

	id, err := r.CreateChannel(context.Background(), "inc-1")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if id != "C-NEW" {
		t.Errorf("CreateChannel = %q, want the swapped-in client's response after apply", id)
	}
}

func TestSlackClientResolver_Apply_SameTokenIsNoOp(t *testing.T) {
	built := withFakeSlackAPIClient(t)
	r := newSlackClientResolver(&fakeSlack{createChannelID: "C-INITIAL"}, "xoxb-same")

	changed := r.apply("xoxb-same")

	if changed {
		t.Error("apply with the already-current token should report changed=false")
	}
	if built.len() != 0 {
		t.Error("apply with the already-current token should not rebuild a client")
	}
	id, err := r.CreateChannel(context.Background(), "inc-1")
	if err != nil || id != "C-INITIAL" {
		t.Errorf("CreateChannel = (%q, %v), want the original client left in place", id, err)
	}
}

func TestSlackClientResolver_SatisfiesSlackClientInterface(t *testing.T) {
	var _ slackClient = newSlackClientResolver(&fakeSlack{}, "")
}
