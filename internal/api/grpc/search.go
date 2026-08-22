package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// searchFanoutLimit bounds the two unpaginated SEVStore.List calls used to
// compute a merged, in-process-sorted result set when a full-text query
// matches announcements as well as SEV fields (see searchWithAnnouncements).
// If either fetch hits this cap, searchWithAnnouncements fails the request
// rather than silently sorting and paginating an incomplete set.
const searchFanoutLimit = 10000

// defaultSearchLimit is the page size applied when the caller doesn't set
// Limit, matching the default already used by postgres.SEVStore.List so the
// merge path (which paginates in Go, see searchWithAnnouncements) behaves
// the same as the single-query path for a Postgres-backed deployment.
const defaultSearchLimit = 100

// SearchServer implements SearchService.
type SearchServer struct {
	pb.UnimplementedSearchServiceServer
	sevs          store.SEVStore
	roles         store.RoleStore
	announcements store.AnnouncementStore
}

// NewSearchServer returns a SearchServer.
func NewSearchServer(sevs store.SEVStore, roles store.RoleStore, announcements store.AnnouncementStore) *SearchServer {
	return &SearchServer{sevs: sevs, roles: roles, announcements: announcements}
}

func (s *SearchServer) SearchSEVs(ctx context.Context, req *pb.SearchSEVsRequest) (*pb.SearchSEVsResponse, error) {
	filter := store.SEVFilter{
		RootCauseCategory: req.GetRootCauseCategory(),
		ServiceIDs:        req.GetServiceIds(),
		Search:            req.GetQuery(),
		Limit:             int(req.GetLimit()),
		Offset:            int(req.GetOffset()),
		SortDesc:          req.GetSortDesc(),
		// There's no sensitive-SEV visibility/ACL mechanism yet (see
		// docs/requirements.md §14); this endpoint's keyword/content-based
		// discovery (including announcement text) shouldn't be the way
		// Sensitive SEVs get surfaced to a Viewer in the meantime.
		ExcludeSensitive: true,
	}
	for _, l := range req.GetSeverityLevels() {
		filter.SeverityLevels = append(filter.SeverityLevels, int16(l))
	}
	for _, st := range req.GetStatuses() {
		filter.Statuses = append(filter.Statuses, store.SEVStatus(st))
	}
	if len(req.GetTags()) > 0 {
		filter.Tags = req.GetTags()
	}
	if req.GetStartedAfter() != nil {
		t := req.GetStartedAfter().AsTime()
		filter.StartedAfter = &t
	}
	if req.GetStartedBefore() != nil {
		t := req.GetStartedBefore().AsTime()
		filter.StartedBefore = &t
	}

	switch req.GetSort() {
	case "":
	case string(store.SEVSortStartedAt), string(store.SEVSortSeverity), string(store.SEVSortMTTR), string(store.SEVSortUpdatedAt):
		filter.Sort = store.SEVSortField(req.GetSort())
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown sort value")
	}

	quickView := req.GetQuickView()
	switch quickView {
	case "":
	case "open":
		if len(filter.Statuses) == 0 {
			filter.Statuses = []store.SEVStatus{store.SEVStatusOpen, store.SEVStatusInvestigating, store.SEVStatusMitigated}
		}
	case "awaiting_postmortem":
		if len(filter.Statuses) == 0 {
			filter.Statuses = []store.SEVStatus{store.SEVStatusResolved, store.SEVStatusPostmortemInProgress}
		}
	case "my_sevs":
		// resolved below once we can attribute it to the caller
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown quick_view value")
	}

	// Resolve role-based constraints (on_call_user, detected_by, and the
	// my_sevs quick view) into a SEV ID allowlist, intersected when more than
	// one is given, so SEVStore never needs to know about role assignments.
	var idSets [][]string
	if u := req.GetOnCallUser(); u != "" {
		onCall := store.SEVRoleOnCall
		ids, err := s.roles.ListSEVIDsByUser(ctx, u, &onCall)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to resolve on_call_user")
		}
		idSets = append(idSets, ids)
	}
	if u := req.GetDetectedBy(); u != "" {
		detectedBy := store.SEVRoleDetectedBy
		ids, err := s.roles.ListSEVIDsByUser(ctx, u, &detectedBy)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to resolve detected_by")
		}
		idSets = append(idSets, ids)
	}
	if quickView == "my_sevs" {
		uc, ok := auth.UserFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "my_sevs requires an authenticated caller")
		}
		ids, err := s.roles.ListSEVIDsByUser(ctx, uc.UserID, nil)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to resolve my_sevs")
		}
		idSets = append(idSets, ids)
	}
	if len(idSets) > 0 {
		filter.IDs = intersectIDs(idSets)
	}

	var announcementIDs []string
	if filter.Search != "" {
		ids, err := s.announcements.SearchSEVIDs(ctx, filter.Search)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to search announcements")
		}
		announcementIDs = ids
	}

	var records []*store.SEV
	var total int
	if len(announcementIDs) == 0 {
		var err error
		total, err = s.sevs.Count(ctx, filter)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to count SEVs")
		}
		records, err = s.sevs.List(ctx, filter)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to search SEVs")
		}
	} else {
		merged, err := s.searchWithAnnouncements(ctx, filter, announcementIDs)
		if err != nil {
			return nil, err
		}
		// searchWithAnnouncements only returns a result when neither
		// underlying fetch hit searchFanoutLimit, so this is a complete,
		// exact count — not an approximation capped by the fanout.
		total = len(merged)
		limit := filter.Limit
		if limit <= 0 {
			limit = defaultSearchLimit
		}
		offset := filter.Offset
		if offset > len(merged) {
			offset = len(merged)
		}
		merged = merged[offset:]
		if limit < len(merged) {
			merged = merged[:limit]
		}
		records = merged
	}

	resp := &pb.SearchSEVsResponse{Total: int32(total)}
	for _, r := range records {
		resp.Sevs = append(resp.Sevs, sevToProto(r))
	}
	return resp, nil
}

// searchWithAnnouncements handles full-text queries that also match
// announcement text, which lives in a separate store. It fetches SEV-field
// matches and announcement matches unpaginated (bounded by searchFanoutLimit),
// unions them, and sorts the merged set — trying to combine two independently
// paginated SQL queries into one page would be incorrect, so pagination
// happens in Go instead, at the call site.
func (s *SearchServer) searchWithAnnouncements(ctx context.Context, filter store.SEVFilter, announcementIDs []string) ([]*store.SEV, error) {
	fieldFilter := filter
	fieldFilter.Limit, fieldFilter.Offset = searchFanoutLimit, 0
	byField, err := s.sevs.List(ctx, fieldFilter)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to search SEVs")
	}

	viaAnnouncements := filter
	viaAnnouncements.Search = ""
	viaAnnouncements.Limit, viaAnnouncements.Offset = searchFanoutLimit, 0
	viaAnnouncements.IDs = intersectIDs([][]string{announcementIDs, filter.IDs})
	byAnnouncement, err := s.sevs.List(ctx, viaAnnouncements)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to search SEVs via announcements")
	}

	// Either fetch hitting the cap means the merged set below would be
	// incomplete, which would silently corrupt both the sort order and the
	// total count for this request (even on page 1, since sorting happens
	// after fetching) — fail loudly instead of returning wrong results.
	if len(byField) >= searchFanoutLimit || len(byAnnouncement) >= searchFanoutLimit {
		return nil, status.Error(codes.ResourceExhausted, "search matched too many SEVs to combine field and announcement results reliably; narrow the query with additional filters")
	}

	merged := mergeSEVsByID(byField, byAnnouncement)
	store.SortSEVs(merged, filter.Sort, filter.SortDesc)
	return merged, nil
}

// intersectIDs computes the intersection of the given sets, treating a nil
// set as "unconstrained" (skipped). Returns nil if every set is nil, and an
// empty (non-nil) slice if the non-nil sets have no common element.
func intersectIDs(sets [][]string) []string {
	var nonNil [][]string
	for _, set := range sets {
		if set != nil {
			nonNil = append(nonNil, set)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}

	counts := make(map[string]int)
	for _, set := range nonNil {
		seen := make(map[string]bool, len(set))
		for _, id := range set {
			if !seen[id] {
				seen[id] = true
				counts[id]++
			}
		}
	}

	out := make([]string, 0)
	for id, c := range counts {
		if c == len(nonNil) {
			out = append(out, id)
		}
	}
	return out
}

func mergeSEVsByID(lists ...[]*store.SEV) []*store.SEV {
	var total int
	for _, l := range lists {
		total += len(l)
	}
	seen := make(map[string]bool, total)
	out := make([]*store.SEV, 0, total)
	for _, list := range lists {
		for _, sv := range list {
			if !seen[sv.ID] {
				seen[sv.ID] = true
				out = append(out, sv)
			}
		}
	}
	return out
}
