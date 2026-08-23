package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/ai"
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
	links   *memory.SEVLinkStore
	pub     *fakePublisher
	ai      *fakeAIDispatcher
}

// newTestSEVServer returns a fresh SEVServer backed by empty in-memory stores.
func newTestSEVServer() *testSEVServer {
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	history := memory.NewStatusHistoryStore()
	links := memory.NewSEVLinkStore()
	pub := &fakePublisher{}
	aiDispatch := &fakeAIDispatcher{}
	return &testSEVServer{
		server:  grpchandler.NewSEVServer(sevs, audit, history, memory.NewRoleStore(), memory.NewServiceStore(), memory.NewPostmortemStore(), links, nil, nil, pub, aiDispatch),
		sevs:    sevs,
		audit:   audit,
		history: history,
		links:   links,
		pub:     pub,
		ai:      aiDispatch,
	}
}

// seedSEV inserts a SEV directly into the backing store and returns the
// auto-assigned ID. Use this for tests that need a pre-existing SEV to call
// handlers such as TransitionStatus or UpdateSEV.
func seedSEV(t *testing.T, ts *testSEVServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Seeded SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEV: %v", err)
	}
	return sv.ID
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
	if resp.GetDescription() != "Primary DB is unresponsive" {
		t.Errorf("Description = %q, want %q", resp.GetDescription(), "Primary DB is unresponsive")
	}
}

func TestCreateSEV_PublishesSEVCreated(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Database failure",
		SeverityLevel: 1,
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].eventType != "sev.created" || events[0].sevID != resp.GetId() {
		t.Errorf("got %+v, want type=sev.created sev_id=%s", events[0], resp.GetId())
	}
}

func TestCreateSEV_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	if _, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Sensitive incident",
		SeverityLevel: 1,
		Sensitive:     true,
	}); err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
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

	sevID := seedSEV(t, ts)

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

	sevID := seedSEV(t, ts)

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

	sevID := seedSEV(t, ts) // starts at Open

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
		Id:       "SEV-9999-0001", // does not exist
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

	sevID := seedSEV(t, ts)

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

// ── WebSocket event publishing ────────────────────────────────────────────────

func TestUpdateSEV_PublishesEvent(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: sevID, Title: "New title"}); err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "sev.updated" {
		t.Errorf("event = %+v, want sev_id=%q type=sev.updated", events[0], sevID)
	}
}

func TestTransitionStatus_PublishesEvent(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusInvestigating)}); err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "sev.status_changed" {
		t.Errorf("event = %+v, want sev_id=%q type=sev.status_changed", events[0], sevID)
	}
}

func seedSensitiveSEV(t *testing.T, ts *testSEVServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Sensitive SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		Sensitive:     true,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSensitiveSEV: %v", err)
	}
	return sv.ID
}

func TestUpdateSEV_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSensitiveSEV(t, ts)

	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: sevID, Title: "New title"}); err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestTransitionStatus_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSensitiveSEV(t, ts)

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusInvestigating)}); err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

// ── AI dispatch (§11.1, M12) ────────────────────────────────────────────────

func TestCreateSEV_DispatchesAIOnOpenForSEV1(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "db down", SeverityLevel: 1})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerSEVOpened || triggers[0].sevID != resp.GetId() {
		t.Fatalf("got triggers %+v, want one sev.opened for %s", triggers, resp.GetId())
	}
}

// TestCreateSEV_SensitiveSEVStillEnqueuesTrigger and
// TestCreateSEV_AIDisabledSEVStillEnqueuesTrigger: SEVServer.dispatchAI
// enqueues unconditionally — it deliberately does not re-implement the
// Sensitive/AIDisabled gate itself (see its doc comment). That gate is
// enforced once, centrally, by ai.Dispatcher against a freshly-fetched
// record (internal/ai/dispatcher_test.go's
// TestDispatch_SensitiveSEVSkipsProactiveTrigger /
// TestDispatch_AIDisabledSEVSkipsProactiveTrigger /
// TestDispatch_SensitiveAtExecutionTimeSkipsTrigger cover the actual
// skip-dispatch behavior); fakeAIDispatcher here is a stand-in for the gRPC
// layer's Dispatch call, not for that gate.
func TestCreateSEV_SensitiveSEVStillEnqueuesTrigger(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "sensitive", SeverityLevel: 1, Sensitive: true})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerSEVOpened || triggers[0].sevID != resp.GetId() {
		t.Errorf("got triggers %+v, want one sev.opened for %s", triggers, resp.GetId())
	}
}

func TestCreateSEV_AIDisabledSEVStillEnqueuesTrigger(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "x", SeverityLevel: 1, AiDisabled: true})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerSEVOpened || triggers[0].sevID != resp.GetId() {
		t.Errorf("got triggers %+v, want one sev.opened for %s", triggers, resp.GetId())
	}
}

func TestTransitionStatus_DispatchesAIOnMitigatedAndResolved(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts) // starts Open, SEV-1

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusMitigated)}); err != nil {
		t.Fatalf("TransitionStatus to mitigated: %v", err)
	}
	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusResolved)}); err != nil {
		t.Fatalf("TransitionStatus to resolved: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 2 || triggers[0].event != ai.TriggerSEVMitigated || triggers[1].event != ai.TriggerSEVResolved {
		t.Fatalf("got triggers %+v, want [sev.mitigated, sev.resolved]", triggers)
	}
}

func TestTransitionStatus_InvestigatingDoesNotDispatchAI(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusInvestigating)}); err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	if triggers := ts.ai.All(); len(triggers) != 0 {
		t.Errorf("triggers = %+v, want none for a transition to investigating", triggers)
	}
}

// ── Recurrence auto-link (§17) ───────────────────────────────────────────────

func TestUpdateSEV_AutoLinksRecurrence_SameServiceAndCategory(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "First outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	links, err := ts.links.ListBySEVID(ctx, second.GetId())
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	found := false
	for _, l := range links {
		if l.SourceSEVID == second.GetId() && l.TargetSEVID == first.GetId() && l.RelationshipType == store.SEVRelationshipRecurrenceOf {
			found = true
		}
	}
	if !found {
		t.Errorf("want a recurrence-of link from %s to %s, got %+v", second.GetId(), first.GetId(), links)
	}
}

func TestUpdateSEV_NoAutoLinkToSensitiveSEV(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Sensitive outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"}, Sensitive: true,
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	// A non-sensitive SEV must never get auto-linked to a Sensitive one —
	// that would surface the sensitive SEV's ID to anyone who can view the
	// new, non-sensitive record via ListLinkedSEVs.
	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 0 {
		t.Errorf("want no auto-link to a sensitive SEV, got %+v", links)
	}
}

func TestUpdateSEV_NoAutoLinkForDifferentService(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "API outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "DB outage", SeverityLevel: 2, AffectedServices: []string{"svc-db"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 0 {
		t.Errorf("want no auto-link for a different affected service, got %+v", links)
	}
}

func TestUpdateSEV_NoAutoLinkForDifferentCategory(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "First outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "hardware"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 0 {
		t.Errorf("want no auto-link for a different root cause category, got %+v", links)
	}
}

func TestUpdateSEV_UnrelatedUpdateDoesNotReLink(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "First outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}
	// An unrelated follow-up update (root cause category unchanged) must not
	// attempt to re-link (which would otherwise surface as a duplicate-link
	// error being silently swallowed — this asserts the guard that prevents
	// the attempt in the first place).
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), Mitigation: "rolled back the bad deploy"}); err != nil {
		t.Fatalf("UpdateSEV(second, unrelated): %v", err)
	}

	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 1 {
		t.Errorf("want exactly 1 link after an unrelated update, got %+v", links)
	}
}
