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

// ChatServer implements pb.ChatServiceServer.
type ChatServer struct {
	pb.UnimplementedChatServiceServer
	chat      store.ChatStore
	sevs      store.SEVStore
	access    store.SEVAccessStore
	publisher Publisher // nil when WebSocket support is not wired up
}

// NewChatServer returns a ChatServer backed by the given stores.
func NewChatServer(chat store.ChatStore, sevs store.SEVStore, access store.SEVAccessStore, publisher Publisher) *ChatServer {
	return &ChatServer{chat: chat, sevs: sevs, access: access, publisher: publisher}
}

func (s *ChatServer) AddChatEntry(ctx context.Context, req *pb.AddChatEntryRequest) (*pb.ChatEntryResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetContent() == "" {
		return nil, status.Error(codes.InvalidArgument, "content is required")
	}
	if req.GetAuthor() == "" {
		return nil, status.Error(codes.InvalidArgument, "author is required")
	}
	if req.GetSource() == "" {
		return nil, status.Error(codes.InvalidArgument, "source is required")
	}

	sevRecord, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	addedBy := req.GetAddedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		addedBy = uc.UserID
	}
	if addedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "added_by is required")
	}

	now := time.Now()
	occurredAt := now
	if req.GetOccurredAt() != nil {
		occurredAt = req.GetOccurredAt().AsTime()
	}

	entry := &store.ChatEntry{
		SEVID:      req.GetSevId(),
		OccurredAt: occurredAt,
		Source:     req.GetSource(),
		Author:     req.GetAuthor(),
		Content:    req.GetContent(),
		AddedAt:    now,
		AddedBy:    addedBy,
	}

	if err := s.chat.Create(ctx, entry); err != nil {
		return nil, internalError(ctx, "failed to add chat entry", err)
	}

	resp := chatEntryToProto(entry)
	if !sevRecord.Sensitive {
		publishProto(s.publisher, entry.SEVID, "chat.created", resp)
	}

	return resp, nil
}

func (s *ChatServer) ListChatEntries(ctx context.Context, req *pb.ListChatEntriesRequest) (*pb.ListChatEntriesResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	if _, err := loadVisibleSEV(ctx, s.sevs, s.access, req.GetSevId()); err != nil {
		return nil, err
	}

	entries, err := s.chat.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, internalError(ctx, "failed to list chat entries", err)
	}

	resp := &pb.ListChatEntriesResponse{}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, chatEntryToProto(e))
	}
	return resp, nil
}

func chatEntryToProto(e *store.ChatEntry) *pb.ChatEntryResponse {
	return &pb.ChatEntryResponse{
		Id:         e.ID,
		SevId:      e.SEVID,
		OccurredAt: timestamppb.New(e.OccurredAt),
		Source:     e.Source,
		Author:     e.Author,
		Content:    e.Content,
		AddedAt:    timestamppb.New(e.AddedAt),
		AddedBy:    e.AddedBy,
	}
}
