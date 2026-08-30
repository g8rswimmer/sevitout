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

	existing, err := s.integrations.Get(ctx, req.GetIntegrationType())
	switch {
	case err == nil:
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
	// below can be validated against reality — not an empty map that would
	// look like "not configured" — without any of them needing to read or
	// decrypt anything from the datastore themselves.
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
		// credentials that will actually remain in effect.
		decrypted, decErr := DecryptIntegrationCredentials(s.crypto, cfg)
		if decErr != nil {
			// Pre-existing data this request didn't touch failing to
			// decrypt (e.g. ENCRYPTION_KEY rotated since it was written)
			// shouldn't block an otherwise-valid settings update — the
			// refreshers below just see no credentials for this call, same
			// as if none had ever been stored.
			slog.WarnContext(ctx, "existing integration credentials could not be decrypted; refreshers will see none",
				"integration_type", cfg.IntegrationType, "err", decErr)
		} else {
			plaintextCreds = decrypted
		}
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

	// Give every registered refresher (see cmd/server's *Resolver types) a
	// chance to reject these credentials *before* anything is persisted —
	// see IntegrationCredentialsRefresher's doc comment. A refresher only
	// acts on the integration_type it owns, so at most one is expected to
	// actually validate anything for a given call; errors.Join is used only
	// defensively, in case more than one somehow reports a problem.
	var refreshErrs []error
	for _, r := range s.refreshers {
		if err := r.RefreshIntegrationCredentials(ctx, cfg.IntegrationType, plaintextCreds, cfg.Settings); err != nil {
			refreshErrs = append(refreshErrs, err)
		}
	}
	if refreshErr := errors.Join(refreshErrs...); refreshErr != nil {
		slog.WarnContext(ctx, "integration config rejected by refresher, not saved",
			"actor", callerID(ctx), "integration_type", cfg.IntegrationType, "err", refreshErr)
		return nil, status.Errorf(codes.InvalidArgument, "integration config rejected: %v", refreshErr)
	}

	if err := s.integrations.Upsert(ctx, cfg); err != nil {
		return nil, internalError(ctx, "failed to save integration config", err)
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
