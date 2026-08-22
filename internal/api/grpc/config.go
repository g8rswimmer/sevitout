package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// Encryptor encrypts and decrypts integration credentials at rest with
// AES-256-GCM (see internal/store/crypto). Declared here (the consumer) so
// this package depends only on the two operations it needs; crypto.KeyEncryptor
// satisfies this implicitly.
type Encryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// ConfigServer implements pb.ConfigServiceServer: the admin configuration API
// (service registry, user management, on-call rotations, integration
// credentials, and data retention policy).
type ConfigServer struct {
	pb.UnimplementedConfigServiceServer
	services     store.ServiceStore
	users        store.UserStore
	oncall       store.OnCallStore
	integrations store.IntegrationConfigStore
	retention    store.RetentionConfigStore
	crypto       Encryptor // nil when ENCRYPTION_KEY is not set
}

// NewConfigServer returns a ConfigServer. crypto may be nil; in that case
// UpsertIntegrationConfig rejects any request that supplies credentials.
func NewConfigServer(
	services store.ServiceStore,
	users store.UserStore,
	oncall store.OnCallStore,
	integrations store.IntegrationConfigStore,
	retention store.RetentionConfigStore,
	crypto Encryptor,
) *ConfigServer {
	return &ConfigServer{
		services:     services,
		users:        users,
		oncall:       oncall,
		integrations: integrations,
		retention:    retention,
		crypto:       crypto,
	}
}

func callerID(ctx context.Context) string {
	if uc, ok := auth.UserFromContext(ctx); ok {
		return uc.UserID
	}
	return ""
}

// ── Service registry ─────────────────────────────────────────────────────────

func (s *ConfigServer) CreateService(ctx context.Context, req *pb.CreateServiceRequest) (*pb.ServiceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	now := time.Now()
	svc := &store.Service{
		ID:        req.GetId(),
		Name:      req.GetName(),
		Tags:      req.GetTags(),
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if v := req.GetDescription(); v != "" {
		svc.Description = &v
	}
	if v := req.GetOwningTeam(); v != "" {
		svc.OwningTeam = &v
	}
	if v := req.GetPagerdutyServiceId(); v != "" {
		svc.PagerDutyServiceID = &v
	}

	if err := s.services.Create(ctx, svc); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Error(codes.AlreadyExists, "a service with this id or name already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create service")
	}
	return serviceToProto(svc), nil
}

func (s *ConfigServer) GetService(ctx context.Context, req *pb.GetServiceRequest) (*pb.ServiceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	svc, err := s.services.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, status.Error(codes.Internal, "failed to get service")
	}
	return serviceToProto(svc), nil
}

func (s *ConfigServer) UpdateService(ctx context.Context, req *pb.UpdateServiceRequest) (*pb.ServiceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	svc, err := s.services.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, status.Error(codes.Internal, "failed to get service")
	}

	if v := req.GetName(); v != "" {
		svc.Name = v
	}
	if v := req.GetDescription(); v != "" {
		svc.Description = &v
	}
	if v := req.GetOwningTeam(); v != "" {
		svc.OwningTeam = &v
	}
	if v := req.GetPagerdutyServiceId(); v != "" {
		svc.PagerDutyServiceID = &v
	}
	if v := req.GetTags(); len(v) > 0 {
		svc.Tags = v
	}
	if req.GetActive() != nil {
		svc.Active = req.GetActive().GetValue()
	}
	svc.UpdatedAt = time.Now()

	if err := s.services.Update(ctx, svc); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, status.Error(codes.Internal, "failed to update service")
	}
	return serviceToProto(svc), nil
}

// DeleteService permanently removes a service record. Prefer UpdateService
// with active=false to retire a service while keeping historical SEV
// references intact (see docs/requirements.md §18.1).
func (s *ConfigServer) DeleteService(ctx context.Context, req *pb.DeleteServiceRequest) (*emptypb.Empty, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.services.Delete(ctx, req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete service")
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListServices(ctx context.Context, req *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
	svcs, err := s.services.List(ctx, req.GetActiveOnly())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list services")
	}
	resp := &pb.ListServicesResponse{}
	for _, svc := range svcs {
		resp.Services = append(resp.Services, serviceToProto(svc))
	}
	return resp, nil
}

func serviceToProto(svc *store.Service) *pb.ServiceResponse {
	resp := &pb.ServiceResponse{
		Id:        svc.ID,
		Name:      svc.Name,
		Tags:      svc.Tags,
		Active:    svc.Active,
		CreatedAt: timestamppb.New(svc.CreatedAt),
		UpdatedAt: timestamppb.New(svc.UpdatedAt),
	}
	if svc.Description != nil {
		resp.Description = *svc.Description
	}
	if svc.OwningTeam != nil {
		resp.OwningTeam = *svc.OwningTeam
	}
	if svc.PagerDutyServiceID != nil {
		resp.PagerdutyServiceId = *svc.PagerDutyServiceID
	}
	return resp
}

// ── User management ──────────────────────────────────────────────────────────

func (s *ConfigServer) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list users")
	}
	q := strings.ToLower(req.GetQuery())
	resp := &pb.ListUsersResponse{}
	for _, u := range users {
		if q != "" && !strings.Contains(strings.ToLower(u.Name), q) && !strings.Contains(strings.ToLower(u.Email), q) {
			continue
		}
		resp.Users = append(resp.Users, userToProto(u))
	}
	return resp, nil
}

func (s *ConfigServer) UpdateUserRole(ctx context.Context, req *pb.UpdateUserRoleRequest) (*pb.UserResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := validateOrgRole(req.GetOrgRole()); err != nil {
		return nil, err
	}

	u, err := s.users.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "failed to get user")
	}

	oldRole := u.OrgRole
	u.OrgRole = store.OrgRole(req.GetOrgRole())
	u.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, u); err != nil {
		return nil, status.Error(codes.Internal, "failed to update user")
	}

	// Permission changes must be logged (docs/requirements.md §14, §18.2).
	slog.InfoContext(ctx, "user role changed",
		"actor", callerID(ctx), "user_id", u.ID, "old_role", oldRole, "new_role", u.OrgRole)

	return userToProto(u), nil
}

func (s *ConfigServer) DeactivateUser(ctx context.Context, req *pb.DeactivateUserRequest) (*pb.UserResponse, error) {
	return s.setUserActive(ctx, req.GetId(), false)
}

func (s *ConfigServer) ReactivateUser(ctx context.Context, req *pb.ReactivateUserRequest) (*pb.UserResponse, error) {
	return s.setUserActive(ctx, req.GetId(), true)
}

func (s *ConfigServer) setUserActive(ctx context.Context, id string, active bool) (*pb.UserResponse, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	u, err := s.users.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "failed to get user")
	}
	u.Active = active
	u.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, u); err != nil {
		return nil, status.Error(codes.Internal, "failed to update user")
	}

	action := "deactivated"
	if active {
		action = "reactivated"
	}
	slog.InfoContext(ctx, "user "+action, "actor", callerID(ctx), "user_id", u.ID)

	return userToProto(u), nil
}

func validateOrgRole(role string) error {
	switch store.OrgRole(role) {
	case store.OrgRoleViewer, store.OrgRoleResponder, store.OrgRoleIncidentCommander, store.OrgRoleAdmin:
		return nil
	default:
		return status.Error(codes.InvalidArgument,
			"org_role must be one of: viewer, responder, incident-commander, admin")
	}
}

func userToProto(u *store.User) *pb.UserResponse {
	resp := &pb.UserResponse{
		Id:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		OrgRole:   string(u.OrgRole),
		Active:    u.Active,
		CreatedAt: timestamppb.New(u.CreatedAt),
		UpdatedAt: timestamppb.New(u.UpdatedAt),
	}
	if u.AvatarURL != nil {
		resp.AvatarUrl = *u.AvatarURL
	}
	return resp
}

// ── On-call configuration ────────────────────────────────────────────────────

func (s *ConfigServer) CreateOnCallRotation(ctx context.Context, req *pb.CreateOnCallRotationRequest) (*pb.OnCallRotationResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := validateOverrideWindow(req.GetOverrideStart(), req.GetOverrideEnd()); err != nil {
		return nil, err
	}

	now := time.Now()
	r := &store.OnCallRotation{
		Name:      req.GetName(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if v := req.GetServiceId(); v != "" {
		r.ServiceID = &v
	}
	if v := req.GetPagerdutyScheduleId(); v != "" {
		r.PagerDutyScheduleID = &v
	}
	if v := req.GetManualUserId(); v != "" {
		r.ManualUserID = &v
	}
	if v := req.GetManualDisplayName(); v != "" {
		r.ManualDisplayName = &v
	}
	if req.GetOverrideStart() != nil {
		t := req.GetOverrideStart().AsTime()
		r.OverrideStart = &t
	}
	if req.GetOverrideEnd() != nil {
		t := req.GetOverrideEnd().AsTime()
		r.OverrideEnd = &t
	}

	if err := s.oncall.Create(ctx, r); err != nil {
		return nil, status.Error(codes.Internal, "failed to create on-call rotation")
	}
	return onCallToProto(r), nil
}

func (s *ConfigServer) GetOnCallRotation(ctx context.Context, req *pb.GetOnCallRotationRequest) (*pb.OnCallRotationResponse, error) {
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	r, err := s.oncall.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "on-call rotation not found")
		}
		return nil, status.Error(codes.Internal, "failed to get on-call rotation")
	}
	return onCallToProto(r), nil
}

func (s *ConfigServer) UpdateOnCallRotation(ctx context.Context, req *pb.UpdateOnCallRotationRequest) (*pb.OnCallRotationResponse, error) {
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	r, err := s.oncall.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "on-call rotation not found")
		}
		return nil, status.Error(codes.Internal, "failed to get on-call rotation")
	}

	if v := req.GetName(); v != "" {
		r.Name = v
	}
	if v := req.GetServiceId(); v != "" {
		r.ServiceID = &v
	}
	if v := req.GetPagerdutyScheduleId(); v != "" {
		r.PagerDutyScheduleID = &v
	}
	if v := req.GetManualUserId(); v != "" {
		r.ManualUserID = &v
	}
	if v := req.GetManualDisplayName(); v != "" {
		r.ManualDisplayName = &v
	}
	if req.GetOverrideStart() != nil {
		t := req.GetOverrideStart().AsTime()
		r.OverrideStart = &t
	}
	if req.GetOverrideEnd() != nil {
		t := req.GetOverrideEnd().AsTime()
		r.OverrideEnd = &t
	}
	// Validate the window that will actually be persisted — a partial update
	// (e.g. only override_start supplied) merges onto whatever the other
	// bound already was, so checking the raw request in isolation isn't
	// enough to catch a resulting start >= end.
	if err := validateOverrideWindowTimes(r.OverrideStart, r.OverrideEnd); err != nil {
		return nil, err
	}
	r.UpdatedAt = time.Now()

	if err := s.oncall.Update(ctx, r); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "on-call rotation not found")
		}
		return nil, status.Error(codes.Internal, "failed to update on-call rotation")
	}
	return onCallToProto(r), nil
}

func (s *ConfigServer) DeleteOnCallRotation(ctx context.Context, req *pb.DeleteOnCallRotationRequest) (*emptypb.Empty, error) {
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.oncall.Delete(ctx, req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "on-call rotation not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete on-call rotation")
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListOnCallRotations(ctx context.Context, _ *pb.ListOnCallRotationsRequest) (*pb.ListOnCallRotationsResponse, error) {
	rotations, err := s.oncall.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list on-call rotations")
	}
	resp := &pb.ListOnCallRotationsResponse{}
	for _, r := range rotations {
		resp.Rotations = append(resp.Rotations, onCallToProto(r))
	}
	return resp, nil
}

// validateOverrideWindow rejects an override window where the end precedes
// (or equals) the start; a nil bound on either side is always accepted. Used
// by CreateOnCallRotation, where the request fields are the entire window
// (there's no existing record to merge onto).
func validateOverrideWindow(start, end *timestamppb.Timestamp) error {
	if start == nil || end == nil {
		return nil
	}
	s, e := start.AsTime(), end.AsTime()
	return validateOverrideWindowTimes(&s, &e)
}

// validateOverrideWindowTimes is validateOverrideWindow's *time.Time
// equivalent, used by UpdateOnCallRotation to validate the window that
// results after merging request fields onto the stored record — not just
// the (possibly partial) fields present on the request in isolation.
func validateOverrideWindowTimes(start, end *time.Time) error {
	if start == nil || end == nil {
		return nil
	}
	if !start.Before(*end) {
		return status.Error(codes.InvalidArgument, "override_start must be before override_end")
	}
	return nil
}

func onCallToProto(r *store.OnCallRotation) *pb.OnCallRotationResponse {
	resp := &pb.OnCallRotationResponse{
		Id:        r.ID,
		Name:      r.Name,
		CreatedAt: timestamppb.New(r.CreatedAt),
		UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
	if r.ServiceID != nil {
		resp.ServiceId = *r.ServiceID
	}
	if r.PagerDutyScheduleID != nil {
		resp.PagerdutyScheduleId = *r.PagerDutyScheduleID
	}
	if r.ManualUserID != nil {
		resp.ManualUserId = *r.ManualUserID
	}
	if r.ManualDisplayName != nil {
		resp.ManualDisplayName = *r.ManualDisplayName
	}
	if r.OverrideStart != nil {
		resp.OverrideStart = timestamppb.New(*r.OverrideStart)
	}
	if r.OverrideEnd != nil {
		resp.OverrideEnd = timestamppb.New(*r.OverrideEnd)
	}
	return resp
}

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
		return nil, status.Error(codes.Internal, "failed to get integration config")
	}

	if creds := req.GetCredentials(); len(creds) > 0 {
		if s.crypto == nil {
			return nil, status.Error(codes.FailedPrecondition,
				"credential encryption is not configured (ENCRYPTION_KEY not set)")
		}
		raw, err := json.Marshal(creds)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to encode credentials")
		}
		sealed, err := s.crypto.Encrypt(raw)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to encrypt credentials")
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
		return nil, status.Error(codes.Internal, "failed to save integration config")
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
		return nil, status.Error(codes.Internal, "failed to get integration config")
	}
	return integrationConfigToProto(cfg), nil
}

func (s *ConfigServer) ListIntegrationConfigs(ctx context.Context, _ *pb.ListIntegrationConfigsRequest) (*pb.ListIntegrationConfigsResponse, error) {
	cfgs, err := s.integrations.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list integration configs")
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

// ── Data retention ────────────────────────────────────────────────────────────

func (s *ConfigServer) GetRetentionConfig(ctx context.Context, req *pb.GetRetentionConfigRequest) (*pb.RetentionConfigResponse, error) {
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	cfg, err := s.retention.Get(ctx, int16(req.GetSeverityLevel()))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "retention config not found for this severity level")
		}
		return nil, status.Error(codes.Internal, "failed to get retention config")
	}
	return retentionConfigToProto(cfg), nil
}

func (s *ConfigServer) UpdateRetentionConfig(ctx context.Context, req *pb.UpdateRetentionConfigRequest) (*pb.RetentionConfigResponse, error) {
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	if req.GetRetentionDays() < 0 {
		return nil, status.Error(codes.InvalidArgument, "retention_days must be >= 0 (0 means retain forever)")
	}

	now := time.Now()
	cfg := &store.RetentionConfig{
		SeverityLevel: int16(req.GetSeverityLevel()),
		RetentionDays: int(req.GetRetentionDays()),
		HardDelete:    req.GetHardDelete(),
		// CreatedAt is only used when no row exists yet for this severity
		// level; the store overwrites it from the existing row otherwise.
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.retention.Upsert(ctx, cfg); err != nil {
		return nil, status.Error(codes.Internal, "failed to update retention config")
	}

	slog.InfoContext(ctx, "retention config updated",
		"actor", callerID(ctx), "severity_level", cfg.SeverityLevel,
		"retention_days", cfg.RetentionDays, "hard_delete", cfg.HardDelete)

	return retentionConfigToProto(cfg), nil
}

func (s *ConfigServer) ListRetentionConfig(ctx context.Context, _ *pb.ListRetentionConfigRequest) (*pb.ListRetentionConfigResponse, error) {
	cfgs, err := s.retention.List(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list retention config")
	}
	resp := &pb.ListRetentionConfigResponse{}
	for _, cfg := range cfgs {
		resp.Configs = append(resp.Configs, retentionConfigToProto(cfg))
	}
	return resp, nil
}

func validateSeverityLevel(level int32) error {
	if level < 1 || level > 4 {
		return status.Error(codes.InvalidArgument, "severity_level must be between 1 and 4")
	}
	return nil
}

func retentionConfigToProto(cfg *store.RetentionConfig) *pb.RetentionConfigResponse {
	return &pb.RetentionConfigResponse{
		SeverityLevel: int32(cfg.SeverityLevel),
		RetentionDays: int32(cfg.RetentionDays),
		HardDelete:    cfg.HardDelete,
		CreatedAt:     timestamppb.New(cfg.CreatedAt),
		UpdatedAt:     timestamppb.New(cfg.UpdatedAt),
	}
}
