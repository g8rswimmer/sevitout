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
	cfg := &fakeConfigAPI{
		resp: &pb.IntegrationConfigResponse{
			Settings: map[string]string{
				"default_channel":           "general",
				"channel_naming_convention": "inc-{level}-{id}",
			},
		},
		credentialResp: &pb.GetSlackBotCredentialResponse{BotToken: "xoxb-store", AppToken: "xapp-store"},
	}

	ch, conv, bot, app := loadSlackSettings(context.Background(), slog.Default(), cfg, "xoxb-static", "xapp-static")

	if ch != "general" || conv != "inc-{level}-{id}" {
		t.Errorf("got (%q, %q)", ch, conv)
	}
	if bot != "xoxb-store" || app != "xapp-store" {
		t.Errorf("bot/app token = (%q, %q), want the datastore-configured pair preferred over static tokens", bot, app)
	}
}

func TestLoadSlackSettings_NoDatastoreCredential_FallsBackToStaticTokens(t *testing.T) {
	cfg := &fakeConfigAPI{resp: &pb.IntegrationConfigResponse{}}

	_, _, bot, app := loadSlackSettings(context.Background(), slog.Default(), cfg, "xoxb-static", "xapp-static")

	if bot != "xoxb-static" || app != "xapp-static" {
		t.Errorf("bot/app token = (%q, %q), want static fallback when nothing is datastore-configured", bot, app)
	}
}

func TestLoadSlackSettings_GivesUpAfterRetriesExhausted(t *testing.T) {
	withFastRetries(t)
	cfg := &fakeConfigAPI{err: errAlways}

	ch, conv, bot, app := loadSlackSettings(context.Background(), slog.Default(), cfg, "xoxb-static", "xapp-static")

	if ch != "" || conv != "" {
		t.Errorf("got (%q, %q), want empty defaults after every attempt fails", ch, conv)
	}
	if bot != "xoxb-static" || app != "xapp-static" {
		t.Errorf("bot/app token = (%q, %q), want static fallback after every attempt fails", bot, app)
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
		loadSlackSettings(ctx, slog.Default(), cfg, "xoxb-static", "xapp-static")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loadSlackSettings did not return promptly after ctx was canceled")
	}
}
