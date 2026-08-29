package grpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

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
		return nil, internalError(ctx, "failed to create on-call rotation", err)
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
		return nil, internalError(ctx, "failed to get on-call rotation", err)
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
		return nil, internalError(ctx, "failed to get on-call rotation", err)
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
		return nil, internalError(ctx, "failed to update on-call rotation", err)
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
		return nil, internalError(ctx, "failed to delete on-call rotation", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ConfigServer) ListOnCallRotations(ctx context.Context, _ *pb.ListOnCallRotationsRequest) (*pb.ListOnCallRotationsResponse, error) {
	rotations, err := s.oncall.List(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to list on-call rotations", err)
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
