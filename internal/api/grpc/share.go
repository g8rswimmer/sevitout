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

// defaultShareExpiryDays is how long a shareable link stays valid when the
// caller doesn't specify expires_in_days (or gives a non-positive value).
const defaultShareExpiryDays = 30

// ShareSigner creates a signed, self-contained token scoped to a SEV ID and
// expiry. Declared here (the consumer) per this repo's interface-ownership
// convention — share.Signer satisfies it implicitly.
type ShareSigner interface {
	Sign(sevID string, expiresAt time.Time) (string, error)
}

// ShareServer implements pb.ShareServiceServer: creating and revoking public
// shareable links. The public view itself is served separately by
// ShareViewHandler — see share.proto's doc comment for why.
type ShareServer struct {
	pb.UnimplementedShareServiceServer
	shares store.ShareStore
	sevs   store.SEVStore
	audit  store.AuditStore
	signer ShareSigner
}

// ShareServerParams groups NewShareServer's dependencies.
type ShareServerParams struct {
	Shares store.ShareStore
	SEVs   store.SEVStore
	Audit  store.AuditStore
	Signer ShareSigner
}

// NewShareServer returns a ShareServer backed by p.
func NewShareServer(p ShareServerParams) *ShareServer {
	return &ShareServer{shares: p.Shares, sevs: p.SEVs, audit: p.Audit, signer: p.Signer}
}

func (s *ShareServer) CreateShareLink(ctx context.Context, req *pb.CreateShareLinkRequest) (*pb.ShareLinkResponse, error) {
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

	// §14.1: "Sensitive SEVs cannot have shareable links generated."
	if sev.Sensitive {
		return nil, status.Error(codes.FailedPrecondition, "sensitive SEVs cannot have shareable links generated")
	}

	days := int(req.GetExpiresInDays())
	if days <= 0 {
		days = defaultShareExpiryDays
	}
	expiresAt := time.Now().AddDate(0, 0, days)

	token, err := s.signer.Sign(req.GetSevId(), expiresAt)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to sign share token")
	}

	callerID := req.GetCreatedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	link := &store.ShareableLink{
		SEVID:     req.GetSevId(),
		Token:     token,
		CreatedBy: callerID,
		ExpiresAt: &expiresAt,
		CreatedAt: now,
	}
	if err := s.shares.Create(ctx, link); err != nil {
		return nil, status.Error(codes.Internal, "failed to create share link")
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "sev.share_link_created",
		CreatedAt: now,
	})

	return shareLinkToProto(link), nil
}

func (s *ShareServer) RevokeShareLink(ctx context.Context, req *pb.RevokeShareLinkRequest) (*emptypb.Empty, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	link, err := s.shares.GetByToken(ctx, req.GetToken())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "share link not found")
		}
		return nil, status.Error(codes.Internal, "failed to get share link")
	}
	if link.SEVID != req.GetSevId() {
		return nil, status.Error(codes.InvalidArgument, "token does not belong to sev_id")
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	if err := s.shares.Revoke(ctx, req.GetToken(), callerID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "share link not found")
		}
		return nil, status.Error(codes.Internal, "failed to revoke share link")
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "sev.share_link_revoked",
		CreatedAt: time.Now(),
	})

	return &emptypb.Empty{}, nil
}

func shareLinkToProto(l *store.ShareableLink) *pb.ShareLinkResponse {
	resp := &pb.ShareLinkResponse{
		Id:        l.ID,
		SevId:     l.SEVID,
		Token:     l.Token,
		Path:      "/s/" + l.Token,
		Revoked:   l.Revoked,
		CreatedBy: l.CreatedBy,
		CreatedAt: timestamppb.New(l.CreatedAt),
	}
	if l.ExpiresAt != nil {
		resp.ExpiresAt = timestamppb.New(*l.ExpiresAt)
	}
	return resp
}
