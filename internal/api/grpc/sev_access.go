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

// SEVAccessServer implements pb.SEVAccessServiceServer — explicit per-user
// visibility grants for SEVs flagged Sensitive (§14).
type SEVAccessServer struct {
	pb.UnimplementedSEVAccessServiceServer
	access store.SEVAccessStore
	sevs   store.SEVStore
	audit  store.AuditStore
}

// NewSEVAccessServer returns a SEVAccessServer backed by the given stores.
func NewSEVAccessServer(access store.SEVAccessStore, sevs store.SEVStore, audit store.AuditStore) *SEVAccessServer {
	return &SEVAccessServer{access: access, sevs: sevs, audit: audit}
}

func (s *SEVAccessServer) GrantAccess(ctx context.Context, req *pb.GrantAccessRequest) (*pb.SEVAccessResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	// Granting access doesn't expose the SEV's content — only that a grant
	// record now exists for this ID — so this deliberately doesn't call
	// sensitiveSEVVisible on the caller: an Incident Commander needs to be
	// able to grant access to a Sensitive SEV they were told about
	// out-of-band without first needing visibility into it themselves.
	if _, err := s.sevs.Get(ctx, req.GetSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	grant := &store.SEVAccess{
		SEVID:     req.GetSevId(),
		UserID:    req.GetUserId(),
		CreatedAt: now,
		CreatedBy: callerID,
	}
	if err := s.access.Grant(ctx, grant); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Error(codes.AlreadyExists, "access already granted")
		}
		return nil, internalError(ctx, "failed to grant access", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "sev.access_granted",
		NewValue:  strPtr(req.GetUserId()),
		CreatedAt: now,
	})

	return sevAccessToProto(grant), nil
}

func (s *SEVAccessServer) RevokeAccess(ctx context.Context, req *pb.RevokeAccessRequest) (*emptypb.Empty, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	if _, err := s.sevs.Get(ctx, req.GetSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	if err := s.access.Revoke(ctx, req.GetSevId(), req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "access grant not found")
		}
		return nil, internalError(ctx, "failed to revoke access", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "sev.access_revoked",
		CreatedAt: time.Now(),
	})

	return &emptypb.Empty{}, nil
}

func (s *SEVAccessServer) ListAccess(ctx context.Context, req *pb.ListAccessRequest) (*pb.ListAccessResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	// Unlike Grant/Revoke, listing the grants themselves reveals who can see
	// a Sensitive SEV — the same information GetSEV's masking protects, so
	// this applies the identical check: a non-allowed Viewer gets NotFound
	// rather than a chance to enumerate the access list of a SEV they can't
	// otherwise see.
	visible, err := sensitiveSEVVisible(ctx, s.access, sevRecord)
	if err != nil {
		return nil, internalError(ctx, "failed to check SEV visibility", err)
	}
	if !visible {
		return nil, status.Error(codes.NotFound, "SEV not found")
	}

	grants, err := s.access.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, internalError(ctx, "failed to list access grants", err)
	}

	resp := &pb.ListAccessResponse{}
	for _, g := range grants {
		resp.Access = append(resp.Access, sevAccessToProto(g))
	}
	return resp, nil
}

func sevAccessToProto(a *store.SEVAccess) *pb.SEVAccessResponse {
	return &pb.SEVAccessResponse{
		Id:        a.ID,
		SevId:     a.SEVID,
		UserId:    a.UserID,
		CreatedAt: timestamppb.New(a.CreatedAt),
		CreatedBy: a.CreatedBy,
	}
}
