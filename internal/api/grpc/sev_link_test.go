package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testSEVLinkServer struct {
	server *grpchandler.SEVLinkServer
	links  *memory.SEVLinkStore
	sevs   *memory.SEVStore
	audit  *memory.AuditStore
}

func newTestSEVLinkServer() *testSEVLinkServer {
	links := memory.NewSEVLinkStore()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	return &testSEVLinkServer{
		server: grpchandler.NewSEVLinkServer(links, sevs, audit),
		links:  links,
		sevs:   sevs,
		audit:  audit,
	}
}

func seedSEVForLink(t *testing.T, ts *testSEVLinkServer, title string) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         title,
		SeverityLevel: 2,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForLink: %v", err)
	}
	return sv.ID
}

// ── LinkSEVs ──────────────────────────────────────────────────────────────────

func TestLinkSEVs_Valid(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	resp, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
		CreatedBy:        "user-1",
	})
	if err != nil {
		t.Fatalf("LinkSEVs: %v", err)
	}
	if resp.GetSourceSevId() != sevA {
		t.Errorf("source_sev_id = %q, want %q", resp.GetSourceSevId(), sevA)
	}
	if resp.GetTargetSevId() != sevB {
		t.Errorf("target_sev_id = %q, want %q", resp.GetTargetSevId(), sevB)
	}
	if resp.GetRelationshipType() != string(store.SEVRelationshipRelated) {
		t.Errorf("relationship_type = %q, want related", resp.GetRelationshipType())
	}
}

func TestLinkSEVs_Bidirectional(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipDuplicate),
	})
	if err != nil {
		t.Fatalf("LinkSEVs: %v", err)
	}

	// Both A and B should show the link in their list.
	respA, err := ts.server.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevA})
	if err != nil {
		t.Fatalf("ListLinkedSEVs(A): %v", err)
	}
	respB, err := ts.server.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevB})
	if err != nil {
		t.Fatalf("ListLinkedSEVs(B): %v", err)
	}

	// The store's ListBySEVID returns links where sevID is source OR target,
	// but we insert two rows so each side is listed as a source link.
	if len(respA.GetLinks()) == 0 {
		t.Error("SEV A should have at least one link")
	}
	if len(respB.GetLinks()) == 0 {
		t.Error("SEV B should have at least one link (bidirectional)")
	}
}

func TestLinkSEVs_AllRelationshipTypes(t *testing.T) {
	relTypes := []store.SEVRelationshipType{
		store.SEVRelationshipRelated,
		store.SEVRelationshipCausedBy,
		store.SEVRelationshipDuplicate,
		store.SEVRelationshipRecurrenceOf,
	}
	for _, rt := range relTypes {
		ts := newTestSEVLinkServer()
		ctx := context.Background()
		sevA := seedSEVForLink(t, ts, "SEV A")
		sevB := seedSEVForLink(t, ts, "SEV B")

		_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
			SourceSevId:      sevA,
			TargetSevId:      sevB,
			RelationshipType: string(rt),
		})
		if err != nil {
			t.Errorf("LinkSEVs with %s: %v", rt, err)
		}
	}
}

func TestLinkSEVs_MissingSourceSEVID(t *testing.T) {
	ts := newTestSEVLinkServer()
	_, err := ts.server.LinkSEVs(context.Background(), &pb.LinkSEVsRequest{
		TargetSevId:      "SEV-9999-0002",
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_MissingTargetSEVID(t *testing.T) {
	ts := newTestSEVLinkServer()
	_, err := ts.server.LinkSEVs(context.Background(), &pb.LinkSEVsRequest{
		SourceSevId:      "SEV-9999-0001",
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_SameSEV(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevA,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for self-link, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_InvalidRelationshipType(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: "blocks",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_SourceNotFound(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevB := seedSEVForLink(t, ts, "SEV B")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      "SEV-9999-0001",
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_TargetNotFound(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      "SEV-9999-0002",
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_AlreadyExists(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	req := &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	}
	if _, err := ts.server.LinkSEVs(ctx, req); err != nil {
		t.Fatalf("first LinkSEVs: %v", err)
	}
	_, err := ts.server.LinkSEVs(ctx, req)
	if grpcCode(err) != codes.AlreadyExists {
		t.Errorf("want AlreadyExists, got %v", grpcCode(err))
	}
}

func TestLinkSEVs_AuditEntryCreated(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if err != nil {
		t.Fatalf("LinkSEVs: %v", err)
	}

	entries, _ := ts.audit.ListBySEVID(ctx, sevA)
	found := false
	for _, e := range entries {
		if e.Action == "sev.linked" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no audit entry with action sev.linked after LinkSEVs")
	}
}

// ── UnlinkSEVs ────────────────────────────────────────────────────────────────

func TestUnlinkSEVs_Valid(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	_, err := ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if err != nil {
		t.Fatalf("LinkSEVs: %v", err)
	}

	_, err = ts.server.UnlinkSEVs(ctx, &pb.UnlinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if err != nil {
		t.Fatalf("UnlinkSEVs: %v", err)
	}

	// Both directions should be gone.
	respA, _ := ts.server.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevA})
	respB, _ := ts.server.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevB})

	// After unlinking, neither A→B nor B→A should remain.
	for _, l := range respA.GetLinks() {
		if l.GetTargetSevId() == sevB {
			t.Error("A→B link should be removed after unlink")
		}
	}
	for _, l := range respB.GetLinks() {
		if l.GetTargetSevId() == sevA {
			t.Error("B→A reverse link should be removed after unlink")
		}
	}
}

func TestUnlinkSEVs_NotFound(t *testing.T) {
	ts := newTestSEVLinkServer()
	_, err := ts.server.UnlinkSEVs(context.Background(), &pb.UnlinkSEVsRequest{
		SourceSevId:      "SEV-9999-0001",
		TargetSevId:      "SEV-9999-0002",
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestUnlinkSEVs_MissingSourceSEVID(t *testing.T) {
	ts := newTestSEVLinkServer()
	_, err := ts.server.UnlinkSEVs(context.Background(), &pb.UnlinkSEVsRequest{
		TargetSevId:      "SEV-9999-0002",
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestUnlinkSEVs_AuditEntryCreated(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")

	_, _ = ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	_, _ = ts.server.UnlinkSEVs(ctx, &pb.UnlinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})

	entries, _ := ts.audit.ListBySEVID(ctx, sevA)
	found := false
	for _, e := range entries {
		if e.Action == "sev.unlinked" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no audit entry with action sev.unlinked after UnlinkSEVs")
	}
}

// ── ListLinkedSEVs ────────────────────────────────────────────────────────────

func TestListLinkedSEVs_Empty(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")

	resp, err := ts.server.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevA})
	if err != nil {
		t.Fatalf("ListLinkedSEVs: %v", err)
	}
	if len(resp.GetLinks()) != 0 {
		t.Errorf("want 0 links, got %d", len(resp.GetLinks()))
	}
}

func TestListLinkedSEVs_MissingSEVID(t *testing.T) {
	ts := newTestSEVLinkServer()
	_, err := ts.server.ListLinkedSEVs(context.Background(), &pb.ListLinkedSEVsRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestListLinkedSEVs_SEVNotFound(t *testing.T) {
	ts := newTestSEVLinkServer()
	_, err := ts.server.ListLinkedSEVs(context.Background(), &pb.ListLinkedSEVsRequest{
		SevId: "SEV-9999-0001",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestListLinkedSEVs_MultipleLinks(t *testing.T) {
	ts := newTestSEVLinkServer()
	ctx := context.Background()
	sevA := seedSEVForLink(t, ts, "SEV A")
	sevB := seedSEVForLink(t, ts, "SEV B")
	sevC := seedSEVForLink(t, ts, "SEV C")

	_, _ = ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevB,
		RelationshipType: string(store.SEVRelationshipRelated),
	})
	_, _ = ts.server.LinkSEVs(ctx, &pb.LinkSEVsRequest{
		SourceSevId:      sevA,
		TargetSevId:      sevC,
		RelationshipType: string(store.SEVRelationshipCausedBy),
	})

	resp, err := ts.server.ListLinkedSEVs(ctx, &pb.ListLinkedSEVsRequest{SevId: sevA})
	if err != nil {
		t.Fatalf("ListLinkedSEVs: %v", err)
	}
	// A→B and A→C (two forward links; reverse links appear on B and C)
	if len(resp.GetLinks()) < 2 {
		t.Errorf("want at least 2 links for SEV A, got %d", len(resp.GetLinks()))
	}
}
