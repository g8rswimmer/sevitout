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

type testAuditServer struct {
	server *grpchandler.AuditServer
	audit  *memory.AuditStore
	sevs   *memory.SEVStore
	access *memory.SEVAccessStore
}

func newTestAuditServer() *testAuditServer {
	audit := memory.NewAuditStore()
	sevs := memory.NewSEVStore()
	access := memory.NewSEVAccessStore()
	return &testAuditServer{
		server: grpchandler.NewAuditServer(audit, sevs, access),
		audit:  audit,
		sevs:   sevs,
		access: access,
	}
}

func seedSEVForAudit(t *testing.T, ts *testAuditServer, sensitive bool) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title: "Audit test SEV", SeverityLevel: 2, Status: store.SEVStatusOpen,
		Sensitive: sensitive, CreatedBy: "user-admin", CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}
	return sv.ID
}

func TestListAuditEntries_MissingSEVID(t *testing.T) {
	ts := newTestAuditServer()

	_, err := ts.server.ListAuditEntries(context.Background(), &pb.ListAuditEntriesRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestListAuditEntries_UnknownSEV(t *testing.T) {
	ts := newTestAuditServer()

	_, err := ts.server.ListAuditEntries(context.Background(), &pb.ListAuditEntriesRequest{SevId: "no-such-sev"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound", grpcCode(err))
	}
}

func TestListAuditEntries_Empty(t *testing.T) {
	ts := newTestAuditServer()
	sevID := seedSEVForAudit(t, ts, false)

	resp, err := ts.server.ListAuditEntries(context.Background(), &pb.ListAuditEntriesRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if len(resp.GetEntries()) != 0 {
		t.Fatalf("want 0 entries, got %d", len(resp.GetEntries()))
	}
}

func TestListAuditEntries_ReturnsEntriesForSEV(t *testing.T) {
	ts := newTestAuditServer()
	ctx := context.Background()
	sevID := seedSEVForAudit(t, ts, false)
	otherSEVID := seedSEVForAudit(t, ts, false)

	field, old, newVal := "status", "open", "investigating"
	if err := ts.audit.Append(ctx, &store.AuditEntry{
		SEVID: sevID, UserID: "user-1", Action: "sev.status_changed",
		FieldName: &field, OldValue: &old, NewValue: &newVal, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: Append: %v", err)
	}
	if err := ts.audit.Append(ctx, &store.AuditEntry{
		SEVID: sevID, UserID: "user-2", Action: "role.assigned", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: Append second: %v", err)
	}
	// A different SEV's entries must not leak into the response.
	if err := ts.audit.Append(ctx, &store.AuditEntry{
		SEVID: otherSEVID, UserID: "user-1", Action: "sev.created", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: Append other SEV: %v", err)
	}

	resp, err := ts.server.ListAuditEntries(ctx, &pb.ListAuditEntriesRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	entries := resp.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}

	var found bool
	for _, e := range entries {
		if e.GetSevId() != sevID {
			t.Errorf("entry belongs to wrong SEV: %s", e.GetSevId())
		}
		if e.GetAction() == "sev.status_changed" {
			found = true
			if e.GetFieldName() != "status" || e.GetOldValue() != "open" || e.GetNewValue() != "investigating" {
				t.Errorf("field/old/new value did not round-trip: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("expected sev.status_changed entry not found in response")
	}
}

func TestListAuditEntries_SensitiveSEVHiddenFromCallerWithoutAccess(t *testing.T) {
	ts := newTestAuditServer()
	ctx := context.Background()
	sevID := seedSEVForAudit(t, ts, true)
	if err := ts.audit.Append(ctx, &store.AuditEntry{
		SEVID: sevID, UserID: "user-1", Action: "sev.created", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed: Append: %v", err)
	}

	viewerCtx := auth.WithUser(ctx, &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.ListAuditEntries(viewerCtx, &pb.ListAuditEntriesRequest{SevId: sevID})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound (masking existence, not PermissionDenied)", grpcCode(err))
	}
}

func TestListAuditEntries_SensitiveSEVVisibleToGrantedUser(t *testing.T) {
	ts := newTestAuditServer()
	ctx := context.Background()
	sevID := seedSEVForAudit(t, ts, true)

	if err := ts.access.Grant(ctx, &store.SEVAccess{SEVID: sevID, UserID: "user-granted", CreatedBy: "user-admin"}); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	grantedCtx := auth.WithUser(ctx, &auth.UserContext{UserID: "user-granted", OrgRole: store.OrgRoleViewer})
	if _, err := ts.server.ListAuditEntries(grantedCtx, &pb.ListAuditEntriesRequest{SevId: sevID}); err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
}
