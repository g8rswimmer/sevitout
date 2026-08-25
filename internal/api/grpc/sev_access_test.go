package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testSEVAccessServer struct {
	server *grpchandler.SEVAccessServer
	access *memory.SEVAccessStore
	sevs   *memory.SEVStore
	audit  *memory.AuditStore
}

func newTestSEVAccessServer() *testSEVAccessServer {
	access := memory.NewSEVAccessStore()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	return &testSEVAccessServer{
		server: grpchandler.NewSEVAccessServer(access, sevs, audit),
		access: access,
		sevs:   sevs,
		audit:  audit,
	}
}

func seedSEVForAccess(t *testing.T, ts *testSEVAccessServer, sensitive bool) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Seeded SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		Sensitive:     sensitive,
		CreatedBy:     "user-reporter",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForAccess: %v", err)
	}
	return sv.ID
}

// ── GrantAccess ───────────────────────────────────────────────────────────────

func TestGrantAccess_Valid(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-ic", OrgRole: store.OrgRoleIncidentCommander})
	sevID := seedSEVForAccess(t, ts, true)

	resp, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{SevId: sevID, UserId: "user-viewer"})
	if err != nil {
		t.Fatalf("GrantAccess: %v", err)
	}
	if resp.GetUserId() != "user-viewer" {
		t.Errorf("user_id = %q, want user-viewer", resp.GetUserId())
	}
	if resp.GetCreatedBy() != "user-ic" {
		t.Errorf("created_by = %q, want user-ic", resp.GetCreatedBy())
	}

	ok, _ := ts.access.HasAccess(ctx, sevID, "user-viewer")
	if !ok {
		t.Fatal("expected grant to be persisted")
	}
}

func TestGrantAccess_UnknownSEV(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := context.Background()

	_, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{SevId: "SEV-does-not-exist", UserId: "user-viewer"})
	if grpcCode(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestGrantAccess_Duplicate(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := context.Background()
	sevID := seedSEVForAccess(t, ts, true)

	if _, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{SevId: sevID, UserId: "user-viewer"}); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	_, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{SevId: sevID, UserId: "user-viewer"})
	if grpcCode(err) != codes.AlreadyExists {
		t.Fatalf("want AlreadyExists, got %v", err)
	}
}

func TestGrantAccess_MissingFields(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := context.Background()
	sevID := seedSEVForAccess(t, ts, true)

	if _, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{UserId: "user-viewer"}); grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("missing sev_id: want InvalidArgument, got %v", err)
	}
	if _, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{SevId: sevID}); grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("missing user_id: want InvalidArgument, got %v", err)
	}
}

// ── RevokeAccess ──────────────────────────────────────────────────────────────

func TestRevokeAccess_Valid(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := context.Background()
	sevID := seedSEVForAccess(t, ts, true)

	grantResp, err := ts.server.GrantAccess(ctx, &pb.GrantAccessRequest{SevId: sevID, UserId: "user-viewer"})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	if _, err := ts.server.RevokeAccess(ctx, &pb.RevokeAccessRequest{SevId: sevID, Id: grantResp.GetId()}); err != nil {
		t.Fatalf("RevokeAccess: %v", err)
	}

	ok, _ := ts.access.HasAccess(ctx, sevID, "user-viewer")
	if ok {
		t.Fatal("expected access to be revoked")
	}
}

func TestRevokeAccess_NotFound(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := context.Background()
	sevID := seedSEVForAccess(t, ts, true)

	_, err := ts.server.RevokeAccess(ctx, &pb.RevokeAccessRequest{SevId: sevID, Id: 9999})
	if grpcCode(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestRevokeAccess_UnknownSEV(t *testing.T) {
	ts := newTestSEVAccessServer()
	ctx := context.Background()

	_, err := ts.server.RevokeAccess(ctx, &pb.RevokeAccessRequest{SevId: "SEV-does-not-exist", Id: 1})
	if grpcCode(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

// ── ListAccess ────────────────────────────────────────────────────────────────

func TestListAccess_VisibleToGrantedUser(t *testing.T) {
	ts := newTestSEVAccessServer()
	adminCtx := context.Background()
	sevID := seedSEVForAccess(t, ts, true)
	if _, err := ts.server.GrantAccess(adminCtx, &pb.GrantAccessRequest{SevId: sevID, UserId: "user-viewer"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-viewer", OrgRole: store.OrgRoleViewer})
	resp, err := ts.server.ListAccess(viewerCtx, &pb.ListAccessRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListAccess: %v", err)
	}
	if len(resp.GetAccess()) != 1 {
		t.Fatalf("want 1 grant, got %d", len(resp.GetAccess()))
	}
}

func TestListAccess_HiddenFromNonGrantedViewer(t *testing.T) {
	ts := newTestSEVAccessServer()
	adminCtx := context.Background()
	sevID := seedSEVForAccess(t, ts, true)
	if _, err := ts.server.GrantAccess(adminCtx, &pb.GrantAccessRequest{SevId: sevID, UserId: "user-viewer"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	outsiderCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.ListAccess(outsiderCtx, &pb.ListAccessRequest{SevId: sevID})
	if grpcCode(err) != codes.NotFound {
		t.Fatalf("want NotFound (masking existence), got %v", err)
	}
}

func TestListAccess_VisibleToAdminWithoutGrant(t *testing.T) {
	ts := newTestSEVAccessServer()
	sevID := seedSEVForAccess(t, ts, true)

	adminCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-admin", OrgRole: store.OrgRoleAdmin})
	if _, err := ts.server.ListAccess(adminCtx, &pb.ListAccessRequest{SevId: sevID}); err != nil {
		t.Fatalf("ListAccess as admin: %v", err)
	}
}
