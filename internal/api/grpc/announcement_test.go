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

type testAnnouncementServer struct {
	server        *grpchandler.AnnouncementServer
	announcements *memory.AnnouncementStore
	sevs          *memory.SEVStore
}

func newTestAnnouncementServer() *testAnnouncementServer {
	announcements := memory.NewAnnouncementStore()
	sevs := memory.NewSEVStore()
	return &testAnnouncementServer{
		server:        grpchandler.NewAnnouncementServer(announcements, sevs),
		announcements: announcements,
		sevs:          sevs,
	}
}

func seedSEVForAnnouncement(t *testing.T, ts *testAnnouncementServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Announcement test SEV",
		SeverityLevel: 2,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForAnnouncement: %v", err)
	}
	return sv.ID
}

// ── CreateAnnouncement ────────────────────────────────────────────────────────

func TestCreateAnnouncement_ValidInternal(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	resp, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:    sevID,
		Message:  "We are investigating the issue.",
		Audience: string(store.AudienceInternal),
		AuthorId: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	if resp.GetId() == 0 {
		t.Error("ID should be set after create")
	}
	if resp.GetSevId() != sevID {
		t.Errorf("sev_id = %q, want %q", resp.GetSevId(), sevID)
	}
	if resp.GetAudience() != string(store.AudienceInternal) {
		t.Errorf("audience = %q, want internal", resp.GetAudience())
	}
	if resp.GetMessage() != "We are investigating the issue." {
		t.Errorf("message mismatch")
	}
}

func TestCreateAnnouncement_ValidExternal(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	resp, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:    sevID,
		Message:  "We are aware of an issue affecting some users.",
		Audience: string(store.AudienceExternal),
		AuthorId: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	if resp.GetAudience() != string(store.AudienceExternal) {
		t.Errorf("audience = %q, want external", resp.GetAudience())
	}
}

func TestCreateAnnouncement_ValidStatusPage(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	resp, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:    sevID,
		Message:  "Service disruption in progress.",
		Audience: string(store.AudienceStatusPage),
		AuthorId: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	if resp.GetAudience() != string(store.AudienceStatusPage) {
		t.Errorf("audience = %q, want status-page", resp.GetAudience())
	}
}

func TestCreateAnnouncement_MilestoneFlag(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	resp, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:       sevID,
		Message:     "Mitigation complete.",
		Audience:    string(store.AudienceInternal),
		IsMilestone: true,
		AuthorId:    "user-1",
	})
	if err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	if !resp.GetIsMilestone() {
		t.Error("is_milestone should be true")
	}
}

func TestCreateAnnouncement_MissingSEVID(t *testing.T) {
	ts := newTestAnnouncementServer()
	_, err := ts.server.CreateAnnouncement(context.Background(), &pb.CreateAnnouncementRequest{
		Message:  "msg",
		Audience: string(store.AudienceInternal),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateAnnouncement_MissingMessage(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	_, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:    sevID,
		Audience: string(store.AudienceInternal),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateAnnouncement_MissingAudience(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	_, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:   sevID,
		Message: "msg",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateAnnouncement_InvalidAudience(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	_, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
		SevId:    sevID,
		Message:  "msg",
		Audience: "public",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateAnnouncement_SEVNotFound(t *testing.T) {
	ts := newTestAnnouncementServer()
	_, err := ts.server.CreateAnnouncement(context.Background(), &pb.CreateAnnouncementRequest{
		SevId:    "SEV-9999-0001",
		Message:  "msg",
		Audience: string(store.AudienceInternal),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// ── ListAnnouncements ─────────────────────────────────────────────────────────

func TestListAnnouncements_Ordering(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	messages := []string{"first", "second", "third"}
	for _, msg := range messages {
		_, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
			SevId:    sevID,
			Message:  msg,
			Audience: string(store.AudienceInternal),
			AuthorId: "user-1",
		})
		if err != nil {
			t.Fatalf("CreateAnnouncement %q: %v", msg, err)
		}
	}

	resp, err := ts.server.ListAnnouncements(ctx, &pb.ListAnnouncementsRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListAnnouncements: %v", err)
	}
	if len(resp.GetAnnouncements()) != 3 {
		t.Fatalf("want 3 announcements, got %d", len(resp.GetAnnouncements()))
	}
	for i, msg := range messages {
		if resp.GetAnnouncements()[i].GetMessage() != msg {
			t.Errorf("announcement[%d] = %q, want %q", i, resp.GetAnnouncements()[i].GetMessage(), msg)
		}
	}
}

func TestListAnnouncements_AudienceFilter(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	audiences := []store.AudienceType{
		store.AudienceInternal,
		store.AudienceExternal,
		store.AudienceStatusPage,
		store.AudienceInternal,
	}
	for _, aud := range audiences {
		_, err := ts.server.CreateAnnouncement(ctx, &pb.CreateAnnouncementRequest{
			SevId:    sevID,
			Message:  "msg for " + string(aud),
			Audience: string(aud),
			AuthorId: "user-1",
		})
		if err != nil {
			t.Fatalf("CreateAnnouncement: %v", err)
		}
	}

	tests := []struct {
		filter string
		want   int
	}{
		{string(store.AudienceInternal), 2},
		{string(store.AudienceExternal), 1},
		{string(store.AudienceStatusPage), 1},
		{"", 4},
	}
	for _, tc := range tests {
		resp, err := ts.server.ListAnnouncements(ctx, &pb.ListAnnouncementsRequest{
			SevId:    sevID,
			Audience: tc.filter,
		})
		if err != nil {
			t.Fatalf("ListAnnouncements filter=%q: %v", tc.filter, err)
		}
		if len(resp.GetAnnouncements()) != tc.want {
			t.Errorf("filter=%q: got %d, want %d", tc.filter, len(resp.GetAnnouncements()), tc.want)
		}
	}
}

func TestListAnnouncements_MissingSEVID(t *testing.T) {
	ts := newTestAnnouncementServer()
	_, err := ts.server.ListAnnouncements(context.Background(), &pb.ListAnnouncementsRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestListAnnouncements_SEVNotFound(t *testing.T) {
	ts := newTestAnnouncementServer()
	_, err := ts.server.ListAnnouncements(context.Background(), &pb.ListAnnouncementsRequest{
		SevId: "SEV-9999-0001",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestListAnnouncements_Empty(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	resp, err := ts.server.ListAnnouncements(ctx, &pb.ListAnnouncementsRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListAnnouncements: %v", err)
	}
	if len(resp.GetAnnouncements()) != 0 {
		t.Errorf("want 0 announcements, got %d", len(resp.GetAnnouncements()))
	}
}

func TestListAnnouncements_InvalidAudienceFilter(t *testing.T) {
	ts := newTestAnnouncementServer()
	ctx := context.Background()
	sevID := seedSEVForAnnouncement(t, ts)

	_, err := ts.server.ListAnnouncements(ctx, &pb.ListAnnouncementsRequest{
		SevId:    sevID,
		Audience: "public",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for invalid audience filter, got %v", grpcCode(err))
	}
}
