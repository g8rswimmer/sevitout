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
