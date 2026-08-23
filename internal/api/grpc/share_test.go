package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/share"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testShareServer struct {
	server *grpchandler.ShareServer
	shares *memory.ShareStore
	sevs   *memory.SEVStore
	audit  *memory.AuditStore
	signer *share.Signer
}

func newTestShareServer() *testShareServer {
	shares := memory.NewShareStore()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	signer := share.NewSigner("test-secret")
	return &testShareServer{
		server: grpchandler.NewShareServer(shares, sevs, audit, signer),
		shares: shares,
		sevs:   sevs,
		audit:  audit,
		signer: signer,
	}
}

func seedSEVForShare(t *testing.T, ts *testShareServer, sensitive bool) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Shareable SEV",
		SeverityLevel: 2,
		Status:        store.SEVStatusOpen,
		Sensitive:     sensitive,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForShare: %v", err)
	}
	return sv.ID
}

// ── CreateShareLink ───────────────────────────────────────────────────────────

func TestCreateShareLink_Valid(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)

	resp, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{
		SevId:     sevID,
		CreatedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	if resp.GetToken() == "" {
		t.Error("token should not be empty")
	}
	if resp.GetPath() != "/s/"+resp.GetToken() {
		t.Errorf("path = %q, want /s/%s", resp.GetPath(), resp.GetToken())
	}
	if resp.GetExpiresAt() == nil {
		t.Error("expires_at should be set")
	}
	if resp.GetRevoked() {
		t.Error("revoked should be false on creation")
	}
}

func TestCreateShareLink_DefaultExpiry(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)

	before := time.Now()
	resp, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	expiresAt := resp.GetExpiresAt().AsTime()
	want := before.AddDate(0, 0, 30)
	if expiresAt.Before(want.Add(-time.Minute)) || expiresAt.After(want.Add(time.Minute)) {
		t.Errorf("expires_at = %v, want ~30 days from now (%v)", expiresAt, want)
	}
}

func TestCreateShareLink_CustomExpiry(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)

	resp, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{
		SevId: sevID, ExpiresInDays: 7,
	})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}
	got := resp.GetExpiresAt().AsTime()
	want := time.Now().AddDate(0, 0, 7)
	if got.Before(want.Add(-time.Minute)) || got.After(want.Add(time.Minute)) {
		t.Errorf("expires_at = %v, want ~7 days from now (%v)", got, want)
	}
}

func TestCreateShareLink_SensitiveSEVBlocked(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, true)

	_, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: sevID})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("want FailedPrecondition for sensitive SEV, got %v", grpcCode(err))
	}
}

func TestCreateShareLink_MissingSEVID(t *testing.T) {
	ts := newTestShareServer()
	_, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateShareLink_SEVNotFound(t *testing.T) {
	ts := newTestShareServer()
	_, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: "SEV-9999-0001"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestCreateShareLink_AuditEntryCreated(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)

	_, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: sevID, CreatedBy: "user-1"})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}

	entries, _ := ts.audit.ListBySEVID(context.Background(), sevID)
	found := false
	for _, e := range entries {
		if e.Action == "sev.share_link_created" {
			found = true
		}
	}
	if !found {
		t.Error("no audit entry with action sev.share_link_created")
	}
}

// ── RevokeShareLink ───────────────────────────────────────────────────────────

func TestRevokeShareLink_Valid(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)
	created, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}

	_, err = ts.server.RevokeShareLink(context.Background(), &pb.RevokeShareLinkRequest{
		SevId: sevID, Token: created.GetToken(),
	})
	if err != nil {
		t.Fatalf("RevokeShareLink: %v", err)
	}

	link, err := ts.shares.GetByToken(context.Background(), created.GetToken())
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if !link.Revoked {
		t.Error("link should be revoked")
	}
}

func TestRevokeShareLink_MissingSEVID(t *testing.T) {
	ts := newTestShareServer()
	_, err := ts.server.RevokeShareLink(context.Background(), &pb.RevokeShareLinkRequest{Token: "tok"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestRevokeShareLink_MissingToken(t *testing.T) {
	ts := newTestShareServer()
	_, err := ts.server.RevokeShareLink(context.Background(), &pb.RevokeShareLinkRequest{SevId: "SEV-2026-0001"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestRevokeShareLink_NotFound(t *testing.T) {
	ts := newTestShareServer()
	_, err := ts.server.RevokeShareLink(context.Background(), &pb.RevokeShareLinkRequest{
		SevId: "SEV-2026-0001", Token: "does-not-exist",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestRevokeShareLink_TokenSEVMismatch(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)
	otherSEVID := seedSEVForShare(t, ts, false)
	created, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}

	_, err = ts.server.RevokeShareLink(context.Background(), &pb.RevokeShareLinkRequest{
		SevId: otherSEVID, Token: created.GetToken(),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for sev_id/token mismatch, got %v", grpcCode(err))
	}
}

func TestRevokeShareLink_AuditEntryCreated(t *testing.T) {
	ts := newTestShareServer()
	sevID := seedSEVForShare(t, ts, false)
	created, err := ts.server.CreateShareLink(context.Background(), &pb.CreateShareLinkRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("CreateShareLink: %v", err)
	}

	_, err = ts.server.RevokeShareLink(context.Background(), &pb.RevokeShareLinkRequest{SevId: sevID, Token: created.GetToken()})
	if err != nil {
		t.Fatalf("RevokeShareLink: %v", err)
	}

	entries, _ := ts.audit.ListBySEVID(context.Background(), sevID)
	found := false
	for _, e := range entries {
		if e.Action == "sev.share_link_revoked" {
			found = true
		}
	}
	if !found {
		t.Error("no audit entry with action sev.share_link_revoked")
	}
}
