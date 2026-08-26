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
