package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// slackSettingsRefreshInterval is how often runSettingsRefresher re-fetches
// the "slack" integration config in the background, so an admin changing
// default_channel or channel_naming_convention via ConfigService takes
// effect without restarting the bot. Declared as a var (not a const) so
// tests can shrink it.
var slackSettingsRefreshInterval = 5 * time.Minute

// runSettingsRefresher periodically re-fetches the "slack" integration
// config and applies any change to b's settings, until ctx is canceled. Per
// docs/roadmap.md Phase 8, the same tick also refreshes restClient's REST
// client from a datastore-configured bot token, if one is now available —
// folded into this existing poller rather than adding a second one, since
// both read the same "slack" integration config's credentials/settings.
// restClient may be nil (e.g. in tests that don't exercise the credential
// path), in which case that half of the refresh is skipped.
//
// Unlike the one-shot loadSlackSettings called at startup — which has
// nothing better to fall back to than empty defaults — a failed refresh
// here leaves the bot's current settings and REST client untouched rather
// than clearing them: a transient error talking to the API server shouldn't
// silently disable notifications, or revert to a stale bot token, that were
// already working.
func (b *bot) runSettingsRefresher(ctx context.Context, config configAPI, restClient *slackClientResolver) {
	ticker := time.NewTicker(slackSettingsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resp, err := config.GetIntegrationConfig(ctx, &pb.GetIntegrationConfigRequest{IntegrationType: "slack"})
			if err != nil {
				b.log.WarnContext(ctx, "periodic slack integration config refresh failed, keeping previous settings", "err", err)
			} else {
				b.setSlackSettings(resp.GetSettings()["default_channel"], resp.GetSettings()["channel_naming_convention"])
			}
			refreshSlackRESTClient(ctx, b.log, config, restClient)
		}
	}
}

// refreshSlackRESTClient polls ConfigService.GetSlackBotCredential and, if
// it returns a usable bot_token, swaps it into restClient. A fetch error or
// an empty bot_token (nothing datastore-configured, or this server has no
// slackbot service account set up) leaves restClient's current client
// exactly as it was — there is nothing to fall back to here beyond what
// startup already resolved, so "leave it alone" is the safe default, not
// "clear it."
func refreshSlackRESTClient(ctx context.Context, log *slog.Logger, config configAPI, restClient *slackClientResolver) {
	if restClient == nil {
		return
	}
	resp, err := config.GetSlackBotCredential(ctx, &pb.GetSlackBotCredentialRequest{})
	if err != nil {
		log.DebugContext(ctx, "periodic slack bot credential refresh failed, keeping current REST client", "err", err)
		return
	}
	if resp.GetBotToken() == "" {
		return
	}
	if restClient.apply(resp.GetBotToken()) {
		log.InfoContext(ctx, "applied a rotated datastore-configured slack bot credential to the REST client")
	}
}
