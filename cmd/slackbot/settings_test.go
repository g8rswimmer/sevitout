package main

import (
	"context"
	"log/slog"
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
	go b.runSettingsRefresher(ctx, cfg, nil)

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
	go b.runSettingsRefresher(ctx, cfg, nil)

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
		b.runSettingsRefresher(ctx, cfg, nil)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runSettingsRefresher did not return promptly after ctx was canceled")
	}
}

func TestRunSettingsRefresher_AppliesDatastoreConfiguredSlackCredential(t *testing.T) {
	withFastRefresh(t, 10*time.Millisecond)
	built := withFakeSlackAPIClient(t)
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	resolver := newSlackClientResolver(&fakeSlack{}, "")
	cfg := &fakeConfigAPI{
		resp:           &pb.IntegrationConfigResponse{},
		credentialResp: &pb.GetSlackBotCredentialResponse{BotToken: "xoxb-rotated"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.runSettingsRefresher(ctx, cfg, resolver)

	deadline := time.After(time.Second)
	for {
		if _, ok := built.get("xoxb-rotated"); ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("REST client was never rebuilt with the rotated token")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRunSettingsRefresher_NilRESTClient_DoesNotPanic(t *testing.T) {
	withFastRefresh(t, 10*time.Millisecond)
	b := newTestBot(nil, nil, nil, nil, nil, "", "")
	cfg := &fakeConfigAPI{
		resp:           &pb.IntegrationConfigResponse{},
		credentialResp: &pb.GetSlackBotCredentialResponse{BotToken: "xoxb-rotated"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go b.runSettingsRefresher(ctx, cfg, nil)
	time.Sleep(30 * time.Millisecond)
	cancel()
}

func TestRefreshSlackRESTClient_EmptyBotToken_LeavesCurrentClientInPlace(t *testing.T) {
	built := withFakeSlackAPIClient(t)
	resolver := newSlackClientResolver(&fakeSlack{createChannelID: "C-ORIGINAL"}, "")
	cfg := &fakeConfigAPI{credentialResp: &pb.GetSlackBotCredentialResponse{}}

	refreshSlackRESTClient(context.Background(), slog.Default(), cfg, resolver)

	if built.len() != 0 {
		t.Error("an empty bot_token should not rebuild the client")
	}
	id, err := resolver.CreateChannel(context.Background(), "inc-1")
	if err != nil || id != "C-ORIGINAL" {
		t.Errorf("CreateChannel = (%q, %v), want the original client left untouched", id, err)
	}
}

func TestRefreshSlackRESTClient_FetchError_LeavesCurrentClientInPlace(t *testing.T) {
	built := withFakeSlackAPIClient(t)
	resolver := newSlackClientResolver(&fakeSlack{createChannelID: "C-ORIGINAL"}, "")
	cfg := &fakeConfigAPI{credentialErr: errAlways}

	refreshSlackRESTClient(context.Background(), slog.Default(), cfg, resolver)

	if built.len() != 0 {
		t.Error("a fetch error should not rebuild the client")
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
