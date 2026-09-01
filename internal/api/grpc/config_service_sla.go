package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// ── Per-service SLA targets (docs/roadmap.md Phase 12) ──────────────────────

func (s *ConfigServer) GetServiceSLA(ctx context.Context, req *pb.GetServiceSLARequest) (*pb.ServiceSLAResponse, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	sla, err := s.serviceSLAs.Get(ctx, req.GetServiceId(), int16(req.GetSeverityLevel()))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SLA not configured for this service and severity level")
		}
		return nil, internalError(ctx, "failed to get service SLA", err)
	}
	return serviceSLAToProto(sla), nil
}

func (s *ConfigServer) UpsertServiceSLA(ctx context.Context, req *pb.UpsertServiceSLARequest) (*pb.ServiceSLAResponse, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	// service_id must reference a real service — an SLA on an unregistered
	// service could never be resolved by MostStrictSLA (which only looks up
	// rows keyed by the service IDs a SEV actually attaches), so allowing it
	// here would just create a silently-dead row.
	if _, err := s.services.Get(ctx, req.GetServiceId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "service not found")
		}
		return nil, internalError(ctx, "failed to look up service", err)
	}

	now := time.Now()
	sla := &store.ServiceSLA{
		ServiceID:     req.GetServiceId(),
		SeverityLevel: int16(req.GetSeverityLevel()),
		// A field left at 0 clears that metric's target — full-replace, like
		// UpdateRetentionConfigRequest, not a sparse patch.
		MTTDTargetSeconds: nonZeroTarget(req.GetMttdTargetSeconds()),
		MTTMTargetSeconds: nonZeroTarget(req.GetMttmTargetSeconds()),
		MTTRTargetSeconds: nonZeroTarget(req.GetMttrTargetSeconds()),
		RTPCTargetSeconds: nonZeroTarget(req.GetRtpcTargetSeconds()),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.serviceSLAs.Upsert(ctx, sla); err != nil {
		return nil, internalError(ctx, "failed to upsert service SLA", err)
	}

	slog.InfoContext(ctx, "service SLA updated",
		"actor", callerID(ctx), "service_id", sla.ServiceID, "severity_level", sla.SeverityLevel)

	return serviceSLAToProto(sla), nil
}

func (s *ConfigServer) DeleteServiceSLA(ctx context.Context, req *pb.DeleteServiceSLARequest) (*emptypb.Empty, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	if err := validateSeverityLevel(req.GetSeverityLevel()); err != nil {
		return nil, err
	}
	if err := s.serviceSLAs.Delete(ctx, req.GetServiceId(), int16(req.GetSeverityLevel())); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SLA not configured for this service and severity level")
		}
		return nil, internalError(ctx, "failed to delete service SLA", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListServiceSLAs(ctx context.Context, req *pb.ListServiceSLAsRequest) (*pb.ListServiceSLAsResponse, error) {
	if req.GetServiceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "service_id is required")
	}
	slas, err := s.serviceSLAs.ListByService(ctx, req.GetServiceId())
	if err != nil {
		return nil, internalError(ctx, "failed to list service SLAs", err)
	}
	resp := &pb.ListServiceSLAsResponse{}
	for _, sla := range slas {
		resp.Slas = append(resp.Slas, serviceSLAToProto(sla))
	}
	return resp, nil
}

// nonZeroTarget converts a proto3 int64 (0 = unset) to the store's nullable
// pointer representation.
func nonZeroTarget(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func serviceSLAToProto(sla *store.ServiceSLA) *pb.ServiceSLAResponse {
	resp := &pb.ServiceSLAResponse{
		ServiceId:     sla.ServiceID,
		SeverityLevel: int32(sla.SeverityLevel),
		CreatedAt:     timestamppb.New(sla.CreatedAt),
		UpdatedAt:     timestamppb.New(sla.UpdatedAt),
	}
	if sla.MTTDTargetSeconds != nil {
		resp.MttdTargetSeconds = *sla.MTTDTargetSeconds
	}
	if sla.MTTMTargetSeconds != nil {
		resp.MttmTargetSeconds = *sla.MTTMTargetSeconds
	}
	if sla.MTTRTargetSeconds != nil {
		resp.MttrTargetSeconds = *sla.MTTRTargetSeconds
	}
	if sla.RTPCTargetSeconds != nil {
		resp.RtpcTargetSeconds = *sla.RTPCTargetSeconds
	}
	return resp
}
