package main

import "testing"

func TestNotifyChannel_PrefersIncidentChannelOverDefault(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "general", "")
	b.setChannelFor("SEV-1", "C-INCIDENT")

	if got := b.notifyChannel("SEV-1"); got != "C-INCIDENT" {
		t.Errorf("got %q, want the incident channel", got)
	}
	if got := b.notifyChannel("SEV-2"); got != "general" {
		t.Errorf("got %q, want the default channel for a SEV with no incident channel", got)
	}
}

func TestNotifyChannel_EmptyWhenNeitherConfigured(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	if got := b.notifyChannel("SEV-1"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestChannelOrRegisterOpener_ReturnsExistingChannel(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	b.setChannelFor("SEV-1", "C-INCIDENT")

	if got := b.channelOrRegisterOpener("SEV-1", "U1"); got != "C-INCIDENT" {
		t.Errorf("got %q, want the existing incident channel", got)
	}
	if opener := b.takePendingOpener("SEV-1"); opener != "" {
		t.Errorf("opener = %q, want nothing registered when a channel already existed", opener)
	}
}

func TestChannelOrRegisterOpener_RegistersOpenerWhenNoChannelYet(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")

	if got := b.channelOrRegisterOpener("SEV-1", "U1"); got != "" {
		t.Errorf("got %q, want empty (no channel yet)", got)
	}
	if opener := b.takePendingOpener("SEV-1"); opener != "U1" {
		t.Errorf("opener = %q, want U1", opener)
	}
}

func TestTakePendingOpener_IsOneShot(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	b.channelOrRegisterOpener("SEV-1", "U1")

	if opener := b.takePendingOpener("SEV-1"); opener != "U1" {
		t.Fatalf("first take = %q, want U1", opener)
	}
	if opener := b.takePendingOpener("SEV-1"); opener != "" {
		t.Errorf("second take = %q, want empty (already consumed)", opener)
	}
}

func TestTakePendingOpener_NoneRegisteredReturnsEmpty(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	if opener := b.takePendingOpener("SEV-1"); opener != "" {
		t.Errorf("opener = %q, want empty", opener)
	}
}
