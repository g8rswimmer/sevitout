package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
)

type AuditServer struct {
	pb.UnimplementedAuditServiceServer
	audit store.AuditStore
}

func NewAuditServer(audit store.AuditStore) *AuditServer {
	return &AuditServer{audit: audit}
}

func (s *AuditServer) ListAuditEntries(ctx context.Context, req *pb.ListAuditEntriesRequest) (*pb.ListAuditEntriesResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	entries, err := s.audit.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list audit entries")
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
