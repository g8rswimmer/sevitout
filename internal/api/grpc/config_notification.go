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

// validateNotificationConfigKey validates the (role, event, channel_type)
// triple shared by Upsert/Delete — the rule's identity.
func validateNotificationConfigKey(role, event, channelType string) error {
	if !validNotificationRole(role) {
		return status.Error(codes.InvalidArgument, "role must be one of: viewer, responder, incident-commander, admin")
	}
	if !notificationEvents[event] {
		return status.Error(codes.InvalidArgument, "unknown event type")
	}
	if !validChannelType(channelType) {
		return status.Error(codes.InvalidArgument, "channel_type must be one of: slack, email")
	}
	return nil
}

func (s *ConfigServer) UpsertNotificationConfig(ctx context.Context, req *pb.UpsertNotificationConfigRequest) (*pb.NotificationConfigResponse, error) {
	if err := validateNotificationConfigKey(req.GetRole(), req.GetEvent(), req.GetChannelType()); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.GetChannelTarget())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "channel_target is required")
	}
	var maxSeverity *int16
	if lvl := req.GetMaxSeverityLevel(); lvl != 0 {
		if lvl < 1 || lvl > 4 {
			return nil, status.Error(codes.InvalidArgument, "max_severity_level must be between 1 and 4, or 0 for unset")
		}
		v := int16(lvl)
		maxSeverity = &v
	}

	now := time.Now()
	cfg := &store.NotificationConfig{
		Role:             store.OrgRole(req.GetRole()),
		Event:            req.GetEvent(),
		ChannelType:      store.NotificationChannelType(req.GetChannelType()),
		ChannelTarget:    target,
		MaxSeverityLevel: maxSeverity,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.notificationConfigs.Upsert(ctx, cfg); err != nil {
		return nil, internalError(ctx, "failed to upsert notification config", err)
	}

	slog.InfoContext(ctx, "notification config updated",
		"actor", callerID(ctx), "role", cfg.Role, "event", cfg.Event, "channel_type", cfg.ChannelType)

	return notificationConfigToProto(cfg), nil
}

func (s *ConfigServer) DeleteNotificationConfig(ctx context.Context, req *pb.DeleteNotificationConfigRequest) (*emptypb.Empty, error) {
	if err := validateNotificationConfigKey(req.GetRole(), req.GetEvent(), req.GetChannelType()); err != nil {
		return nil, err
	}
	if err := s.notificationConfigs.Delete(ctx, store.OrgRole(req.GetRole()), req.GetEvent(), store.NotificationChannelType(req.GetChannelType())); err != nil {
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

func notificationConfigToProto(c *store.NotificationConfig) *pb.NotificationConfigResponse {
	resp := &pb.NotificationConfigResponse{
		Role:          string(c.Role),
		Event:         c.Event,
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
