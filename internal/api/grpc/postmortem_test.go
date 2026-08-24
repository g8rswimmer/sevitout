package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/g8rswimmer/sevitout/internal/ai"
	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// testPostmortemServer bundles a PostmortemServer with its in-memory stores.
type testPostmortemServer struct {
	server      *grpchandler.PostmortemServer
	postmortems *memory.PostmortemStore
	sevs        *memory.SEVStore
	audit       *memory.AuditStore
	unlock      *postmortem.UnlockSigner
	pub         *fakePublisher
	ai          *fakeAIDispatcher
}

func newTestPostmortemServer() *testPostmortemServer {
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	pms := memory.NewPostmortemStore()
	signer := postmortem.NewUnlockSigner("test-secret-at-least-32-chars-long")
	pub := &fakePublisher{}
	aiDispatch := &fakeAIDispatcher{}
	return &testPostmortemServer{
		server: grpchandler.NewPostmortemServer(grpchandler.PostmortemServerParams{
			Postmortems: pms, SEVs: sevs, Audit: audit, Unlock: signer, Publisher: pub, AIDispatch: aiDispatch,
		}),
		postmortems: pms,
		sevs:        sevs,
		audit:       audit,
		unlock:      signer,
		pub:         pub,
		ai:          aiDispatch,
	}
}

// seedLockedSEV creates a SEV in PostmortemComplete state (locked=true) with
// an associated postmortem record and returns the SEV ID.
func seedLockedSEV(t *testing.T, ts *testPostmortemServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Completed SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusPostmortemComplete,
		Locked:        true,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedLockedSEV: create SEV: %v", err)
	}
	if err := ts.postmortems.Create(context.Background(), &store.Postmortem{
		SEVID:     sv.ID,
		Status:    store.PostmortemStatusApproved,
		Content:   "original content",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seedLockedSEV: create postmortem: %v", err)
	}
	return sv.ID
}

// seedUnlockedSEVWithPostmortem creates an open SEV with a Draft postmortem.
func seedUnlockedSEVWithPostmortem(t *testing.T, ts *testPostmortemServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Active SEV",
		SeverityLevel: 2,
		Status:        store.SEVStatusResolved,
		Locked:        false,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seed: create SEV: %v", err)
	}
	if err := ts.postmortems.Create(context.Background(), &store.Postmortem{
		SEVID:     sv.ID,
		Status:    store.PostmortemStatusDraft,
		Content:   "",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: create postmortem: %v", err)
	}
	return sv.ID
}

// ── GetPostmortem ─────────────────────────────────────────────────────────────

func TestGetPostmortem_Found(t *testing.T) {
	ts := newTestPostmortemServer()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	resp, err := ts.server.GetPostmortem(context.Background(), &pb.GetPostmortemRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("GetPostmortem: %v", err)
	}
	if resp.GetSevId() != sevID {
		t.Errorf("SevId = %q, want %q", resp.GetSevId(), sevID)
	}
	if resp.GetStatus() != string(store.PostmortemStatusDraft) {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), store.PostmortemStatusDraft)
	}
}

func TestGetPostmortem_MissingSEVID(t *testing.T) {
	ts := newTestPostmortemServer()
	_, err := ts.server.GetPostmortem(context.Background(), &pb.GetPostmortemRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestGetPostmortem_NotFound(t *testing.T) {
	ts := newTestPostmortemServer()
	_, err := ts.server.GetPostmortem(context.Background(), &pb.GetPostmortemRequest{SevId: "no-such-sev"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("code = %v, want NotFound", grpcCode(err))
	}
}

// ── UpdatePostmortem — unlocked SEV ──────────────────────────────────────────

func TestUpdatePostmortem_UnlockedSEV(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	resp, err := ts.server.UpdatePostmortem(ctx, &pb.UpdatePostmortemRequest{
		SevId:   sevID,
		Content: "## Summary\n\nOutage lasted 45 minutes.",
		UserId:  "user-1",
	})
	if err != nil {
		t.Fatalf("UpdatePostmortem: %v", err)
	}
	if resp.GetContent() != "## Summary\n\nOutage lasted 45 minutes." {
		t.Errorf("Content = %q", resp.GetContent())
	}
}

// ── Lock enforcement ─────────────────────────────────────────────────────────

func TestUpdatePostmortem_LockedWithoutToken_Rejected(t *testing.T) {
	ts := newTestPostmortemServer()
	sevID := seedLockedSEV(t, ts)

	_, err := ts.server.UpdatePostmortem(context.Background(), &pb.UpdatePostmortemRequest{
		SevId:   sevID,
		Content: "should be rejected",
	})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied", grpcCode(err))
	}
}

func TestUpdatePostmortem_LockedWithValidToken_Allowed(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedLockedSEV(t, ts)

	token, err := ts.unlock.Sign(sevID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	resp, err := ts.server.UpdatePostmortem(ctx, &pb.UpdatePostmortemRequest{
		SevId:       sevID,
		Content:     "updated content",
		UnlockToken: token,
		UserId:      "user-ic",
	})
	if err != nil {
		t.Fatalf("UpdatePostmortem with valid token: %v", err)
	}
	if resp.GetContent() != "updated content" {
		t.Errorf("Content = %q, want %q", resp.GetContent(), "updated content")
	}
}

func TestUpdatePostmortem_LockedWithWrongSEVToken_Rejected(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedLockedSEV(t, ts)

	// Token signed for a different SEV
	token, err := ts.unlock.Sign("SEV-9999-9999")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, err = ts.server.UpdatePostmortem(ctx, &pb.UpdatePostmortemRequest{
		SevId:       sevID,
		Content:     "should be rejected",
		UnlockToken: token,
	})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied", grpcCode(err))
	}
}

func TestUpdatePostmortem_RelockAfterWrite(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedLockedSEV(t, ts)

	token, err := ts.unlock.Sign(sevID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// First write with token succeeds.
	if _, err := ts.server.UpdatePostmortem(ctx, &pb.UpdatePostmortemRequest{
		SevId:       sevID,
		Content:     "first edit",
		UnlockToken: token,
		UserId:      "user-ic",
	}); err != nil {
		t.Fatalf("first UpdatePostmortem: %v", err)
	}

	// After the write, the SEV is still locked — a write without a token fails.
	sv, err := ts.sevs.Get(ctx, sevID)
	if err != nil {
		t.Fatalf("Get SEV: %v", err)
	}
	if !sv.Locked {
		t.Error("SEV should still be locked after write, but Locked=false")
	}
}

// ── TransitionPostmortemStatus ────────────────────────────────────────────────

func TestTransitionPostmortemStatus_DraftToInReview(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	resp, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId:    sevID,
		ToStatus: string(store.PostmortemStatusInReview),
		UserId:   "user-ic",
	})
	if err != nil {
		t.Fatalf("TransitionPostmortemStatus: %v", err)
	}
	if resp.GetStatus() != string(store.PostmortemStatusInReview) {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), store.PostmortemStatusInReview)
	}
}

func TestTransitionPostmortemStatus_InvalidTransition(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	// Draft → Approved is not valid (must go through InReview first).
	_, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId:    sevID,
		ToStatus: string(store.PostmortemStatusApproved),
		UserId:   "user-ic",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestTransitionPostmortemStatus_AuditEntryCreated(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	_, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId:    sevID,
		ToStatus: string(store.PostmortemStatusInReview),
		UserId:   "user-ic",
	})
	if err != nil {
		t.Fatalf("TransitionPostmortemStatus: %v", err)
	}

	entries, err := ts.audit.ListBySEVID(ctx, sevID)
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Action == "postmortem.status_transitioned" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no audit entry with action postmortem.status_transitioned found")
	}
}

// ── UnlockSEV ─────────────────────────────────────────────────────────────────

func TestUnlockSEV_ReturnsToken(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedLockedSEV(t, ts)

	resp, err := ts.server.UnlockSEV(ctx, &pb.UnlockSEVRequest{
		SevId:  sevID,
		Reason: "fixing typo discovered post-approval",
		UserId: "user-ic",
	})
	if err != nil {
		t.Fatalf("UnlockSEV: %v", err)
	}
	if resp.GetUnlockToken() == "" {
		t.Error("expected non-empty unlock_token")
	}

	// The returned token must validate for the correct SEV.
	if err := ts.unlock.Validate(resp.GetUnlockToken(), sevID); err != nil {
		t.Errorf("returned token failed Validate: %v", err)
	}
}

func TestUnlockSEV_MissingReason(t *testing.T) {
	ts := newTestPostmortemServer()
	sevID := seedLockedSEV(t, ts)

	_, err := ts.server.UnlockSEV(context.Background(), &pb.UnlockSEVRequest{
		SevId: sevID,
		// Reason intentionally omitted
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestUnlockSEV_AlreadyUnlocked(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts) // not locked

	_, err := ts.server.UnlockSEV(ctx, &pb.UnlockSEVRequest{
		SevId:  sevID,
		Reason: "trying to unlock a non-locked SEV",
	})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", grpcCode(err))
	}
}

func TestUnlockSEV_AuditLogWritten(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedLockedSEV(t, ts)

	if _, err := ts.server.UnlockSEV(ctx, &pb.UnlockSEVRequest{
		SevId:  sevID,
		Reason: "audit test",
		UserId: "user-ic",
	}); err != nil {
		t.Fatalf("UnlockSEV: %v", err)
	}

	entries, err := ts.audit.ListBySEVID(ctx, sevID)
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Action == "sev.unlock_requested" {
			found = true
			if e.NewValue == nil || *e.NewValue != "audit test" {
				t.Errorf("audit entry NewValue = %v, want %q", e.NewValue, "audit test")
			}
			break
		}
	}
	if !found {
		t.Errorf("no audit entry with action sev.unlock_requested found")
	}
}

// ── WebSocket event publishing ────────────────────────────────────────────────

func TestUpdatePostmortem_PublishesEvent(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	_, err := ts.server.UpdatePostmortem(ctx, &pb.UpdatePostmortemRequest{
		SevId:   sevID,
		Content: "## Summary",
		UserId:  "user-1",
	})
	if err != nil {
		t.Fatalf("UpdatePostmortem: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "postmortem.updated" {
		t.Errorf("event = %+v, want sev_id=%q type=postmortem.updated", events[0], sevID)
	}
}

func TestTransitionPostmortemStatus_PublishesEvent(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	_, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId:    sevID,
		ToStatus: string(store.PostmortemStatusInReview),
		UserId:   "user-ic",
	})
	if err != nil {
		t.Fatalf("TransitionPostmortemStatus: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "postmortem.updated" {
		t.Errorf("event = %+v, want sev_id=%q type=postmortem.updated", events[0], sevID)
	}
}

func seedSensitiveSEVWithPostmortem(t *testing.T, ts *testPostmortemServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title: "Sensitive SEV", SeverityLevel: 2, Status: store.SEVStatusResolved,
		Sensitive: true, CreatedBy: "user-seed", CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seed: create sensitive SEV: %v", err)
	}
	if err := ts.postmortems.Create(context.Background(), &store.Postmortem{
		SEVID: sv.ID, Status: store.PostmortemStatusDraft, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: create postmortem: %v", err)
	}
	return sv.ID
}

func TestUpdatePostmortem_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedSensitiveSEVWithPostmortem(t, ts)

	_, err := ts.server.UpdatePostmortem(ctx, &pb.UpdatePostmortemRequest{
		SevId: sevID, Content: "## Summary", UserId: "user-1",
	})
	if err != nil {
		t.Fatalf("UpdatePostmortem: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestTransitionPostmortemStatus_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedSensitiveSEVWithPostmortem(t, ts)

	_, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId: sevID, ToStatus: string(store.PostmortemStatusInReview), UserId: "user-ic",
	})
	if err != nil {
		t.Fatalf("TransitionPostmortemStatus: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

// ── AI dispatch (§11.1, M12) ────────────────────────────────────────────────

func TestTransitionPostmortemStatus_DispatchesAIOnInReview(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedUnlockedSEVWithPostmortem(t, ts)

	if _, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId: sevID, ToStatus: string(store.PostmortemStatusInReview), UserId: "user-ic",
	}); err != nil {
		t.Fatalf("TransitionPostmortemStatus: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerPostmortemInReview || triggers[0].sevID != sevID {
		t.Fatalf("got triggers %+v, want one postmortem.in_review for %s", triggers, sevID)
	}
}

// TestTransitionPostmortemStatus_SensitiveSEVStillEnqueuesTrigger: this
// handler enqueues unconditionally — it deliberately does not re-implement
// the Sensitive/AIDisabled gate itself (see SEVServer.dispatchAI's doc
// comment). That gate is enforced once, centrally, by ai.Dispatcher against
// a freshly-fetched record (internal/ai/dispatcher_test.go's
// TestDispatch_SensitiveSEVSkipsProactiveTrigger covers the actual
// skip-dispatch behavior); fakeAIDispatcher here is a stand-in for the gRPC
// layer's Dispatch call, not for that gate.
func TestTransitionPostmortemStatus_SensitiveSEVStillEnqueuesTrigger(t *testing.T) {
	ts := newTestPostmortemServer()
	ctx := context.Background()
	sevID := seedSensitiveSEVWithPostmortem(t, ts)

	if _, err := ts.server.TransitionPostmortemStatus(ctx, &pb.TransitionPostmortemStatusRequest{
		SevId: sevID, ToStatus: string(store.PostmortemStatusInReview), UserId: "user-ic",
	}); err != nil {
		t.Fatalf("TransitionPostmortemStatus: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerPostmortemInReview || triggers[0].sevID != sevID {
		t.Errorf("got triggers %+v, want one postmortem.in_review for %s", triggers, sevID)
	}
}
