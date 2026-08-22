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
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// RoleServer implements pb.RoleServiceServer.
type RoleServer struct {
	pb.UnimplementedRoleServiceServer
	roles     store.RoleStore
	sevs      store.SEVStore
	audit     store.AuditStore
	publisher Publisher // nil when WebSocket support is not wired up
}

// NewRoleServer returns a RoleServer backed by the given stores.
func NewRoleServer(roles store.RoleStore, sevs store.SEVStore, audit store.AuditStore, publisher Publisher) *RoleServer {
	return &RoleServer{roles: roles, sevs: sevs, audit: audit, publisher: publisher}
}

func (s *RoleServer) AssignRole(ctx context.Context, req *pb.AssignRoleRequest) (*pb.SEVRoleResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetRoleType() == "" {
		return nil, status.Error(codes.InvalidArgument, "role_type is required")
	}
	if req.GetDisplayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "display_name is required")
	}

	switch store.SEVRoleType(req.GetRoleType()) {
	case store.SEVRoleOnCall, store.SEVRoleDetectedBy, store.SEVRoleIncidentCommander,
		store.SEVRoleCommsLead, store.SEVRoleRecorder, store.SEVRoleResponder:
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown role_type")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	callerID := req.GetCreatedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	role := &store.SEVRole{
		SEVID:       req.GetSevId(),
		RoleType:    store.SEVRoleType(req.GetRoleType()),
		DisplayName: req.GetDisplayName(),
		CreatedAt:   now,
		CreatedBy:   callerID,
	}
	if v := req.GetUserId(); v != "" {
		role.UserID = &v
	}

	if err := s.roles.Assign(ctx, role); err != nil {
		return nil, status.Error(codes.Internal, "failed to assign role")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "role.assigned",
		CreatedAt: now,
	})

	resp := roleToProto(role)
	if !sevRecord.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "role.changed", resp)
	}

	return resp, nil
}

func (s *RoleServer) RemoveRole(ctx context.Context, req *pb.RemoveRoleRequest) (*emptypb.Empty, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	if err := s.roles.Remove(ctx, req.GetSevId(), req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "role assignment not found")
		}
		return nil, status.Error(codes.Internal, "failed to remove role")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "role.removed",
		CreatedAt: time.Now(),
	})

	if !sevRecord.Sensitive {
		publishJSON(s.publisher, req.GetSevId(), "role.changed", map[string]any{
			"id":      req.GetId(),
			"removed": true,
		})
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleServer) ListRoles(ctx context.Context, req *pb.ListRolesRequest) (*pb.ListRolesResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	if _, err := s.sevs.Get(ctx, req.GetSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	roles, err := s.roles.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list roles")
	}

	resp := &pb.ListRolesResponse{}
	for _, r := range roles {
		resp.Roles = append(resp.Roles, roleToProto(r))
	}
	return resp, nil
}

func roleToProto(r *store.SEVRole) *pb.SEVRoleResponse {
	resp := &pb.SEVRoleResponse{
		Id:          r.ID,
		SevId:       r.SEVID,
		RoleType:    string(r.RoleType),
		DisplayName: r.DisplayName,
		CreatedAt:   timestamppb.New(r.CreatedAt),
		CreatedBy:   r.CreatedBy,
	}
	if r.UserID != nil {
		resp.UserId = *r.UserID
	}
	return resp
}
