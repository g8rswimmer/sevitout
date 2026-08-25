package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testChatServer struct {
	server *grpchandler.ChatServer
	chat   *memory.ChatStore
	sevs   *memory.SEVStore
	access *memory.SEVAccessStore
	pub    *fakePublisher
}

func newTestChatServer() *testChatServer {
	chat := memory.NewChatStore()
	sevs := memory.NewSEVStore()
	access := memory.NewSEVAccessStore()
	pub := &fakePublisher{}
	return &testChatServer{
		server: grpchandler.NewChatServer(chat, sevs, access, pub),
		chat:   chat,
		sevs:   sevs,
		access: access,
		pub:    pub,
	}
}

func seedSEVForChat(t *testing.T, ts *testChatServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Chat test SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForChat: %v", err)
	}
	return sv.ID
}

// ── AddChatEntry ──────────────────────────────────────────────────────────────

func TestAddChatEntry_Valid(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	occurred := time.Now().Add(-5 * time.Minute)
	resp, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId:      sevID,
		OccurredAt: timestamppb.New(occurred),
		Source:     "slack",
		Author:     "alice",
		Content:    "checking the metrics now",
		AddedBy:    "user-1",
	})
	if err != nil {
		t.Fatalf("AddChatEntry: %v", err)
	}
	if resp.GetId() == 0 {
		t.Error("ID should be set after add")
	}
	if resp.GetSevId() != sevID {
		t.Errorf("sev_id = %q, want %q", resp.GetSevId(), sevID)
	}
	if resp.GetSource() != "slack" {
		t.Errorf("source = %q, want slack", resp.GetSource())
	}
	if resp.GetAuthor() != "alice" {
		t.Errorf("author = %q, want alice", resp.GetAuthor())
	}
	if resp.GetContent() != "checking the metrics now" {
		t.Errorf("content mismatch")
	}
}

func TestAddChatEntry_DefaultsOccurredAtToNow(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	before := time.Now()
	resp, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId:   sevID,
		Source:  "slack",
		Author:  "bob",
		Content: "on it",
		AddedBy: "user-1",
	})
	after := time.Now()

	if err != nil {
		t.Fatalf("AddChatEntry: %v", err)
	}
	oa := resp.GetOccurredAt().AsTime()
	if oa.Before(before) || oa.After(after) {
		t.Errorf("occurred_at %v not in expected range [%v, %v]", oa, before, after)
	}
}

func TestAddChatEntry_MissingSEVID(t *testing.T) {
	ts := newTestChatServer()
	_, err := ts.server.AddChatEntry(context.Background(), &pb.AddChatEntryRequest{
		Source:  "slack",
		Author:  "alice",
		Content: "msg",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAddChatEntry_MissingContent(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	_, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId:  sevID,
		Source: "slack",
		Author: "alice",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAddChatEntry_MissingAuthor(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	_, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId:   sevID,
		Source:  "slack",
		Content: "msg",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAddChatEntry_MissingSource(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	_, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId:   sevID,
		Author:  "alice",
		Content: "msg",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAddChatEntry_SEVNotFound(t *testing.T) {
	ts := newTestChatServer()
	_, err := ts.server.AddChatEntry(context.Background(), &pb.AddChatEntryRequest{
		SevId:   "SEV-9999-0001",
		Source:  "slack",
		Author:  "alice",
		Content: "msg",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// ── ListChatEntries ───────────────────────────────────────────────────────────

func TestListChatEntries_Ordering(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	contents := []string{"msg-1", "msg-2", "msg-3"}
	for _, c := range contents {
		_, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
			SevId:   sevID,
			Source:  "slack",
			Author:  "alice",
			Content: c,
			AddedBy: "user-1",
		})
		if err != nil {
			t.Fatalf("AddChatEntry %q: %v", c, err)
		}
	}

	resp, err := ts.server.ListChatEntries(ctx, &pb.ListChatEntriesRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListChatEntries: %v", err)
	}
	if len(resp.GetEntries()) != 3 {
		t.Fatalf("want 3 entries, got %d", len(resp.GetEntries()))
	}
	for i, c := range contents {
		if resp.GetEntries()[i].GetContent() != c {
			t.Errorf("entry[%d] content = %q, want %q", i, resp.GetEntries()[i].GetContent(), c)
		}
	}
}

func TestListChatEntries_Empty(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	resp, err := ts.server.ListChatEntries(ctx, &pb.ListChatEntriesRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListChatEntries: %v", err)
	}
	if len(resp.GetEntries()) != 0 {
		t.Errorf("want 0 entries, got %d", len(resp.GetEntries()))
	}
}

func TestListChatEntries_MissingSEVID(t *testing.T) {
	ts := newTestChatServer()
	_, err := ts.server.ListChatEntries(context.Background(), &pb.ListChatEntriesRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestListChatEntries_SEVNotFound(t *testing.T) {
	ts := newTestChatServer()
	_, err := ts.server.ListChatEntries(context.Background(), &pb.ListChatEntriesRequest{
		SevId: "SEV-9999-0001",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// ── WebSocket event publishing ────────────────────────────────────────────────

func TestAddChatEntry_PublishesEvent(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	sevID := seedSEVForChat(t, ts)

	_, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId:   sevID,
		Source:  "slack",
		Author:  "alice",
		Content: "checking the metrics now",
		AddedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("AddChatEntry: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "chat.created" {
		t.Errorf("event = %+v, want sev_id=%q type=chat.created", events[0], sevID)
	}
}

func TestAddChatEntry_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestChatServer()
	ctx := context.Background()
	now := time.Now()
	sv := &store.SEV{
		Title: "Sensitive SEV", SeverityLevel: 1, Status: store.SEVStatusOpen,
		Sensitive: true, CreatedBy: "user-1", CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.sevs.Create(ctx, sv); err != nil {
		t.Fatalf("seed sensitive SEV: %v", err)
	}

	_, err := ts.server.AddChatEntry(ctx, &pb.AddChatEntryRequest{
		SevId: sv.ID, Source: "slack", Author: "alice", Content: "note", AddedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("AddChatEntry: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}
