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
// datastore path is usable. ok=false with a nil err means integrationType is
// legitimately unconfigured — no config row exists yet, or the row has no
// credentials stored (e.g. a settings-only row) — the caller should fall
// back to its static, env-var-configured client and this is ordinary
// operation, not a failure.
//
// A non-nil err means something actually went wrong resolving a row that
// does exist: the store returned an error other than store.ErrNotFound, or
// the stored credentials failed to decrypt (e.g. ENCRYPTION_KEY was rotated
// or is wrong). Callers still fall back to their static client in this case
// too — a broken datastore path degrading to "use the env var" rather than
// breaking the request is the same best-effort posture as everywhere else
// integrations are consulted (see grpchandler.SEVServer's on-call lookup) —
// but they also propagate err upward so ConfigServer.UpsertIntegrationConfig
// can tell the caller their write did not actually take effect, instead of
// reporting success for a credential that silently isn't usable.
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
) (creds map[string]string, settings map[string]any, ok bool, err error) {
	cfg, err := integrations.Get(ctx, integrationType)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, false, nil
		}
		slog.WarnContext(ctx, "integration config lookup failed, using static fallback",
			"integration_type", integrationType, "err", err)
		return nil, nil, false, err
	}

	creds, err = grpchandler.DecryptIntegrationCredentials(crypto, cfg)
	if err != nil {
		slog.WarnContext(ctx, "integration credential decryption failed, using static fallback",
			"integration_type", integrationType, "err", err)
		return nil, nil, false, err
	}
	if len(creds) == 0 {
		return nil, nil, false, nil
	}
	return creds, cfg.Settings, true, nil
}
