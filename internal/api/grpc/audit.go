package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// AuditServer implements pb.AuditServiceServer.
type AuditServer struct {
	pb.UnimplementedAuditServiceServer
	audit  store.AuditStore
	sevs   store.SEVStore
	access store.SEVAccessStore
}

// NewAuditServer returns an AuditServer backed by the given stores.
func NewAuditServer(audit store.AuditStore, sevs store.SEVStore, access store.SEVAccessStore) *AuditServer {
	return &AuditServer{audit: audit, sevs: sevs, access: access}
}

func (s *AuditServer) ListAuditEntries(ctx context.Context, req *pb.ListAuditEntriesRequest) (*pb.ListAuditEntriesResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	if _, err := loadVisibleSEV(ctx, s.sevs, s.access, req.GetSevId()); err != nil {
		return nil, err
	}

	entries, err := s.audit.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, internalError(ctx, "failed to list audit entries", err)
	}

	resp := &pb.ListAuditEntriesResponse{}
	for _, e := range entries {
		entry := &pb.AuditEntryResponse{
			Id:        e.ID,
			SevId:     e.SEVID,
			UserId:    e.UserID,
			Action:    e.Action,
			CreatedAt: timestamppb.New(e.CreatedAt),
		}
		if e.FieldName != nil {
			entry.FieldName = *e.FieldName
		}
		if e.OldValue != nil {
			entry.OldValue = *e.OldValue
		}
		if e.NewValue != nil {
			entry.NewValue = *e.NewValue
		}
		resp.Entries = append(resp.Entries, entry)
	}
	return resp, nil
}
