package main

import (
	"context"
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
// config and applies any change to b's settings, until ctx is canceled.
//
// Unlike the one-shot loadSlackSettings called at startup — which has
// nothing better to fall back to than empty defaults — a failed refresh
// here leaves the bot's current settings untouched rather than clearing
// them: a transient error talking to the API server shouldn't silently
// disable notifications that were already working.
func (b *bot) runSettingsRefresher(ctx context.Context, config configAPI) {
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
				continue
			}
			b.setSlackSettings(resp.GetSettings()["default_channel"], resp.GetSettings()["channel_naming_convention"])
		}
	}
}
