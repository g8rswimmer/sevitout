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

// testSearchServer groups a SearchServer with its backing in-memory stores.
type testSearchServer struct {
	server        *grpchandler.SearchServer
	sevs          *memory.SEVStore
	roles         *memory.RoleStore
	announcements *memory.AnnouncementStore
}

func newTestSearchServer() *testSearchServer {
	sevs := memory.NewSEVStore()
	roles := memory.NewRoleStore()
	announcements := memory.NewAnnouncementStore()
	return &testSearchServer{
		server:        grpchandler.NewSearchServer(sevs, roles, announcements),
		sevs:          sevs,
		roles:         roles,
		announcements: announcements,
	}
}

// seedSearchSEV inserts sv directly into the backing store and returns the
// auto-assigned ID.
func seedSearchSEV(t *testing.T, ts *testSearchServer, sv *store.SEV) string {
	t.Helper()
	if sv.CreatedAt.IsZero() {
		sv.CreatedAt = time.Now()
	}
	if sv.UpdatedAt.IsZero() {
		sv.UpdatedAt = sv.CreatedAt
	}
	if sv.CreatedBy == "" {
		sv.CreatedBy = "user-seed"
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSearchSEV: %v", err)
	}
	return sv.ID
}

func resultIDs(resp *pb.SearchSEVsResponse) map[string]bool {
	out := make(map[string]bool, len(resp.GetSevs()))
	for _, s := range resp.GetSevs() {
		out[s.GetId()] = true
	}
	return out
}

func TestSearchSEVs_FilterCombination(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	openCritical := seedSearchSEV(t, ts, &store.SEV{
		Title: "checkout down", SeverityLevel: 1, Status: store.SEVStatusOpen,
		AffectedServices: []string{"checkout"},
	})
	_ = seedSearchSEV(t, ts, &store.SEV{
		Title: "billing slow", SeverityLevel: 3, Status: store.SEVStatusInvestigating,
		AffectedServices: []string{"billing"},
	})

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{
		SeverityLevels: []int32{1},
		Statuses:       []string{"open"},
		ServiceIds:     []string{"checkout"},
	})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 1 || !ids[openCritical] {
		t.Fatalf("want only %s, got %v", openCritical, ids)
	}
	if resp.GetTotal() != 1 {
		t.Fatalf("want total=1, got %d", resp.GetTotal())
	}
}

func TestSearchSEVs_QuickView_Open(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	open := seedSearchSEV(t, ts, &store.SEV{Title: "a", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "b", SeverityLevel: 2, Status: store.SEVStatusResolved})

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{QuickView: "open"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 1 || !ids[open] {
		t.Fatalf("want only %s, got %v", open, ids)
	}
}

func TestSearchSEVs_QuickView_AwaitingPostmortem(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	_ = seedSearchSEV(t, ts, &store.SEV{Title: "a", SeverityLevel: 2, Status: store.SEVStatusOpen})
	resolved := seedSearchSEV(t, ts, &store.SEV{Title: "b", SeverityLevel: 2, Status: store.SEVStatusResolved})
	inProgress := seedSearchSEV(t, ts, &store.SEV{Title: "c", SeverityLevel: 2, Status: store.SEVStatusPostmortemInProgress})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "d", SeverityLevel: 2, Status: store.SEVStatusPostmortemComplete})

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{QuickView: "awaiting_postmortem"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 2 || !ids[resolved] || !ids[inProgress] {
		t.Fatalf("want %s and %s, got %v", resolved, inProgress, ids)
	}
}

func TestSearchSEVs_QuickView_Unknown(t *testing.T) {
	ts := newTestSearchServer()
	_, err := ts.server.SearchSEVs(context.Background(), &pb.SearchSEVsRequest{QuickView: "bogus"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

func TestSearchSEVs_Sort_Unknown(t *testing.T) {
	ts := newTestSearchServer()
	_, err := ts.server.SearchSEVs(context.Background(), &pb.SearchSEVsRequest{Sort: "bogus"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

func TestSearchSEVs_QuickView_MySEVs(t *testing.T) {
	ts := newTestSearchServer()
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-42"})

	mine := seedSearchSEV(t, ts, &store.SEV{Title: "mine", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "not mine", SeverityLevel: 2, Status: store.SEVStatusOpen})

	if err := ts.roles.Assign(context.Background(), &store.SEVRole{
		SEVID: mine, RoleType: store.SEVRoleResponder, UserID: strPtr("user-42"), DisplayName: "Me", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{QuickView: "my_sevs"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 1 || !ids[mine] {
		t.Fatalf("want only %s, got %v", mine, ids)
	}
}

func TestSearchSEVs_QuickView_MySEVs_RequiresAuth(t *testing.T) {
	ts := newTestSearchServer()
	_, err := ts.server.SearchSEVs(context.Background(), &pb.SearchSEVsRequest{QuickView: "my_sevs"})
	if grpcCode(err) != codes.Unauthenticated {
		t.Fatalf("want Unauthenticated, got %v", err)
	}
}

func TestSearchSEVs_OnCallUser(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	onCallSEV := seedSearchSEV(t, ts, &store.SEV{Title: "a", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "b", SeverityLevel: 2, Status: store.SEVStatusOpen})

	if err := ts.roles.Assign(ctx, &store.SEVRole{
		SEVID: onCallSEV, RoleType: store.SEVRoleOnCall, DisplayName: "alice@example.com", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{OnCallUser: "alice@example.com"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 1 || !ids[onCallSEV] {
		t.Fatalf("want only %s, got %v", onCallSEV, ids)
	}
}

func TestSearchSEVs_DetectedBy(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	detected := seedSearchSEV(t, ts, &store.SEV{Title: "a", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "b", SeverityLevel: 2, Status: store.SEVStatusOpen})

	if err := ts.roles.Assign(ctx, &store.SEVRole{
		SEVID: detected, RoleType: store.SEVRoleDetectedBy, DisplayName: "synthetic-monitor", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{DetectedBy: "synthetic-monitor"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 1 || !ids[detected] {
		t.Fatalf("want only %s, got %v", detected, ids)
	}
}

// TestSearchSEVs_TextSearch_IncludesAnnouncementMatches exercises the
// dual-source merge path: one SEV matches the query on its own fields, a
// second only matches via an announcement, and a third matches neither and
// must not be returned.
func TestSearchSEVs_TextSearch_IncludesAnnouncementMatches(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	byField := seedSearchSEV(t, ts, &store.SEV{Title: "database failover event", SeverityLevel: 2, Status: store.SEVStatusOpen})
	byAnnouncement := seedSearchSEV(t, ts, &store.SEV{Title: "unrelated title", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "totally different", SeverityLevel: 2, Status: store.SEVStatusOpen})

	if err := ts.announcements.Create(ctx, &store.Announcement{
		SEVID: byAnnouncement, AuthorID: "user-1", Message: "database failover completed", Audience: store.AudienceInternal,
	}); err != nil {
		t.Fatalf("Create announcement: %v", err)
	}

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{Query: "failover"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 2 || !ids[byField] || !ids[byAnnouncement] {
		t.Fatalf("want %s and %s, got %v", byField, byAnnouncement, ids)
	}
	if resp.GetTotal() != 2 {
		t.Fatalf("want total=2, got %d", resp.GetTotal())
	}
}

func TestSearchSEVs_TextSearch_MergedPagination(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	byField := seedSearchSEV(t, ts, &store.SEV{Title: "outage report", SeverityLevel: 2, Status: store.SEVStatusOpen})
	byAnnouncement := seedSearchSEV(t, ts, &store.SEV{Title: "zzz", SeverityLevel: 1, Status: store.SEVStatusOpen})
	if err := ts.announcements.Create(ctx, &store.Announcement{
		SEVID: byAnnouncement, AuthorID: "user-1", Message: "outage mitigated", Audience: store.AudienceInternal,
	}); err != nil {
		t.Fatalf("Create announcement: %v", err)
	}

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{Query: "outage", Sort: "severity", Limit: 1})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	if resp.GetTotal() != 2 {
		t.Fatalf("want total=2 (unpaginated count), got %d", resp.GetTotal())
	}
	if len(resp.GetSevs()) != 1 {
		t.Fatalf("want 1 result on this page, got %d", len(resp.GetSevs()))
	}
	if resp.GetSevs()[0].GetId() != byAnnouncement {
		t.Fatalf("want lowest severity (%s) first, got %s", byAnnouncement, resp.GetSevs()[0].GetId())
	}
	_ = byField
}

func strPtr(v string) *string { return &v }
