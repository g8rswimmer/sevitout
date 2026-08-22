package grpc_test

import (
	"context"
	"fmt"
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

// TestSearchSEVs_QuickView_MySEVs_NoRolesReturnsEmpty locks in the fix for a
// nil-vs-empty ambiguity: ListSEVIDsByUser used to return nil both for "no
// user given" and "user given, zero matches", so intersectIDs treated a real
// user with zero role assignments as "unconstrained" and returned every SEV.
func TestSearchSEVs_QuickView_MySEVs_NoRolesReturnsEmpty(t *testing.T) {
	ts := newTestSearchServer()
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-with-no-roles"})

	_ = seedSearchSEV(t, ts, &store.SEV{Title: "a", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "b", SeverityLevel: 2, Status: store.SEVStatusOpen})

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{QuickView: "my_sevs"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 0 || resp.GetTotal() != 0 {
		t.Fatalf("want 0 results for a user with no role assignments, got %d (total=%d)", len(resp.GetSevs()), resp.GetTotal())
	}
}

// TestSearchSEVs_OnCallUser_NoMatchReturnsEmpty is the same regression as
// above, exercised via on_call_user instead of the my_sevs quick view.
func TestSearchSEVs_OnCallUser_NoMatchReturnsEmpty(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	_ = seedSearchSEV(t, ts, &store.SEV{Title: "a", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "b", SeverityLevel: 2, Status: store.SEVStatusOpen})

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{OnCallUser: "nobody@example.com"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 0 || resp.GetTotal() != 0 {
		t.Fatalf("want 0 results when on_call_user matches no role assignment, got %d (total=%d)", len(resp.GetSevs()), resp.GetTotal())
	}
}

// TestSearchSEVs_TextSearch_DefaultLimitAppliesOnMergePath locks in the fix
// for the merge (announcement) path ignoring Limit==0: it used to return the
// entire merged set instead of the same default page size (100) the plain
// path gets from postgres.SEVStore.List.
func TestSearchSEVs_TextSearch_DefaultLimitAppliesOnMergePath(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	const byFieldCount = 150
	for i := 0; i < byFieldCount; i++ {
		seedSearchSEV(t, ts, &store.SEV{Title: fmt.Sprintf("outage report %d", i), SeverityLevel: 2, Status: store.SEVStatusOpen})
	}
	viaAnnouncement := seedSearchSEV(t, ts, &store.SEV{Title: "zzz", SeverityLevel: 2, Status: store.SEVStatusOpen})
	if err := ts.announcements.Create(ctx, &store.Announcement{
		SEVID: viaAnnouncement, AuthorID: "user-1", Message: "outage mitigated", Audience: store.AudienceInternal,
	}); err != nil {
		t.Fatalf("Create announcement: %v", err)
	}

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{Query: "outage"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	if resp.GetTotal() != byFieldCount+1 {
		t.Fatalf("want exact total=%d, got %d", byFieldCount+1, resp.GetTotal())
	}
	if len(resp.GetSevs()) != 100 {
		t.Fatalf("want default page size 100, got %d", len(resp.GetSevs()))
	}
}

// TestSearchSEVs_TextSearch_FanoutCapReturnsError locks in the fix for the
// merge path silently truncating (and mis-sorting/mis-counting) results when
// either underlying fetch hits searchFanoutLimit: it now fails loudly with
// ResourceExhausted instead of returning an incomplete, wrongly-ordered page.
func TestSearchSEVs_TextSearch_FanoutCapReturnsError(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	// Must exceed internal/api/grpc/search.go's unexported searchFanoutLimit (10000).
	const overCap = 10001
	for i := 0; i < overCap; i++ {
		seedSearchSEV(t, ts, &store.SEV{Title: fmt.Sprintf("outage %d", i), SeverityLevel: 2, Status: store.SEVStatusOpen})
	}
	viaAnnouncement := seedSearchSEV(t, ts, &store.SEV{Title: "zzz", SeverityLevel: 2, Status: store.SEVStatusOpen})
	if err := ts.announcements.Create(ctx, &store.Announcement{
		SEVID: viaAnnouncement, AuthorID: "user-1", Message: "outage mitigated", Audience: store.AudienceInternal,
	}); err != nil {
		t.Fatalf("Create announcement: %v", err)
	}

	_, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{Query: "outage"})
	if grpcCode(err) != codes.ResourceExhausted {
		t.Fatalf("want ResourceExhausted when the fan-out cap is exceeded, got %v", err)
	}
}

// TestSearchSEVs_ExcludesSensitiveSEVs locks in the mitigation for the lack
// of a sensitive-SEV visibility/ACL mechanism: SearchSEVs must not surface
// Sensitive SEVs via its keyword/filter-based discovery, even though a
// Viewer's request would otherwise match them.
func TestSearchSEVs_ExcludesSensitiveSEVs(t *testing.T) {
	ts := newTestSearchServer()
	ctx := context.Background()

	normal := seedSearchSEV(t, ts, &store.SEV{Title: "outage report", SeverityLevel: 2, Status: store.SEVStatusOpen})
	_ = seedSearchSEV(t, ts, &store.SEV{Title: "outage in the security system", SeverityLevel: 2, Status: store.SEVStatusOpen, Sensitive: true})

	resp, err := ts.server.SearchSEVs(ctx, &pb.SearchSEVsRequest{Query: "outage"})
	if err != nil {
		t.Fatalf("SearchSEVs: %v", err)
	}
	ids := resultIDs(resp)
	if len(ids) != 1 || !ids[normal] {
		t.Fatalf("want only the non-sensitive SEV (%s), got %v", normal, ids)
	}
	if resp.GetTotal() != 1 {
		t.Fatalf("want total=1, got %d", resp.GetTotal())
	}
}

func strPtr(v string) *string { return &v }
