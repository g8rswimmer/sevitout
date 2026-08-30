package main

import (
	"context"
	"errors"
	"log/slog"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// resolveIntegrationCredentials looks up integrationType's datastore-configured
// credentials and returns them decrypted, along with ok=true, when the
// datastore path is usable for this call. It returns ok=false — with creds
// and settings left nil — whenever the caller should fall back to its static,
// env-var-configured client instead: no config row exists yet for
// integrationType, the row has no credentials configured, or decryption
// failed (e.g. ENCRYPTION_KEY was rotated or unset since the row was
// written). Every non-happy-path branch is logged at Warn, not Error: an
// env-var fallback being used is expected, ordinary operation, not a
// failure — mirroring the best-effort, non-blocking treatment of on-call
// lookup failures in grpchandler.SEVServer.
//
// Callers are the *Resolver types' refresh methods, not any per-request
// code path: each resolver calls this once at construction (server
// startup) and again only when notified, via RefreshIntegrationCredentials,
// that its integration's config changed through the Config API — then
// caches the result, so a credential added or changed via the Config API
// takes effect immediately, with no server restart required, but without
// a datastore round trip on every OnCallLookup/CreateIssue call.
func resolveIntegrationCredentials(
	ctx context.Context,
	integrations store.IntegrationConfigStore,
	crypto grpchandler.Encryptor,
	integrationType string,
) (creds map[string]string, settings map[string]any, ok bool) {
	cfg, err := integrations.Get(ctx, integrationType)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.WarnContext(ctx, "integration config lookup failed, using static fallback",
				"integration_type", integrationType, "err", err)
		}
		return nil, nil, false
	}

	creds, err = grpchandler.DecryptIntegrationCredentials(crypto, cfg)
	if err != nil {
		slog.WarnContext(ctx, "integration credential decryption failed, using static fallback",
			"integration_type", integrationType, "err", err)
		return nil, nil, false
	}
	if len(creds) == 0 {
		return nil, nil, false
	}
	return creds, cfg.Settings, true
}
