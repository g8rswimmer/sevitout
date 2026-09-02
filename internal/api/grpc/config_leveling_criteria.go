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

// ── Per-service SEV leveling criteria (docs/roadmap.md Phase 14) ────────────

func (s *ConfigServer) GetLevelingCriteria(ctx context.Context, req *pb.GetLevelingCriteriaRequest) (*pb.LevelingCriteriaResponse, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	c, err := s.levelingCriteria.Get(ctx, req.GetServiceId(), int16(req.GetSeverityLevel()))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "leveling criteria not configured for this service and severity level")
		}
		return nil, internalError(ctx, "failed to get leveling criteria", err)
	}
	return levelingCriteriaToProto(c), nil
}

func (s *ConfigServer) UpsertLevelingCriteria(ctx context.Context, req *pb.UpsertLevelingCriteriaRequest) (*pb.LevelingCriteriaResponse, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	criteria := strings.TrimSpace(req.GetCriteria())
	if criteria == "" {
		// Unlike UpsertServiceSLA's zero-clears-a-field semantics, an empty
		// criteria submission is rejected outright rather than treated as
		// "clear this row" — criteria is NOT NULL (a row only exists when
		// there's guidance to show); clearing existing guidance is
		// DeleteLevelingCriteria's job.
		return nil, status.Error(codes.InvalidArgument, "criteria is required")
	}
	// service_id must reference a real service — an orphaned criteria row
	// could never be resolved by ListForServices, same reasoning
	// UpsertServiceSLA gives.
	if _, err := s.services.Get(ctx, req.GetServiceId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, internalError(ctx, "failed to look up service", err)
	}

	now := time.Now()
	c := &store.ServiceLevelingCriteria{
		ServiceID:     req.GetServiceId(),
		SeverityLevel: int16(req.GetSeverityLevel()),
		Criteria:      criteria,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.levelingCriteria.Upsert(ctx, c); err != nil {
		return nil, internalError(ctx, "failed to upsert leveling criteria", err)
	}

	slog.InfoContext(ctx, "service leveling criteria updated",
		"actor", callerID(ctx), "service_id", c.ServiceID, "severity_level", c.SeverityLevel)

	return levelingCriteriaToProto(c), nil
}

func (s *ConfigServer) DeleteLevelingCriteria(ctx context.Context, req *pb.DeleteLevelingCriteriaRequest) (*emptypb.Empty, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	if err := s.levelingCriteria.Delete(ctx, req.GetServiceId(), int16(req.GetSeverityLevel())); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "leveling criteria not configured for this service and severity level")
		}
		return nil, internalError(ctx, "failed to delete leveling criteria", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListLevelingCriteria(ctx context.Context, req *pb.ListLevelingCriteriaRequest) (*pb.ListLevelingCriteriaResponse, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	rows, err := s.levelingCriteria.ListByService(ctx, req.GetServiceId())
	if err != nil {
		return nil, internalError(ctx, "failed to list leveling criteria", err)
	}
	resp := &pb.ListLevelingCriteriaResponse{}
	for _, c := range rows {
		resp.Criteria = append(resp.Criteria, levelingCriteriaToProto(c))
	}
	return resp, nil
}

// ListLevelingCriteriaForServices is a pure read spanning multiple services —
// no service-existence check is needed; a service ID with no configured row
// is silently omitted from the result, same posture as
// ServiceSLAStore.ListForServices.
func (s *ConfigServer) ListLevelingCriteriaForServices(ctx context.Context, req *pb.ListLevelingCriteriaForServicesRequest) (*pb.ListLevelingCriteriaForServicesResponse, error) {
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	rows, err := s.levelingCriteria.ListForServices(ctx, req.GetServiceIds(), int16(req.GetSeverityLevel()))
	if err != nil {
		return nil, internalError(ctx, "failed to list leveling criteria for services", err)
	}
	resp := &pb.ListLevelingCriteriaForServicesResponse{}
	for _, c := range rows {
		resp.Criteria = append(resp.Criteria, levelingCriteriaToProto(c))
	}
	return resp, nil
}

func levelingCriteriaToProto(c *store.ServiceLevelingCriteria) *pb.LevelingCriteriaResponse {
	return &pb.LevelingCriteriaResponse{
		ServiceId:     c.ServiceID,
		SeverityLevel: int32(c.SeverityLevel),
		Criteria:      c.Criteria,
		CreatedAt:     timestamppb.New(c.CreatedAt),
		UpdatedAt:     timestamppb.New(c.UpdatedAt),
	}
}
