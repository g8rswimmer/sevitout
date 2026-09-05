package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// ── Notifications & Alerting (docs/requirements.md §16/§18.5, docs/roadmap.md
// Phase 15) ──────────────────────────────────────────────────────────────

// notificationEvents is the fixed, known set of event types a
// NotificationConfig rule may route on — every event this codebase actually
// fires a Notify call for (internal/api/grpc/sev.go, announcement.go,
// postmortem.go, and cmd/server's escalation and SLA-risk scanners).
var notificationEvents = map[string]bool{
	"sev.created":          true,
	"sev.updated":          true,
	"sev.status_changed":   true,
	"announcement.created": true,
	"postmortem.due":       true,
	"postmortem.approved":  true,
	"sev.escalation_no_ic": true,
	// sev.sla_at_risk / sev.sla_breached fire from cmd/server's
	// startSLARiskScanner, driven by internal/sev.EvaluateSLA's Overall
	// status (docs/roadmap.md Phase 12's SLA targets) — one-time per SEV per
	// level via SEV.SLANotifiedStatus, same "notify once" posture as
	// sev.escalation_no_ic's EscalatedAt marker.
	"sev.sla_at_risk":  true,
	"sev.sla_breached": true,
}

func validNotificationRole(role string) bool {
	switch store.OrgRole(role) {
	case store.OrgRoleViewer, store.OrgRoleResponder, store.OrgRoleIncidentCommander, store.OrgRoleAdmin:
		return true
	default:
		return false
	}
}

func validChannelType(channelType string) bool {
	switch store.NotificationChannelType(channelType) {
	case store.NotificationChannelSlack, store.NotificationChannelEmail:
		return true
	default:
		return false
	}
}

// validateNotificationConfigFields validates the role/events/channel_type
// shared by Create/Update. Rules are ID-identified (see
// store.NotificationConfig's doc comment), so unlike the original
// single-event design this is no longer also the rule's identity.
func validateNotificationConfigFields(role string, events []string, channelType string) error {
	if !validNotificationRole(role) {
		return status.Error(codes.InvalidArgument, "role must be one of: viewer, responder, incident-commander, admin")
	}
	if len(events) == 0 {
		return status.Error(codes.InvalidArgument, "events must contain at least one event type")
	}
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		if !notificationEvents[event] {
			return status.Error(codes.InvalidArgument, "unknown event type: "+event)
		}
		if seen[event] {
			return status.Error(codes.InvalidArgument, "duplicate event type: "+event)
		}
		seen[event] = true
	}
	if !validChannelType(channelType) {
		return status.Error(codes.InvalidArgument, "channel_type must be one of: slack, email")
	}
	return nil
}

func notificationMaxSeverity(lvl int32) (*int16, error) {
	if lvl == 0 {
		return nil, nil
	}
	if lvl < 1 || lvl > 4 {
		return nil, status.Error(codes.InvalidArgument, "max_severity_level must be between 1 and 4, or 0 for unset")
	}
	v := int16(lvl)
	return &v, nil
}

func (s *ConfigServer) CreateNotificationConfig(ctx context.Context, req *pb.CreateNotificationConfigRequest) (*pb.NotificationConfigResponse, error) {
	if err := validateNotificationConfigFields(req.GetRole(), req.GetEvents(), req.GetChannelType()); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.GetChannelTarget())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "channel_target is required")
	}
	maxSeverity, err := notificationMaxSeverity(req.GetMaxSeverityLevel())
	if err != nil {
		return nil, err
	}

	// The in-memory store persists whatever CreatedAt/UpdatedAt it's handed
	// rather than stamping them itself (same convention as AIPluginStore);
	// the postgres store overwrites both via NOW() regardless, so this is
	// only load-bearing for the memory backend.
	now := time.Now()
	cfg := &store.NotificationConfig{
		Role:             store.OrgRole(req.GetRole()),
		Events:           req.GetEvents(),
		ChannelType:      store.NotificationChannelType(req.GetChannelType()),
		ChannelTarget:    target,
		MaxSeverityLevel: maxSeverity,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.notificationConfigs.Create(ctx, cfg); err != nil {
		return nil, internalError(ctx, "failed to create notification config", err)
	}

	slog.InfoContext(ctx, "notification config created",
		"actor", callerID(ctx), "id", cfg.ID, "role", cfg.Role, "events", cfg.Events, "channel_type", cfg.ChannelType)

	return notificationConfigToProto(cfg), nil
}

func (s *ConfigServer) UpdateNotificationConfig(ctx context.Context, req *pb.UpdateNotificationConfigRequest) (*pb.NotificationConfigResponse, error) {
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := validateNotificationConfigFields(req.GetRole(), req.GetEvents(), req.GetChannelType()); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.GetChannelTarget())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "channel_target is required")
	}
	maxSeverity, err := notificationMaxSeverity(req.GetMaxSeverityLevel())
	if err != nil {
		return nil, err
	}

	// UpdatedAt matters for the memory backend the same way it does in
	// Create above; CreatedAt is ignored on update by both backends (memory
	// preserves the existing value explicitly, postgres's UPDATE never
	// touches the column), so it's left unset here.
	cfg := &store.NotificationConfig{
		ID:               req.GetId(),
		Role:             store.OrgRole(req.GetRole()),
		Events:           req.GetEvents(),
		ChannelType:      store.NotificationChannelType(req.GetChannelType()),
		ChannelTarget:    target,
		MaxSeverityLevel: maxSeverity,
		UpdatedAt:        time.Now(),
	}
	if err := s.notificationConfigs.Update(ctx, cfg); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "notification config not found")
		}
		return nil, internalError(ctx, "failed to update notification config", err)
	}

	slog.InfoContext(ctx, "notification config updated",
		"actor", callerID(ctx), "id", cfg.ID, "role", cfg.Role, "events", cfg.Events, "channel_type", cfg.ChannelType)

	return notificationConfigToProto(cfg), nil
}

func (s *ConfigServer) DeleteNotificationConfig(ctx context.Context, req *pb.DeleteNotificationConfigRequest) (*emptypb.Empty, error) {
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.notificationConfigs.Delete(ctx, req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "notification config not found")
		}
		return nil, internalError(ctx, "failed to delete notification config", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListNotificationConfigs(ctx context.Context, _ *emptypb.Empty) (*pb.ListNotificationConfigsResponse, error) {
	rows, err := s.notificationConfigs.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list notification configs", err)
	}
	resp := &pb.ListNotificationConfigsResponse{}
	for _, c := range rows {
		resp.Configs = append(resp.Configs, notificationConfigToProto(c))
	}
	return resp, nil
}

// TestNotificationConfig sends one real test message per event in req
// straight to req's channel_type/channel_target (Notifier.Test — bypasses
// ListForEvent's event/severity matching entirely). Works for a rule that's
// already saved (pass its current field values from the rules table) or
// still being drafted in the Add-rule form (no id involved either way) —
// lets an admin verify a channel/integration actually works without
// waiting for a real SEV event. Every field is validated exactly like
// Create, since a test send with an invalid role/event/channel_type would
// just be confusing rather than informative.
func (s *ConfigServer) TestNotificationConfig(ctx context.Context, req *pb.TestNotificationConfigRequest) (*pb.TestNotificationConfigResponse, error) {
	if s.notifier == nil {
		return nil, status.Error(codes.Unavailable, "notification testing is not available on this server")
	}
	if err := validateNotificationConfigFields(req.GetRole(), req.GetEvents(), req.GetChannelType()); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.GetChannelTarget())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "channel_target is required")
	}
	maxSeverity, err := notificationMaxSeverity(req.GetMaxSeverityLevel())
	if err != nil {
		return nil, err
	}

	cfg := &store.NotificationConfig{
		Role:             store.OrgRole(req.GetRole()),
		Events:           req.GetEvents(),
		ChannelType:      store.NotificationChannelType(req.GetChannelType()),
		ChannelTarget:    target,
		MaxSeverityLevel: maxSeverity,
	}
	results := s.notifier.Test(ctx, cfg)

	slog.InfoContext(ctx, "notification config tested",
		"actor", callerID(ctx), "events", cfg.Events, "channel_type", cfg.ChannelType, "channel_target", cfg.ChannelTarget)

	resp := &pb.TestNotificationConfigResponse{}
	for _, r := range results {
		result := &pb.TestNotificationResult{Event: r.Event, Success: r.Err == nil}
		if r.Err != nil {
			result.Error = r.Err.Error()
		}
		resp.Results = append(resp.Results, result)
	}
	return resp, nil
}

func notificationConfigToProto(c *store.NotificationConfig) *pb.NotificationConfigResponse {
	resp := &pb.NotificationConfigResponse{
		Id:            c.ID,
		Role:          string(c.Role),
		Events:        c.Events,
		ChannelType:   string(c.ChannelType),
		ChannelTarget: c.ChannelTarget,
		CreatedAt:     timestamppb.New(c.CreatedAt),
		UpdatedAt:     timestamppb.New(c.UpdatedAt),
	}
	if c.MaxSeverityLevel != nil {
		resp.MaxSeverityLevel = int32(*c.MaxSeverityLevel)
	}
	return resp
}

// ── Escalation thresholds ─────────────────────────────────────────────────

func (s *ConfigServer) UpsertEscalationConfig(ctx context.Context, req *pb.UpsertEscalationConfigRequest) (*pb.EscalationConfigResponse, error) {
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	if req.GetThresholdMinutes() < 0 {
		return nil, status.Error(codes.InvalidArgument, "threshold_minutes must be >= 0")
	}

	now := time.Now()
	cfg := &store.EscalationConfig{
		SeverityLevel:    int16(req.GetSeverityLevel()),
		ThresholdMinutes: req.GetThresholdMinutes(),
		Enabled:          req.GetEnabled(),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.escalationConfigs.Upsert(ctx, cfg); err != nil {
		return nil, internalError(ctx, "failed to upsert escalation config", err)
	}

	slog.InfoContext(ctx, "escalation config updated",
		"actor", callerID(ctx), "severity_level", cfg.SeverityLevel,
		"threshold_minutes", cfg.ThresholdMinutes, "enabled", cfg.Enabled)

	return escalationConfigToProto(cfg), nil
}

func (s *ConfigServer) ListEscalationConfigs(ctx context.Context, _ *emptypb.Empty) (*pb.ListEscalationConfigsResponse, error) {
	rows, err := s.escalationConfigs.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list escalation configs", err)
	}
	resp := &pb.ListEscalationConfigsResponse{}
	for _, cfg := range rows {
		resp.Configs = append(resp.Configs, escalationConfigToProto(cfg))
	}
	return resp, nil
}

func escalationConfigToProto(cfg *store.EscalationConfig) *pb.EscalationConfigResponse {
	return &pb.EscalationConfigResponse{
		SeverityLevel:    int32(cfg.SeverityLevel),
		ThresholdMinutes: cfg.ThresholdMinutes,
		Enabled:          cfg.Enabled,
		UpdatedAt:        timestamppb.New(cfg.UpdatedAt),
	}
}
