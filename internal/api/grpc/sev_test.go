package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// testSEVServer groups a SEVServer with its backing in-memory stores.
type testSEVServer struct {
	server  *grpchandler.SEVServer
	sevs    *memory.SEVStore
	audit   *memory.AuditStore
	history *memory.StatusHistoryStore
}

// newTestSEVServer returns a fresh SEVServer backed by empty in-memory stores.
func newTestSEVServer() *testSEVServer {
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	history := memory.NewStatusHistoryStore()
	return &testSEVServer{
		server:  grpchandler.NewSEVServer(sevs, audit, history),
		sevs:    sevs,
		audit:   audit,
		history: history,
	}
}

// seedSEV inserts a SEV directly into the backing store with a known, non-empty
// ID.  Use this for tests that need to call handlers (e.g. TransitionStatus)
// that validate the ID field; the CreateSEV handler does not assign IDs, so
// the in-memory store uses whatever ID is already on the record.
func seedSEV(t *testing.T, ts *testSEVServer, id string) {
	t.Helper()
	now := time.Now()
	if err := ts.sevs.Create(context.Background(), &store.SEV{
		ID:            id,
		Title:         "Seeded SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seedSEV(%q): %v", id, err)
	}
}

// grpcCode extracts the gRPC status code from an error returned by a handler.
func grpcCode(err error) codes.Code {
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}

// ── CreateSEV ─────────────────────────────────────────────────────────────────

func TestCreateSEV_ValidRequest(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Database failure",
		SeverityLevel: 2,
		CreatedBy:     "user-1",
		Description:   "Primary DB is unresponsive",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}
	if resp.GetStatus() != string(store.SEVStatusOpen) {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), store.SEVStatusOpen)
	}
	if resp.GetTitle() != "Database failure" {
		t.Errorf("Title = %q, want %q", resp.GetTitle(), "Database failure")
	}
	if resp.GetSeverityLevel() != 2 {
		t.Errorf("SeverityLevel = %d, want 2", resp.GetSeverityLevel())
	}
	if resp.GetCreatedBy() != "user-1" {
		t.Errorf("CreatedBy = %q, want %q", resp.GetCreatedBy(), "user-1")
	}
	if resp.GetDescription() != "Primary DB is unresponsive" {
		t.Errorf("Description = %q, want %q", resp.GetDescription(), "Primary DB is unresponsive")
	}
}

func TestCreateSEV_MissingTitle(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		SeverityLevel: 1,
		CreatedBy:     "user-1",
		// Title intentionally omitted
	})
	if err == nil {
		t.Fatal("CreateSEV: want error for missing title, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestCreateSEV_SeverityLevelZero(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Test SEV",
		SeverityLevel: 0, // proto default — invalid; must be 1-4
		CreatedBy:     "user-1",
	})
	if err == nil {
		t.Fatal("CreateSEV: want error for severity_level 0, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

// ── GetSEV ────────────────────────────────────────────────────────────────────

func TestGetSEV_AfterCreate(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Network outage",
		SeverityLevel: 1,
		CreatedBy:     "user-1",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	// Use the ID returned by Create (the in-memory store uses whatever ID is
	// on the record; with the current handler this will be the empty string).
	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetSEV: %v", err)
	}
	if got.GetTitle() != "Network outage" {
		t.Errorf("Title = %q, want %q", got.GetTitle(), "Network outage")
	}
	if got.GetStatus() != string(store.SEVStatusOpen) {
		t.Errorf("Status = %q, want %q", got.GetStatus(), store.SEVStatusOpen)
	}
}

func TestGetSEV_UnknownID(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: "does-not-exist"})
	if err == nil {
		t.Fatal("GetSEV: want error for unknown ID, got nil")
	}
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound", code)
	}
}

// ── UpdateSEV ─────────────────────────────────────────────────────────────────

func TestUpdateSEV_Title(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	const sevID = "SEV-2026-0001"
	seedSEV(t, ts, sevID)

	resp, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{
		Id:    sevID,
		Title: "Updated title",
	})
	if err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}
	if resp.GetTitle() != "Updated title" {
		t.Errorf("response Title = %q, want %q", resp.GetTitle(), "Updated title")
	}

	// Verify the change is persisted — read it back through the handler.
	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: sevID})
	if err != nil {
		t.Fatalf("GetSEV after update: %v", err)
	}
	if got.GetTitle() != "Updated title" {
		t.Errorf("persisted Title = %q, want %q", got.GetTitle(), "Updated title")
	}
}

// ── ListSEVs ──────────────────────────────────────────────────────────────────

func TestListSEVs_Empty(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.ListSEVs(ctx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 0 {
		t.Errorf("len(SEVs) = %d, want 0", len(resp.GetSevs()))
	}
}

func TestListSEVs_AfterCreate(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Memory leak",
		SeverityLevel: 3,
		CreatedBy:     "user-1",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	resp, err := ts.server.ListSEVs(ctx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 1 {
		t.Errorf("len(SEVs) = %d, want 1", len(resp.GetSevs()))
	}
	if resp.GetTotal() != 1 {
		t.Errorf("Total = %d, want 1", resp.GetTotal())
	}
	if len(resp.GetSevs()) > 0 && resp.GetSevs()[0].GetTitle() != "Memory leak" {
		t.Errorf("SEV[0].Title = %q, want %q", resp.GetSevs()[0].GetTitle(), "Memory leak")
	}
}

// ── TransitionStatus ──────────────────────────────────────────────────────────

func TestTransitionStatus_OpenToInvestigating(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	const sevID = "SEV-2026-0001"
	seedSEV(t, ts, sevID)

	resp, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       sevID,
		ToStatus: string(store.SEVStatusInvestigating),
		UserId:   "user-1",
	})
	if err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}
	if resp.GetStatus() != string(store.SEVStatusInvestigating) {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), store.SEVStatusInvestigating)
	}
}

func TestTransitionStatus_InvalidTransition(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	const sevID = "SEV-2026-0002"
	seedSEV(t, ts, sevID) // starts at Open

	// Open → Resolved is not an allowed transition.
	_, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       sevID,
		ToStatus: string(store.SEVStatusResolved),
		UserId:   "user-1",
	})
	if err == nil {
		t.Fatal("TransitionStatus: want error for invalid transition Open→Resolved, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestTransitionStatus_UnknownSEV(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       "no-such-sev",
		ToStatus: string(store.SEVStatusInvestigating),
		UserId:   "user-1",
	})
	if err == nil {
		t.Fatal("TransitionStatus: want error for unknown SEV, got nil")
	}
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound", code)
	}
}

func TestTransitionStatus_AuditEntryCreated(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	const sevID = "SEV-2026-0003"
	seedSEV(t, ts, sevID)

	_, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       sevID,
		ToStatus: string(store.SEVStatusInvestigating),
		UserId:   "user-1",
	})
	if err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	entries, err := ts.audit.ListBySEVID(ctx, sevID)
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("audit log is empty after TransitionStatus, want at least one entry")
	}

	found := false
	for _, e := range entries {
		if e.Action == "sev.status_transitioned" {
			found = true
			if e.SEVID != sevID {
				t.Errorf("audit entry SEVID = %q, want %q", e.SEVID, sevID)
			}
			break
		}
	}
	if !found {
		t.Errorf("no audit entry with action %q found in %d entries", "sev.status_transitioned", len(entries))
	}
}
