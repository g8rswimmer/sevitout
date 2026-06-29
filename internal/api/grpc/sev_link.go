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

// SEVLinkServer implements pb.SEVLinkServiceServer.
type SEVLinkServer struct {
	pb.UnimplementedSEVLinkServiceServer
	links store.SEVLinkStore
	sevs  store.SEVStore
	audit store.AuditStore
}

// NewSEVLinkServer returns a SEVLinkServer backed by the given stores.
func NewSEVLinkServer(links store.SEVLinkStore, sevs store.SEVStore, audit store.AuditStore) *SEVLinkServer {
	return &SEVLinkServer{links: links, sevs: sevs, audit: audit}
}

func (s *SEVLinkServer) LinkSEVs(ctx context.Context, req *pb.LinkSEVsRequest) (*pb.SEVLinkResponse, error) {
	if req.GetSourceSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "source_sev_id is required")
	}
	if req.GetTargetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_sev_id is required")
	}
	if req.GetRelationshipType() == "" {
		return nil, status.Error(codes.InvalidArgument, "relationship_type is required")
	}
	if req.GetSourceSevId() == req.GetTargetSevId() {
		return nil, status.Error(codes.InvalidArgument, "source_sev_id and target_sev_id must be different")
	}

	relType := store.SEVRelationshipType(req.GetRelationshipType())
	switch relType {
	case store.SEVRelationshipRelated, store.SEVRelationshipCausedBy,
		store.SEVRelationshipDuplicate, store.SEVRelationshipRecurrenceOf:
	default:
		return nil, status.Error(codes.InvalidArgument, "relationship_type must be one of: related, caused-by, duplicate, recurrence-of")
	}

	if _, err := s.sevs.Get(ctx, req.GetSourceSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "source SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get source SEV")
	}
	if _, err := s.sevs.Get(ctx, req.GetTargetSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "target SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get target SEV")
	}

	callerID := req.GetCreatedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	forward := &store.SEVLink{
		SourceSEVID:      req.GetSourceSevId(),
		TargetSEVID:      req.GetTargetSevId(),
		RelationshipType: relType,
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	if err := s.links.Create(ctx, forward); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Error(codes.AlreadyExists, "link already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create link")
	}

	// Bidirectional: insert the reverse direction as well.
	reverse := &store.SEVLink{
		SourceSEVID:      req.GetTargetSevId(),
		TargetSEVID:      req.GetSourceSevId(),
		RelationshipType: relType,
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	if err := s.links.Create(ctx, reverse); err != nil && !errors.Is(err, store.ErrConflict) {
		return nil, status.Error(codes.Internal, "failed to create reverse link")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSourceSevId(),
		UserID:    callerID,
		Action:    "sev.linked",
		NewValue:  strPtr(req.GetTargetSevId()),
		CreatedAt: now,
	})

	return sevLinkToProto(forward), nil
}

func (s *SEVLinkServer) UnlinkSEVs(ctx context.Context, req *pb.UnlinkSEVsRequest) (*emptypb.Empty, error) {
	if req.GetSourceSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "source_sev_id is required")
	}
	if req.GetTargetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_sev_id is required")
	}
	if req.GetRelationshipType() == "" {
		return nil, status.Error(codes.InvalidArgument, "relationship_type is required")
	}

	relType := store.SEVRelationshipType(req.GetRelationshipType())
	switch relType {
	case store.SEVRelationshipRelated, store.SEVRelationshipCausedBy,
		store.SEVRelationshipDuplicate, store.SEVRelationshipRecurrenceOf:
	default:
		return nil, status.Error(codes.InvalidArgument, "relationship_type must be one of: related, caused-by, duplicate, recurrence-of")
	}

	if err := s.links.Delete(ctx, req.GetSourceSevId(), req.GetTargetSevId(), relType); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "link not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete link")
	}

	// Remove the reverse direction; ignore not-found in case it was already cleaned up.
	_ = s.links.Delete(ctx, req.GetTargetSevId(), req.GetSourceSevId(), relType)

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}
	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSourceSevId(),
		UserID:    callerID,
		Action:    "sev.unlinked",
		OldValue:  strPtr(req.GetTargetSevId()),
		CreatedAt: time.Now(),
	})

	return &emptypb.Empty{}, nil
}

func (s *SEVLinkServer) ListLinkedSEVs(ctx context.Context, req *pb.ListLinkedSEVsRequest) (*pb.ListLinkedSEVsResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	if _, err := s.sevs.Get(ctx, req.GetSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	links, err := s.links.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list links")
	}

	resp := &pb.ListLinkedSEVsResponse{}
	for _, l := range links {
		resp.Links = append(resp.Links, sevLinkToProto(l))
	}
	return resp, nil
}

func sevLinkToProto(l *store.SEVLink) *pb.SEVLinkResponse {
	return &pb.SEVLinkResponse{
		Id:               l.ID,
		SourceSevId:      l.SourceSEVID,
		TargetSevId:      l.TargetSEVID,
		RelationshipType: string(l.RelationshipType),
		CreatedAt:        timestamppb.New(l.CreatedAt),
		CreatedBy:        l.CreatedBy,
	}
}
