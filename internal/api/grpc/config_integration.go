package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// ── Integration configuration ────────────────────────────────────────────────

// UpsertIntegrationConfig creates or updates the settings and credentials for
// one integration type. When credentials are supplied they are JSON-encoded
// and sealed with AES-256-GCM before being handed to the store — the store
// (and the database behind it) never sees plaintext. Omitting credentials
// leaves any previously stored credentials untouched.
func (s *ConfigServer) UpsertIntegrationConfig(ctx context.Context, req *pb.UpsertIntegrationConfigRequest) (*pb.IntegrationConfigResponse, error) {
	if req.GetIntegrationType() == "" {
		return nil, status.Error(codes.InvalidArgument, "integration_type is required")
	}

	now := time.Now()
	cfg := &store.IntegrationConfig{
		IntegrationType: req.GetIntegrationType(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// existing and hadExisting are kept around past this switch so a
	// refresh failure below can roll the write back to exactly what was
	// there before this call.
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

	// Let any in-process client cached from this integration's credentials
	// (see cmd/server's *Resolver types) pick up the change immediately,
	// rather than only on the next server restart. A refresher only acts on
	// notifications for the integration_type it owns (see
	// IntegrationCredentialsRefresher's doc comment), so at most one of
	// these is expected to actually do anything for a given call —
	// errors.Join is used only defensively, in case more than one somehow
	// reports a problem.
	var refreshErrs []error
	for _, r := range s.refreshers {
		if err := r.RefreshIntegrationCredentials(ctx, cfg.IntegrationType); err != nil {
			refreshErrs = append(refreshErrs, err)
		}
	}
	if refreshErr := errors.Join(refreshErrs...); refreshErr != nil {
		// The write above left credentials in the datastore that can't
		// actually be used (e.g. they fail to decrypt) — restore whatever
		// was there before this call, rather than confirming a save that
		// silently isn't in effect. There's no delete for a row that didn't
		// exist before this call; a rollback in that case clears it back to
		// an empty (no credentials, no settings) row for integrationType,
		// which every refresher already treats identically to "no row at
		// all" (see resolveIntegrationCredentials).
		rollback := &store.IntegrationConfig{IntegrationType: cfg.IntegrationType, CreatedAt: cfg.CreatedAt, UpdatedAt: cfg.CreatedAt}
		if hadExisting {
			rollback = existing
		}
		if err := s.integrations.Upsert(ctx, rollback); err != nil {
			slog.ErrorContext(ctx, "integration config rollback after refresh failure also failed",
				"integration_type", cfg.IntegrationType, "refresh_err", refreshErr, "rollback_err", err)
		}
		return nil, internalError(ctx,
			"integration config could not be applied; the previous configuration has been restored", refreshErr)
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
