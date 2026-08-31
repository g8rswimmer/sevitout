package main

import (
	"context"
	"errors"
	"log/slog"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// resolveIntegrationCredentials looks up integrationType's datastore-configured
// credentials and decrypts them. This is only ever called once per resolver
// — from its constructor, at server startup. After that, a config change
// reaches a resolver via RefreshIntegrationCredentials, which is handed the
// plaintext credentials/settings directly by
// grpchandler.ConfigServer.UpsertIntegrationConfig, so no resolver ever
// needs to read or decrypt anything from the datastore again after startup.
//
// A nil creds with a nil err means integrationType is legitimately
// unconfigured — no row exists yet, or the row has no credentials stored —
// callers should use their static fallback and this is ordinary operation,
// not a failure. A non-nil err means resolving an existing row actually
// failed (the store returned an error other than store.ErrNotFound, or the
// stored credentials failed to decrypt, e.g. a wrong/rotated
// ENCRYPTION_KEY); it's returned only so the caller can log it — there's no
// request to report a startup failure to, so every caller degrades to its
// static fallback regardless of err.
func resolveIntegrationCredentials(
	ctx context.Context,
	integrations store.IntegrationConfigStore,
	crypto grpchandler.Encryptor,
	integrationType string,
) (creds map[string]string, settings map[string]any, err error) {
	cfg, err := integrations.Get(ctx, integrationType)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil
		}
		slog.WarnContext(ctx, "integration config lookup failed at startup, using static fallback",
			"integration_type", integrationType, "err", err)
		return nil, nil, err
	}

	creds, err = grpchandler.DecryptIntegrationCredentials(crypto, cfg)
	if err != nil {
		slog.WarnContext(ctx, "integration credential decryption failed at startup, using static fallback",
			"integration_type", integrationType, "err", err)
		return nil, nil, err
	}
	if len(creds) == 0 {
		return nil, nil, nil
	}
	return creds, cfg.Settings, nil
}
