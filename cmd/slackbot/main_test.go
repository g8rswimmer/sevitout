package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// withFastRetries shrinks the module-level retry knobs for the duration of
// a test, so a test exercising the "give up after N attempts" path doesn't
// have to actually wait slackSettingsRetryAttempts*slackSettingsRetryDelay.
func withFastRetries(t *testing.T) {
	t.Helper()
	origAttempts, origDelay := slackSettingsRetryAttempts, slackSettingsRetryDelay
	slackSettingsRetryAttempts, slackSettingsRetryDelay = 2, time.Millisecond
	t.Cleanup(func() { slackSettingsRetryAttempts, slackSettingsRetryDelay = origAttempts, origDelay })
}

func TestLoadSlackSettings_Success(t *testing.T) {
	cfg := &fakeConfigAPI{resp: &pb.IntegrationConfigResponse{
		Settings: map[string]string{
			"default_channel":           "general",
			"channel_naming_convention": "inc-{level}-{id}",
		},
	}}

	ch, conv := loadSlackSettings(context.Background(), slog.Default(), cfg)

	if ch != "general" || conv != "inc-{level}-{id}" {
		t.Errorf("got (%q, %q)", ch, conv)
	}
}

func TestLoadSlackSettings_GivesUpAfterRetriesExhausted(t *testing.T) {
	withFastRetries(t)
	cfg := &fakeConfigAPI{err: errAlways}

	ch, conv := loadSlackSettings(context.Background(), slog.Default(), cfg)

	if ch != "" || conv != "" {
		t.Errorf("got (%q, %q), want empty defaults after every attempt fails", ch, conv)
	}
}

func TestLoadSlackSettings_ContextCanceledStopsRetrying(t *testing.T) {
	withFastRetries(t)
	slackSettingsRetryDelay = time.Hour // would hang the test if not respected
	cfg := &fakeConfigAPI{err: errAlways}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		loadSlackSettings(ctx, slog.Default(), cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loadSlackSettings did not return promptly after ctx was canceled")
	}
}
