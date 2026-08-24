package grpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/ai"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// Unlocker signs and validates short-lived unlock tokens for locked SEVs.
// The interface is consumed here and implemented by postmortem.UnlockSigner.
type Unlocker interface {
	Sign(sevID string) (string, error)
	Validate(tokenStr, sevID string) error
}

// PostmortemServer implements pb.PostmortemServiceServer.
type PostmortemServer struct {
	pb.UnimplementedPostmortemServiceServer
	postmortems store.PostmortemStore
	sevs        store.SEVStore
	audit       store.AuditStore
	unlock      Unlocker
	publisher   Publisher    // nil when WebSocket support is not wired up
	aiDispatch  AIDispatcher // nil when no AI plugin is configured
}

// PostmortemServerParams groups NewPostmortemServer's dependencies.
// Publisher and AIDispatch may both be nil (WebSocket/AI plugin support are
// each optional at deploy time).
type PostmortemServerParams struct {
	Postmortems store.PostmortemStore
	SEVs        store.SEVStore
	Audit       store.AuditStore
	Unlock      Unlocker
	Publisher   Publisher
	AIDispatch  AIDispatcher
}

// NewPostmortemServer returns a PostmortemServer backed by the given stores.
func NewPostmortemServer(p PostmortemServerParams) *PostmortemServer {
	return &PostmortemServer{
		postmortems: p.Postmortems,
		sevs:        p.SEVs,
		audit:       p.Audit,
		unlock:      p.Unlock,
		publisher:   p.Publisher,
		aiDispatch:  p.AIDispatch,
	}
}

func (s *PostmortemServer) GetPostmortem(ctx context.Context, req *pb.GetPostmortemRequest) (*pb.PostmortemResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	pm, err := s.postmortems.GetBySEVID(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "postmortem not found")
		}
		return nil, status.Error(codes.Internal, "failed to get postmortem")
	}
	return postmortemToProto(pm), nil
}

func (s *PostmortemServer) UpdatePostmortem(ctx context.Context, req *pb.UpdatePostmortemRequest) (*pb.PostmortemResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	sev, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	if sev.Locked {
		if err := validateUnlock(s.unlock, req.GetUnlockToken(), req.GetSevId()); err != nil {
			return nil, err
		}
	}

	pm, err := s.postmortems.GetBySEVID(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "postmortem not found")
		}
		return nil, status.Error(codes.Internal, "failed to get postmortem")
	}

	callerID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	pm.Content = req.GetContent()
	pm.UpdatedAt = now
	if callerID != "" {
		pm.UpdatedBy = &callerID
	}

	if err := s.postmortems.Update(ctx, pm); err != nil {
		return nil, status.Error(codes.Internal, "failed to update postmortem")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "postmortem.updated",
		CreatedAt: now,
	})

	resp := postmortemToProto(pm)
	if !sev.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "postmortem.updated", resp)
	}

	return resp, nil
}

func (s *PostmortemServer) TransitionPostmortemStatus(ctx context.Context, req *pb.TransitionPostmortemStatusRequest) (*pb.PostmortemResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetToStatus() == "" {
		return nil, status.Error(codes.InvalidArgument, "to_status is required")
	}

	toStatus := store.PostmortemStatus(req.GetToStatus())

	pm, err := s.postmortems.GetBySEVID(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "postmortem not found")
		}
		return nil, status.Error(codes.Internal, "failed to get postmortem")
	}

	if err := postmortem.ValidateTransition(pm.Status, toStatus); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	callerID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	fromStatus := pm.Status
	pm.Status = toStatus
	pm.UpdatedAt = now
	if callerID != "" {
		pm.UpdatedBy = &callerID
	}

	if err := s.postmortems.Update(ctx, pm); err != nil {
		return nil, status.Error(codes.Internal, "failed to update postmortem")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "postmortem.status_transitioned",
		FieldName: strPtr("status"),
		OldValue:  strPtr(string(fromStatus)),
		NewValue:  strPtr(string(toStatus)),
		CreatedAt: now,
	})

	resp := postmortemToProto(pm)
	if sv, err := s.sevs.Get(ctx, req.GetSevId()); err == nil {
		if !sv.Sensitive {
			publishProto(s.publisher, req.GetSevId(), "postmortem.updated", resp)
		}
		// Sensitive/AIDisabled gating is enforced centrally by ai.Dispatcher
		// (see SEVServer.dispatchAI's doc comment) — not duplicated here.
		if toStatus == store.PostmortemStatusInReview && s.aiDispatch != nil {
			s.aiDispatch.Dispatch(ai.TriggerPostmortemInReview, req.GetSevId())
		}
	}

	return resp, nil
}

// UnlockSEV writes an unlock-reason audit entry and returns a short-lived token
// that authorizes subsequent mutations on the locked SEV.
func (s *PostmortemServer) UnlockSEV(ctx context.Context, req *pb.UnlockSEVRequest) (*pb.UnlockSEVResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}

	sev, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	if !sev.Locked {
		return nil, status.Error(codes.FailedPrecondition, "SEV is not locked")
	}

	callerID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	token, err := s.unlock.Sign(req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate unlock token")
	}

	reason := req.GetReason()
	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "sev.unlock_requested",
		NewValue:  &reason,
		CreatedAt: time.Now(),
	})

	return &pb.UnlockSEVResponse{UnlockToken: token}, nil
}

func postmortemToProto(pm *store.Postmortem) *pb.PostmortemResponse {
	resp := &pb.PostmortemResponse{
		Id:        pm.ID,
		SevId:     pm.SEVID,
		Status:    string(pm.Status),
		Content:   pm.Content,
		CreatedAt: timestamppb.New(pm.CreatedAt),
		UpdatedAt: timestamppb.New(pm.UpdatedAt),
	}
	if pm.UpdatedBy != nil {
		resp.UpdatedBy = *pm.UpdatedBy
	}
	return resp
}

func strPtr(s string) *string { return &s }
