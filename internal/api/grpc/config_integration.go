package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/integrations/catalog"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// ── Integration configuration ────────────────────────────────────────────────

// GetIntegrationCatalog returns the fixed, ordered field schema for every
// integration_type this server recognizes — a pure translation of
// catalog.All with no store access. See docs/roadmap.md Phase 9.
func (s *ConfigServer) GetIntegrationCatalog(_ context.Context, _ *pb.GetIntegrationCatalogRequest) (*pb.GetIntegrationCatalogResponse, error) {
	resp := &pb.GetIntegrationCatalogResponse{}
	for _, i := range catalog.All {
		resp.Integrations = append(resp.Integrations, &pb.IntegrationCatalogEntry{
			Type:             i.Type,
			Label:            i.Label,
			CredentialFields: catalogFieldsToProto(i.CredentialFields),
			SettingsFields:   catalogFieldsToProto(i.SettingsFields),
		})
	}
	return resp, nil
}

func catalogFieldsToProto(fields []catalog.Field) []*pb.CatalogField {
	out := make([]*pb.CatalogField, 0, len(fields))
	for _, f := range fields {
		out = append(out, &pb.CatalogField{
			Key:      f.Key,
			Label:    f.Label,
			Kind:     string(f.Kind),
			Required: f.Required,
			Help:     f.Help,
			Options:  f.Options,
		})
	}
	return out
}

// validateAgainstCatalog checks req's integration_type and every
// credential/settings key (and, for a select-kind settings field, its value)
// against the catalog before UpsertIntegrationConfig touches the store or
// crypto — an unknown integration_type or unknown key rejects the whole
// request, matching that method's existing all-or-nothing semantics (it
// already rolls back on refresher rejection). A select field's Required is
// intentionally not checked here — see UpsertIntegrationConfig's doc comment.
func validateAgainstCatalog(req *pb.UpsertIntegrationConfigRequest) error {
	integration, ok := catalog.Find(req.GetIntegrationType())
	if !ok {
		return status.Errorf(codes.InvalidArgument, "unknown integration_type %q", req.GetIntegrationType())
	}

	credentialKeys := integration.CredentialKeys()
	for key := range req.GetCredentials() {
		if !credentialKeys[key] {
			return status.Errorf(codes.InvalidArgument, "%s: unknown credential key %q", integration.Type, key)
		}
	}

	for key, value := range req.GetSettings() {
		field, ok := integration.SettingsField(key)
		if !ok {
			return status.Errorf(codes.InvalidArgument, "%s: unknown settings key %q", integration.Type, key)
		}
		if field.Kind == catalog.KindSelect && !slices.Contains(field.Options, value) {
			return status.Errorf(codes.InvalidArgument, "%s: %s must be one of %v, got %q", integration.Type, key, field.Options, value)
		}
	}

	return nil
}

// UpsertIntegrationConfig creates or updates the settings and credentials for
// one integration type. When credentials are supplied they are JSON-encoded
// and sealed with AES-256-GCM before being handed to the store — the store
// (and the database behind it) never sees plaintext. Omitting credentials
// leaves any previously stored credentials untouched.
//
// The integration_type and every credential/settings key are validated
// against the catalog (validateAgainstCatalog) before anything is written.
// Required is deliberately not enforced here: a request can supply just
// credentials or just settings and leave the other side untouched, so
// "required" would have to reason about the merged existing+incoming state —
// the existing fallback-to-static-client behavior in cmd/server/*_resolver.go
// already covers "this integration won't activate without X", so Required
// stays a UI-only affordance in this phase.
func (s *ConfigServer) UpsertIntegrationConfig(ctx context.Context, req *pb.UpsertIntegrationConfigRequest) (*pb.IntegrationConfigResponse, error) {
	if req.GetIntegrationType() == "" {
		return nil, status.Error(codes.InvalidArgument, "integration_type is required")
	}
	if err := validateAgainstCatalog(req); err != nil {
		return nil, err
	}

	now := time.Now()
	cfg := &store.IntegrationConfig{
		IntegrationType: req.GetIntegrationType(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// existing (and whether it was found) is kept around past this switch
	// for one reason: if the write below succeeds but the refresh step
	// afterward fails, this is exactly what gets restored.
	existing, err := s.integrations.Get(ctx, req.GetIntegrationType())
	hadExisting := false
	switch {
	case err == nil:
		hadExisting = true
		cfg.EncryptedCredentials = existing.EncryptedCredentials
		cfg.Settings = existing.Settings
		cfg.CreatedAt = existing.CreatedAt
	case errors.Is(err, store.ErrNotFound):
		// no existing row — cfg keeps its zero-value credentials and settings
	default:
		return nil, internalError(ctx, "failed to get integration config", err)
	}

	// plaintextCreds tracks whatever credentials will actually be in effect
	// for this integration_type once this write completes, so refreshers
	// below can be handed them directly instead of re-reading and
	// re-decrypting the row this call just wrote.
	var plaintextCreds map[string]string
	if creds := req.GetCredentials(); len(creds) > 0 {
		if s.crypto == nil {
			return nil, status.Error(codes.FailedPrecondition,
				"credential encryption is not configured (ENCRYPTION_KEY not set)")
		}
		raw, err := json.Marshal(creds)
		if err != nil {
			return nil, internalError(ctx, "failed to encode credentials", err)
		}
		sealed, err := s.crypto.Encrypt(raw)
		if err != nil {
			return nil, internalError(ctx, "failed to encrypt credentials", err)
		}
		cfg.EncryptedCredentials = sealed
		plaintextCreds = creds // already plaintext — no need to decrypt what we just encrypted
	} else if len(cfg.EncryptedCredentials) > 0 {
		// This request doesn't touch credentials, but a previous one left
		// some stored — decrypt them once here so refreshers see the
		// credentials that will actually remain in effect. Any decrypt
		// failure here (e.g. ENCRYPTION_KEY rotated since it was written)
		// aborts the whole request before anything is written — nothing to
		// roll back yet, so this is a plain early return.
		decrypted, decErr := DecryptIntegrationCredentials(s.crypto, cfg)
		if decErr != nil {
			return nil, status.Errorf(codes.FailedPrecondition,
				"existing credentials for %q could not be decrypted, refusing to update settings until this is resolved: %v",
				cfg.IntegrationType, decErr)
		}
		plaintextCreds = decrypted
	}

	// Settings, like credentials, are only replaced when the request actually
	// supplies them — an empty/omitted settings map leaves whatever is
	// already stored untouched instead of wiping it.
	if reqSettings := req.GetSettings(); len(reqSettings) > 0 {
		settings := make(map[string]any, len(reqSettings))
		for k, v := range reqSettings {
			settings[k] = v
		}
		cfg.Settings = settings
	}

	if err := s.integrations.Upsert(ctx, cfg); err != nil {
		return nil, internalError(ctx, "failed to save integration config", err)
	}

	// The write above is now durable; let every registered refresher (see
	// cmd/server's *Resolver types) apply it in-process. A refresher only
	// acts on the integration_type it owns, so at most one is expected to
	// actually validate anything for a given call; errors.Join is used only
	// defensively, in case more than one somehow reports a problem.
	//
	// If a refresher rejects it, the whole operation must be treated as
	// failed: roll the datastore back to exactly what it held before this
	// call (or clear it, for a brand-new integration_type — there is no
	// store.Delete) so the datastore and the in-process state it drives
	// never end up disagreeing, and report the failure instead of
	// confirming a save that isn't actually in effect. This is a
	// best-effort compensating write, not a real cross-system transaction —
	// there is a narrow window between the two Upsert calls where a
	// concurrent reader could observe the not-yet-rolled-back config — but
	// it's the closest approximation available when the second step
	// (refresh) is in-process Go, not something a database transaction can
	// span into.
	var refreshErrs []error
	for _, r := range s.refreshers {
		if err := r.RefreshIntegrationCredentials(ctx, cfg.IntegrationType, plaintextCreds, cfg.Settings); err != nil {
			refreshErrs = append(refreshErrs, err)
		}
	}
	if refreshErr := errors.Join(refreshErrs...); refreshErr != nil {
		rollback := &store.IntegrationConfig{IntegrationType: cfg.IntegrationType, CreatedAt: now, UpdatedAt: now}
		if hadExisting {
			rollback = existing
		}
		if rbErr := s.integrations.Upsert(ctx, rollback); rbErr != nil {
			slog.ErrorContext(ctx, "integration config rollback after refresh failure also failed",
				"integration_type", cfg.IntegrationType, "refresh_err", refreshErr, "rollback_err", rbErr)
		}
		slog.WarnContext(ctx, "integration config rejected by refresher, rolled back",
			"actor", callerID(ctx), "integration_type", cfg.IntegrationType, "err", refreshErr)
		return nil, status.Errorf(codes.InvalidArgument,
			"integration config rejected: %v; the previous configuration has been restored", refreshErr)
	}

	slog.InfoContext(ctx, "integration config updated",
		"actor", callerID(ctx), "integration_type", cfg.IntegrationType)

	return integrationConfigToProto(cfg), nil
}

func (s *ConfigServer) GetIntegrationConfig(ctx context.Context, req *pb.GetIntegrationConfigRequest) (*pb.IntegrationConfigResponse, error) {
	if req.GetIntegrationType() == "" {
		return nil, status.Error(codes.InvalidArgument, "integration_type is required")
	}
	cfg, err := s.integrations.Get(ctx, req.GetIntegrationType())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "integration config not found")
		}
		return nil, internalError(ctx, "failed to get integration config", err)
	}
	return integrationConfigToProto(cfg), nil
}

// GetSlackBotCredential returns the decrypted "slack" integration
// credential pair (bot_token/app_token) — see docs/roadmap.md Phase 8 and
// this RPC's proto doc comment for why this is the one deliberate exception
// to "credentials never cross this service's wire". It exists solely for
// cmd/slackbot, which has no in-process access to the datastore or
// ENCRYPTION_KEY the way cmd/server's PagerDuty/GitHub/Jira resolvers do.
//
// Gated to the specific slackbot service account, not "any Admin": the RBAC
// floor (rpcMinRole) still requires OrgRoleAdmin, but that alone would let
// an unrelated admin session pull a plaintext Slack token, which the other
// three integrations' resolvers never expose over any wire at all. This
// caller-identity check narrows it further to whoever is authenticated as
// SlackbotServiceEmail. An empty SlackbotServiceEmail (not configured at
// server startup) fails closed — rejecting every caller — rather than
// silently falling back to "any Admin may call this".
func (s *ConfigServer) GetSlackBotCredential(ctx context.Context, _ *pb.GetSlackBotCredentialRequest) (*pb.GetSlackBotCredentialResponse, error) {
	if s.slackbotServiceEmail == "" {
		return nil, status.Error(codes.PermissionDenied, "no slackbot service account is configured for this server")
	}
	uc, ok := auth.UserFromContext(ctx)
	if !ok || uc.Email != s.slackbotServiceEmail {
		return nil, status.Error(codes.PermissionDenied, "only the slackbot service account may call GetSlackBotCredential")
	}

	cfg, err := s.integrations.Get(ctx, "slack")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Nothing configured — an empty response, not an error, so the
			// caller's own static-token fallback applies.
			return &pb.GetSlackBotCredentialResponse{}, nil
		}
		return nil, internalError(ctx, "failed to get slack integration config", err)
	}
	creds, err := DecryptIntegrationCredentials(s.crypto, cfg)
	if err != nil {
		return nil, internalError(ctx, "failed to decrypt slack integration credentials", err)
	}
	return &pb.GetSlackBotCredentialResponse{
		BotToken: creds["bot_token"],
		AppToken: creds["app_token"],
	}, nil
}

func (s *ConfigServer) ListIntegrationConfigs(ctx context.Context, _ *pb.ListIntegrationConfigsRequest) (*pb.ListIntegrationConfigsResponse, error) {
	cfgs, err := s.integrations.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list integration configs", err)
	}
	resp := &pb.ListIntegrationConfigsResponse{}
	for _, cfg := range cfgs {
		resp.Configs = append(resp.Configs, integrationConfigToProto(cfg))
	}
	return resp, nil
}

// ListEnabledIntegrations returns the integration_types that currently have
// meaningful store-configured settings or credentials — see this RPC's proto
// doc comment for the viewer-safety and static-env-var-fallback caveats.
func (s *ConfigServer) ListEnabledIntegrations(ctx context.Context, _ *pb.ListEnabledIntegrationsRequest) (*pb.ListEnabledIntegrationsResponse, error) {
	cfgs, err := s.integrations.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list integration configs", err)
	}
	resp := &pb.ListEnabledIntegrationsResponse{}
	for _, cfg := range cfgs {
		if integrationConfigured(cfg) {
			resp.EnabledTypes = append(resp.EnabledTypes, cfg.IntegrationType)
		}
	}
	return resp, nil
}

// integrationConfigured reports whether cfg represents a "configured"
// integration by the same concept the admin UI already uses: a non-empty
// encrypted-credentials blob, or — for a settings-only integration_type like
// Monitoring, which has no credential fields at all — a non-empty settings
// map.
func integrationConfigured(cfg *store.IntegrationConfig) bool {
	if len(cfg.EncryptedCredentials) > 0 {
		return true
	}
	if integration, ok := catalog.Find(cfg.IntegrationType); ok && len(integration.CredentialFields) == 0 {
		return len(cfg.Settings) > 0
	}
	return false
}

// integrationConfigToProto never includes the decrypted credentials — only
// whether credentials are currently configured.
func integrationConfigToProto(cfg *store.IntegrationConfig) *pb.IntegrationConfigResponse {
	resp := &pb.IntegrationConfigResponse{
		IntegrationType:       cfg.IntegrationType,
		CredentialsConfigured: len(cfg.EncryptedCredentials) > 0,
		CreatedAt:             timestamppb.New(cfg.CreatedAt),
		UpdatedAt:             timestamppb.New(cfg.UpdatedAt),
	}
	if len(cfg.Settings) > 0 {
		resp.Settings = make(map[string]string, len(cfg.Settings))
		for k, v := range cfg.Settings {
			if sv, ok := v.(string); ok {
				resp.Settings[k] = sv
			}
		}
	}
	return resp
}

// DecryptIntegrationCredentials decrypts and JSON-decodes the credentials
// stored for cfg. It returns (nil, nil) when no credentials are configured.
// Exported for use by integration health checks (see IntegrationsHealthHandler)
// and by future integrations that need the plaintext credentials at runtime.
func DecryptIntegrationCredentials(enc Encryptor, cfg *store.IntegrationConfig) (map[string]string, error) {
	if len(cfg.EncryptedCredentials) == 0 {
		return nil, nil
	}
	if enc == nil {
		return nil, errors.New("credential encryption is not configured (ENCRYPTION_KEY not set)")
	}
	raw, err := enc.Decrypt(cfg.EncryptedCredentials)
	if err != nil {
		return nil, err
	}
	var creds map[string]string
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}
