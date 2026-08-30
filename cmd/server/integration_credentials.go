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
// This hits integrations fresh on every call, with no in-process caching —
// the same "live from the store, decrypt per call" shape as
// internal/ai/dispatcher.go's resolvePlugin/buildProvider — so a credential
// added or changed via the Config API takes effect on the very next request,
// with no server restart required.
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
