package grpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// AnnouncementServer implements pb.AnnouncementServiceServer.
type AnnouncementServer struct {
	pb.UnimplementedAnnouncementServiceServer
	announcements store.AnnouncementStore
	sevs          store.SEVStore
	publisher     Publisher // nil when WebSocket support is not wired up
}

// NewAnnouncementServer returns an AnnouncementServer backed by the given stores.
func NewAnnouncementServer(announcements store.AnnouncementStore, sevs store.SEVStore, publisher Publisher) *AnnouncementServer {
	return &AnnouncementServer{announcements: announcements, sevs: sevs, publisher: publisher}
}

func (s *AnnouncementServer) CreateAnnouncement(ctx context.Context, req *pb.CreateAnnouncementRequest) (*pb.AnnouncementResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetMessage() == "" {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}
	if req.GetAudience() == "" {
		return nil, status.Error(codes.InvalidArgument, "audience is required")
	}

	audience := store.AudienceType(req.GetAudience())
	switch audience {
	case store.AudienceInternal, store.AudienceExternal, store.AudienceStatusPage:
	default:
		return nil, status.Error(codes.InvalidArgument, "audience must be one of: internal, external, status-page")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	authorID := req.GetAuthorId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		authorID = uc.UserID
	}
	if authorID == "" {
		return nil, status.Error(codes.InvalidArgument, "author_id is required")
	}

	a := &store.Announcement{
		SEVID:       req.GetSevId(),
		AuthorID:    authorID,
		Message:     req.GetMessage(),
		Audience:    audience,
		IsMilestone: req.GetIsMilestone(),
		CreatedAt:   time.Now(),
	}

	if err := s.announcements.Create(ctx, a); err != nil {
		return nil, status.Error(codes.Internal, "failed to create announcement")
	}

	resp := announcementToProto(a)
	if !sevRecord.Sensitive {
		publishProto(s.publisher, a.SEVID, "announcement.created", resp)
	}

	return resp, nil
}

func (s *AnnouncementServer) ListAnnouncements(ctx context.Context, req *pb.ListAnnouncementsRequest) (*pb.ListAnnouncementsResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	if _, err := s.sevs.Get(ctx, req.GetSevId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	all, err := s.announcements.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list announcements")
	}

	audienceFilter := store.AudienceType(req.GetAudience())
	if audienceFilter != "" {
		switch audienceFilter {
		case store.AudienceInternal, store.AudienceExternal, store.AudienceStatusPage:
		default:
			return nil, status.Error(codes.InvalidArgument, "audience must be one of: internal, external, status-page")
		}
	}

	resp := &pb.ListAnnouncementsResponse{}
	for _, a := range all {
		if audienceFilter != "" && a.Audience != audienceFilter {
			continue
		}
		resp.Announcements = append(resp.Announcements, announcementToProto(a))
	}
	return resp, nil
}

func announcementToProto(a *store.Announcement) *pb.AnnouncementResponse {
	return &pb.AnnouncementResponse{
		Id:          a.ID,
		SevId:       a.SEVID,
		AuthorId:    a.AuthorID,
		Message:     a.Message,
		Audience:    string(a.Audience),
		IsMilestone: a.IsMilestone,
		CreatedAt:   timestamppb.New(a.CreatedAt),
	}
}
