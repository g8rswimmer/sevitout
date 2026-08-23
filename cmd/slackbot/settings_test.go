package main

import (
	"context"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// withFastRefresh shrinks slackSettingsRefreshInterval for the duration of a
// test, so a test doesn't have to wait the real 5 minutes for a tick.
func withFastRefresh(t *testing.T, interval time.Duration) {
	t.Helper()
	orig := slackSettingsRefreshInterval
	slackSettingsRefreshInterval = interval
	t.Cleanup(func() { slackSettingsRefreshInterval = orig })
}

func TestRunSettingsRefresher_AppliesUpdatedSettings(t *testing.T) {
	withFastRefresh(t, 10*time.Millisecond)
	b := newTestBot(nil, nil, nil, nil, nil, "old-channel", "old-{id}")
	cfg := &fakeConfigAPI{resp: &pb.IntegrationConfigResponse{
		Settings: map[string]string{"default_channel": "new-channel", "channel_naming_convention": "new-{id}"},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.runSettingsRefresher(ctx, cfg)

	deadline := time.After(time.Second)
	for {
		if b.notifyChannel("unmapped-sev") == "new-channel" && b.namingConvention() == "new-{id}" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("settings never updated: default_channel=%q naming=%q", b.notifyChannel("unmapped-sev"), b.namingConvention())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRunSettingsRefresher_FailedRefreshKeepsPreviousSettings(t *testing.T) {
	withFastRefresh(t, 10*time.Millisecond)
	b := newTestBot(nil, nil, nil, nil, nil, "stays-configured", "")
	cfg := &fakeConfigAPI{err: errAlways}

	ctx, cancel := context.WithCancel(context.Background())
	go b.runSettingsRefresher(ctx, cfg)

	// Give it a few ticks to (fail to) refresh, then confirm nothing changed.
	time.Sleep(50 * time.Millisecond)
	cancel()

	if got := b.notifyChannel("unmapped-sev"); got != "stays-configured" {
		t.Errorf("default_channel = %q, want it unchanged after failed refreshes", got)
	}
}

func TestRunSettingsRefresher_StopsOnContextCancel(t *testing.T) {
	withFastRefresh(t, 10*time.Millisecond)
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	cfg := &fakeConfigAPI{resp: &pb.IntegrationConfigResponse{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.runSettingsRefresher(ctx, cfg)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runSettingsRefresher did not return promptly after ctx was canceled")
	}
}

func TestSetSlackSettings_UpdatesBothFields(t *testing.T) {
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	b.setSlackSettings("C123", "conv-{id}")
	if got := b.notifyChannel("unmapped-sev"); got != "C123" {
		t.Errorf("default channel = %q, want C123", got)
	}
	if got := b.namingConvention(); got != "conv-{id}" {
		t.Errorf("naming convention = %q, want conv-{id}", got)
	}
}
