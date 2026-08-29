package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/g8rswimmer/sevitout/internal/ai"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// OnCaller retrieves the current on-call person for a service.
// Implementations must return ("", nil) when no one is on-call.
type OnCaller interface {
	OnCallLookup(ctx context.Context, serviceID string) (string, error)
}

// AIDispatcher routes a proactive lifecycle event (§11.1) to the AI plugin
// system for async processing; the call must never block. Declared here
// (the consumer) per this repo's interface-ownership convention —
// ai.Dispatcher satisfies it implicitly.
type AIDispatcher interface {
	Dispatch(event ai.TriggerEvent, sevID string)
}

type SEVServer struct {
	pb.UnimplementedSEVServiceServer
	sevs        store.SEVStore
	audit       store.AuditStore
	history     store.StatusHistoryStore
	roles       store.RoleStore
	services    store.ServiceStore
	postmortems store.PostmortemStore
	links       store.SEVLinkStore
	access      store.SEVAccessStore
	onCaller    OnCaller // nil when PagerDuty is not configured
	unlock      Unlocker
	publisher   Publisher    // nil when WebSocket support is not wired up
	aiDispatch  AIDispatcher // nil when no AI plugin is configured
}

// SEVServerParams groups NewSEVServer's dependencies. OnCaller, Publisher,
// and AIDispatch may all be nil (PagerDuty/WebSocket/AI plugin support,
// respectively, are each optional at deploy time) — every other field is
// required.
type SEVServerParams struct {
	SEVs        store.SEVStore
	Audit       store.AuditStore
	History     store.StatusHistoryStore
	Roles       store.RoleStore
	Services    store.ServiceStore
	Postmortems store.PostmortemStore
	Links       store.SEVLinkStore
	Access      store.SEVAccessStore
	OnCaller    OnCaller
	Unlock      Unlocker
	Publisher   Publisher
	AIDispatch  AIDispatcher
}

func NewSEVServer(p SEVServerParams) *SEVServer {
	return &SEVServer{
		sevs:        p.SEVs,
		audit:       p.Audit,
		history:     p.History,
		roles:       p.Roles,
		services:    p.Services,
		postmortems: p.Postmortems,
		links:       p.Links,
		access:      p.Access,
		onCaller:    p.OnCaller,
		unlock:      p.Unlock,
		publisher:   p.Publisher,
		aiDispatch:  p.AIDispatch,
	}
}

// autoLinkRecurrence implements docs/requirements.md §17's "recurring
// incident flag": when record has a root cause category set, look for the
// most recent other SEV sharing both that category and at least one
// affected service, and link record → that SEV as recurrence-of. Only the
// single most recent match is linked (not every match) so one common
// service+category pair doesn't fan out into a link per historical
// incident — "recurrence of" reads most naturally as "the same pattern seen
// last time" anyway. Best-effort: failures are logged, never surfaced to the
// caller, matching the on-call auto-population pattern in CreateSEV above.
func (s *SEVServer) autoLinkRecurrence(ctx context.Context, record *store.SEV, callerID string) {
	if record.RootCauseCategory == nil || *record.RootCauseCategory == "" || len(record.AffectedServices) == 0 {
		return
	}

	matches, err := s.sevs.List(ctx, store.SEVFilter{
		ServiceIDs:        record.AffectedServices,
		RootCauseCategory: *record.RootCauseCategory,
		Limit:             5,
		// A non-sensitive SEV shouldn't get auto-linked to a Sensitive one —
		// that would surface the sensitive SEV's ID (via ListLinkedSEVs) to
		// anyone who can view the new, non-sensitive record. Same rationale
		// as SearchServer's ExcludeSensitive use (search.go).
		ExcludeSensitive: true,
	})
	if err != nil {
		slog.ErrorContext(ctx, "recurrence auto-link lookup failed", "sev_id", record.ID, "err", err)
		return
	}

	var prior *store.SEV
	for _, m := range matches {
		if m.ID != record.ID {
			prior = m
			break
		}
	}
	if prior == nil {
		return
	}

	err = s.links.Create(ctx, &store.SEVLink{
		SourceSEVID:      record.ID,
		TargetSEVID:      prior.ID,
		RelationshipType: store.SEVRelationshipRecurrenceOf,
		CreatedAt:        time.Now(),
		CreatedBy:        callerID,
	})
	if err != nil && !errors.Is(err, store.ErrConflict) {
		slog.ErrorContext(ctx, "recurrence auto-link create failed", "sev_id", record.ID, "target_sev_id", prior.ID, "err", err)
	}
}

// dispatchAI fires a proactive AI trigger unless aiDispatch is unconfigured.
// The Sensitive/AIDisabled (§11.3, §14) gate — sensitive SEVs never have
// their content sent to a configured AI plugin, which may be a third-party
// API, consistent with M11's exclusion of sensitive SEVs from Slack incident
// channels — is deliberately not re-implemented here: it's enforced once,
// centrally, by ai.Dispatcher itself (against a freshly-fetched record, so a
// Sensitive flip after this call but before the worker runs is still
// caught), so every entry point — proactive and on-demand alike — shares one
// source of truth instead of drifting independently.
func (s *SEVServer) dispatchAI(event ai.TriggerEvent, record *store.SEV) {
	if s.aiDispatch == nil {
		return
	}
	s.aiDispatch.Dispatch(event, record.ID)
}

// validateDetectionMethod rejects a non-empty detection_method that isn't one
// of docs/requirements.md §4.2's fixed vocabulary — same "switch over the
// known consts, reject unknown" pattern role.go uses for role_type. An empty
// value is allowed (detection method just wasn't recorded).
func validateDetectionMethod(v string) error {
	if v == "" {
		return nil
	}
	switch store.DetectionMethod(v) {
	case store.DetectionMethodAlert, store.DetectionMethodMonitoringDashboard,
		store.DetectionMethodCustomerReport, store.DetectionMethodSyntheticTest,
		store.DetectionMethodManualDiscovery, store.DetectionMethodSlackEscalation:
		return nil
	default:
		return status.Error(codes.InvalidArgument, "unknown detection_method")
	}
}

// newSEVFromCreateRequest builds a *store.SEV from req's fields, callerID,
// and now. Split out of CreateSEV so that handler reads as validation +
// construction + persistence + side effects, rather than burying the field
// mapping in the middle of all of that.
func newSEVFromCreateRequest(req *pb.CreateSEVRequest, callerID string, now time.Time) *store.SEV {
	record := &store.SEV{
		Title:            req.GetTitle(),
		Description:      req.GetDescription(),
		SeverityLevel:    int16(req.GetSeverityLevel()),
		Status:           store.SEVStatusOpen,
		AffectedServices: req.GetAffectedServices(),
		Tags:             req.GetTags(),
		Sensitive:        req.GetSensitive(),
		AIDisabled:       req.GetAiDisabled(),
		CreatedBy:        callerID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if v := req.GetDetectionMethod(); v != "" {
		record.DetectionMethod = &v
	}
	if v := req.GetAlertName(); v != "" {
		record.AlertName = &v
	}
	if v := req.GetMonitoringTool(); v != "" {
		record.MonitoringTool = &v
	}
	if v := req.GetAlertUrl(); v != "" {
		record.AlertURL = &v
	}
	if v := req.GetMetricLink(); v != "" {
		record.MetricLink = &v
	}
	if v := req.GetSnapshotUrl(); v != "" {
		record.SnapshotURL = &v
	}
	if req.GetStartedAt() != nil {
		t := req.GetStartedAt().AsTime()
		record.StartedAt = &t
	}
	// No default when omitted: started_at is left unset until the caller
	// explicitly sets it (docs/requirements.md §2.1 — "may be estimated", not
	// "assume now"). ComputeMetrics already treats a nil StartedAt as
	// "can't compute yet" rather than erroring.
	if req.GetDetectedAt() != nil {
		t := req.GetDetectedAt().AsTime()
		record.DetectedAt = &t
	}

	return record
}

func (s *SEVServer) CreateSEV(ctx context.Context, req *pb.CreateSEVRequest) (*pb.SEVResponse, error) {
	if req.GetTitle() == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if req.GetSeverityLevel() < 1 || req.GetSeverityLevel() > 4 {
		return nil, status.Error(codes.InvalidArgument, "severity_level must be between 1 and 4")
	}
	if err := validateDetectionMethod(req.GetDetectionMethod()); err != nil {
		return nil, err
	}

	callerID := req.GetCreatedBy()
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	record := newSEVFromCreateRequest(req, callerID, now)
	sev.ComputeMetrics(record)

	if err := s.sevs.Create(ctx, record); err != nil {
		return nil, internalError(ctx, "failed to create SEV", err)
	}

	// Auto-grant the creator visibility into their own Sensitive SEV (§14) —
	// without this, the reporter would immediately lose the ability to view
	// what they just filed. Best-effort: never blocks or fails the response,
	// matching the on-call auto-population pattern below.
	if record.Sensitive {
		if err := s.access.Grant(ctx, &store.SEVAccess{
			SEVID: record.ID, UserID: callerID, CreatedAt: now, CreatedBy: callerID,
		}); err != nil {
			slog.ErrorContext(ctx, "auto-grant creator access failed", "sev_id", record.ID, "err", err)
		}
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     record.ID,
		UserID:    callerID,
		Action:    "sev.created",
		CreatedAt: now,
	})

	if err := s.postmortems.Create(ctx, &store.Postmortem{
		SEVID:     record.ID,
		Status:    store.PostmortemStatusDraft,
		Content:   "",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return nil, internalError(ctx, "failed to create postmortem for SEV", err)
	}

	// Best-effort on-call auto-population. Never blocks or fails the response.
	if s.onCaller != nil && len(req.GetAffectedServices()) > 0 {
		if svc, err := s.services.Get(ctx, req.GetAffectedServices()[0]); err == nil && svc.PagerDutyServiceID != nil {
			if displayName, err := s.onCaller.OnCallLookup(ctx, *svc.PagerDutyServiceID); err == nil && displayName != "" {
				if err := s.roles.Assign(ctx, &store.SEVRole{
					SEVID:       record.ID,
					RoleType:    store.SEVRoleOnCall,
					DisplayName: displayName,
					CreatedAt:   now,
					CreatedBy:   callerID,
				}); err != nil {
					slog.ErrorContext(ctx, "on-call role auto-assign failed", "sev_id", record.ID, "err", err)
				}
			}
		}
	}

	resp := sevToProto(record)
	if !record.Sensitive {
		// Published after on-call auto-population above so a subscriber
		// reacting to sev.created (the Slack bot's auto incident-channel
		// creation, M11) can immediately look up the on-call role via
		// RoleService without racing this handler's own writes.
		publishProto(s.publisher, record.ID, "sev.created", resp)
	}
	// SEV opened proactively triggers AI only for SEV-1/SEV-2 (§11.1);
	// Dispatcher itself enforces the severity gate against the freshly
	// stored record, since this async trigger may run after further writes.
	s.dispatchAI(ai.TriggerSEVOpened, record)

	// §17's "new SEV auto-linked on create": root cause category isn't part
	// of CreateSEVRequest today (it's set later via UpdateSEV, once
	// investigation identifies it — see the equivalent call there), so this
	// will normally be a no-op at creation time. It's still called here so
	// the rare caller that does have a root cause pinned down immediately
	// gets the same auto-link behavior as everyone else.
	s.autoLinkRecurrence(ctx, record, callerID)

	return resp, nil
}

func (s *SEVServer) GetSEV(ctx context.Context, req *pb.GetSEVRequest) (*pb.SEVResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	record, err := s.sevs.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}
	visible, err := sensitiveSEVVisible(ctx, s.access, record)
	if err != nil {
		return nil, internalError(ctx, "failed to check SEV visibility", err)
	}
	if !visible {
		return nil, status.Error(codes.NotFound, "SEV not found")
	}
	return sevToProto(record), nil
}

// applySEVUpdate copies every field req sets onto record — an unset/empty
// field on a partial-update request leaves the corresponding field on record
// untouched, matching every other Update* handler in this package. Split out
// of UpdateSEV so that handler reads as validation + fetch + field mapping +
// persistence + side effects, rather than one 160-line function mixing all
// of those together.
//
// Returns whether root_cause_category actually changed (UpdateSEV uses this
// to decide whether to re-run §17's recurrence auto-link) and whether
// sensitive flipped false→true (UpdateSEV uses this to auto-grant the
// original reporter access under §14).
func applySEVUpdate(record *store.SEV, req *pb.UpdateSEVRequest) (rootCauseCategoryChanged, sensitiveFlipped bool, err error) {
	if v := req.GetTitle(); v != "" {
		record.Title = v
	}
	if v := req.GetDescription(); v != "" {
		record.Description = v
	}
	if v := req.GetSeverityLevel(); v != 0 {
		if v < 1 || v > 4 {
			return false, false, status.Error(codes.InvalidArgument, "severity_level must be between 1 and 4")
		}
		record.SeverityLevel = int16(v)
	}
	if v := req.GetRootCauseCategory(); v != "" {
		if record.RootCauseCategory == nil || *record.RootCauseCategory != v {
			rootCauseCategoryChanged = true
		}
		record.RootCauseCategory = &v
	}
	if v := req.GetRootCauseDescription(); v != "" {
		record.RootCauseDescription = &v
	}
	if v := req.GetMitigation(); v != "" {
		record.Mitigation = &v
	}
	if v := req.GetPrevention(); v != "" {
		record.Prevention = &v
	}
	if v := req.GetBusinessImpact(); v != "" {
		record.BusinessImpact = &v
	}
	if v := req.GetAffectedServices(); len(v) > 0 {
		record.AffectedServices = v
	}
	if err := validateDetectionMethod(req.GetDetectionMethod()); err != nil {
		return false, false, err
	}
	if v := req.GetDetectionMethod(); v != "" {
		record.DetectionMethod = &v
	}
	if v := req.GetAlertName(); v != "" {
		record.AlertName = &v
	}
	if v := req.GetMonitoringTool(); v != "" {
		record.MonitoringTool = &v
	}
	if v := req.GetAlertUrl(); v != "" {
		record.AlertURL = &v
	}
	if v := req.GetMetricLink(); v != "" {
		record.MetricLink = &v
	}
	if v := req.GetSnapshotUrl(); v != "" {
		record.SnapshotURL = &v
	}
	if v := req.GetGithubRepo(); v != "" {
		record.GitHubRepo = &v
	}
	if v := req.GetRootCauseReferenceUrl(); v != "" {
		record.RootCauseReferenceURL = &v
	}
	if req.GetRightPeoplePresent() != nil {
		b := req.GetRightPeoplePresent().GetValue()
		record.RightPeoplePresent = &b
	}
	if v := req.GetRightPeopleNotes(); v != "" {
		record.RightPeopleNotes = &v
	}
	if v := req.GetTags(); len(v) > 0 {
		record.Tags = v
	}
	if req.GetStartedAt() != nil {
		t := req.GetStartedAt().AsTime()
		record.StartedAt = &t
	}
	if req.GetDetectedAt() != nil {
		t := req.GetDetectedAt().AsTime()
		record.DetectedAt = &t
	}
	if req.GetSensitive() != nil {
		newVal := req.GetSensitive().GetValue()
		if newVal && !record.Sensitive {
			sensitiveFlipped = true
		}
		record.Sensitive = newVal
	}
	if req.GetAiDisabled() != nil {
		record.AIDisabled = req.GetAiDisabled().GetValue()
	}

	return rootCauseCategoryChanged, sensitiveFlipped, nil
}

func (s *SEVServer) UpdateSEV(ctx context.Context, req *pb.UpdateSEVRequest) (*pb.SEVResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	record, err := s.sevs.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}
	visible, err := sensitiveSEVVisible(ctx, s.access, record)
	if err != nil {
		return nil, internalError(ctx, "failed to check SEV visibility", err)
	}
	if !visible {
		return nil, status.Error(codes.NotFound, "SEV not found")
	}

	if record.Locked {
		if err := validateUnlock(s.unlock, req.GetUnlockToken(), req.GetId()); err != nil {
			return nil, err
		}
	}

	rootCauseCategoryChanged, sensitiveFlipped, err := applySEVUpdate(record, req)
	if err != nil {
		return nil, err
	}

	record.UpdatedAt = time.Now()
	sev.ComputeMetrics(record)

	if err := s.sevs.Update(ctx, record); err != nil {
		return nil, internalError(ctx, "failed to update SEV", err)
	}

	updaterID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		updaterID = uc.UserID
	}

	// Auto-grant the original reporter visibility when someone else flags
	// their SEV Sensitive (§14) — they shouldn't lose access to their own
	// report the moment it's flagged. Deliberately doesn't also grant
	// updaterID: they already bypass the check as Admin/IC (the only roles
	// allowed to flip this flag's visibility consequences matter for), or
	// can grant themselves via SEVAccessService. Best-effort, and ErrConflict
	// (already granted) is expected and swallowed, not an error.
	if sensitiveFlipped {
		if err := s.access.Grant(ctx, &store.SEVAccess{
			SEVID: record.ID, UserID: record.CreatedBy, CreatedAt: record.UpdatedAt, CreatedBy: updaterID,
		}); err != nil && !errors.Is(err, store.ErrConflict) {
			slog.ErrorContext(ctx, "auto-grant creator access on sensitive flip failed", "sev_id", record.ID, "err", err)
		}
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     record.ID,
		UserID:    updaterID,
		Action:    "sev.updated",
		CreatedAt: record.UpdatedAt,
	})

	resp := sevToProto(record)
	if !record.Sensitive {
		publishProto(s.publisher, record.ID, "sev.updated", resp)
	}

	// §17's "new SEV auto-linked on create": in practice, root cause category
	// is usually identified during investigation rather than at creation
	// time (see the equivalent call in CreateSEV), so this is the point
	// where recurrence detection actually fires for most SEVs — only when
	// the category was just set or changed, not on every unrelated update.
	if rootCauseCategoryChanged {
		s.autoLinkRecurrence(ctx, record, updaterID)
	}

	return resp, nil
}

// sevListFanoutLimit bounds the unpaginated SEVStore.List call
// visibleSEVsForNonPrivileged makes to enforce sensitive-SEV visibility (§14)
// in Go rather than in the store layer — mirrors searchFanoutLimit
// (search.go) and reportFanoutLimit (report.go), the same "single-org v1
// scale" trade-off already accepted elsewhere in this codebase.
const sevListFanoutLimit = 10000

// defaultSEVListLimit is the page size applied when the caller doesn't set
// one, for the same non-privileged path — mirrors defaultSearchLimit
// (search.go) and postgres.SEVStore.List's own default.
const defaultSEVListLimit = 100

func (s *SEVServer) ListSEVs(ctx context.Context, req *pb.ListSEVsRequest) (*pb.ListSEVsResponse, error) {
	filter := store.SEVFilter{
		OnCallUser: req.GetOnCallUser(),
		Search:     req.GetSearch(),
		Limit:      int(req.GetLimit()),
		Offset:     int(req.GetOffset()),
	}
	for _, l := range req.GetSeverityLevels() {
		filter.SeverityLevels = append(filter.SeverityLevels, int16(l))
	}
	for _, st := range req.GetStatuses() {
		filter.Statuses = append(filter.Statuses, store.SEVStatus(st))
	}

	// An Admin or Incident Commander bypasses per-user sensitive-SEV
	// visibility filtering entirely (same trust boundary as
	// sensitiveSEVVisible), so they keep today's fully-pushed-down,
	// SQL-paginated fast path unchanged.
	uc, ok := auth.UserFromContext(ctx)
	if ok && (uc.OrgRole == store.OrgRoleAdmin || uc.OrgRole == store.OrgRoleIncidentCommander) {
		total, err := s.sevs.Count(ctx, filter)
		if err != nil {
			return nil, internalError(ctx, "failed to count SEVs", err)
		}
		records, err := s.sevs.List(ctx, filter)
		if err != nil {
			return nil, internalError(ctx, "failed to list SEVs", err)
		}
		resp := &pb.ListSEVsResponse{Total: int32(total)}
		for _, r := range records {
			resp.Sevs = append(resp.Sevs, sevToProto(r))
		}
		return resp, nil
	}

	visible, err := s.visibleSEVsForNonPrivileged(ctx, filter, uc)
	if err != nil {
		return nil, err
	}
	total := len(visible)
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultSEVListLimit
	}
	offset := filter.Offset
	if offset > len(visible) {
		offset = len(visible)
	}
	visible = visible[offset:]
	if limit < len(visible) {
		visible = visible[:limit]
	}

	resp := &pb.ListSEVsResponse{Total: int32(total)}
	for _, r := range visible {
		resp.Sevs = append(resp.Sevs, sevToProto(r))
	}
	return resp, nil
}

// visibleSEVsForNonPrivileged fetches SEVs matching filter — unpaginated but
// capped at sevListFanoutLimit, since combining a per-user visibility filter
// with SQL-level pagination can't be done correctly in one query (same
// reasoning as SearchServer.searchWithAnnouncements) — and drops any
// Sensitive SEV uc hasn't been explicitly granted access to (§14). uc may be
// nil (unauthenticated caller; defensive only, should be unreachable
// post-interceptor), in which case every Sensitive SEV is dropped.
func (s *SEVServer) visibleSEVsForNonPrivileged(ctx context.Context, filter store.SEVFilter, uc *auth.UserContext) ([]*store.SEV, error) {
	unpaginated := filter
	unpaginated.Limit, unpaginated.Offset = sevListFanoutLimit, 0
	all, err := s.sevs.List(ctx, unpaginated)
	if err != nil {
		return nil, internalError(ctx, "failed to list SEVs", err)
	}
	if len(all) >= sevListFanoutLimit {
		return nil, status.Error(codes.ResourceExhausted, "matched too many SEVs to enforce visibility reliably; narrow the filter")
	}

	accessSet := map[string]bool{}
	if uc != nil {
		ids, err := s.access.ListSEVIDsByUser(ctx, uc.UserID)
		if err != nil {
			return nil, internalError(ctx, "failed to resolve sensitive SEV access", err)
		}
		for _, id := range ids {
			accessSet[id] = true
		}
	}

	visible := make([]*store.SEV, 0, len(all))
	for _, r := range all {
		if !r.Sensitive || accessSet[r.ID] {
			visible = append(visible, r)
		}
	}
	return visible, nil
}

// applyTransitionTimestamps copies the optional lifecycle timestamps req
// sets onto record, then applies the postmortem-complete lock/unlock side
// effect: locked (with postmortem_completed_at defaulted to now if the
// caller didn't supply one) on entering PostmortemComplete, unlocked on
// leaving it (e.g. re-open after postmortem review). The unlock token itself
// is validated by the caller before this runs, when record.Locked was true.
func applyTransitionTimestamps(record *store.SEV, req *pb.TransitionStatusRequest, toStatus store.SEVStatus, now time.Time) {
	if req.GetMitigatedAt() != nil {
		t := req.GetMitigatedAt().AsTime()
		record.MitigatedAt = &t
	}
	if req.GetResolvedAt() != nil {
		t := req.GetResolvedAt().AsTime()
		record.ResolvedAt = &t
	}
	if req.GetPostmortemCompletedAt() != nil {
		t := req.GetPostmortemCompletedAt().AsTime()
		record.PostmortemCompletedAt = &t
	}

	if toStatus == store.SEVStatusPostmortemComplete {
		if record.PostmortemCompletedAt == nil {
			record.PostmortemCompletedAt = &now
		}
		record.Locked = true
	} else {
		record.Locked = false
	}
}

func (s *SEVServer) TransitionStatus(ctx context.Context, req *pb.TransitionStatusRequest) (*pb.SEVResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.GetToStatus() == "" {
		return nil, status.Error(codes.InvalidArgument, "to_status is required")
	}

	toStatus := store.SEVStatus(req.GetToStatus())
	switch toStatus {
	case store.SEVStatusOpen,
		store.SEVStatusInvestigating,
		store.SEVStatusMitigated,
		store.SEVStatusResolved,
		store.SEVStatusPostmortemInProgress,
		store.SEVStatusPostmortemComplete:
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown to_status value")
	}

	record, err := s.sevs.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}
	visible, err := sensitiveSEVVisible(ctx, s.access, record)
	if err != nil {
		return nil, internalError(ctx, "failed to check SEV visibility", err)
	}
	if !visible {
		return nil, status.Error(codes.NotFound, "SEV not found")
	}

	// A locked SEV requires a valid unlock token for any transition.
	if record.Locked {
		if err := validateUnlock(s.unlock, req.GetUnlockToken(), req.GetId()); err != nil {
			return nil, err
		}
	}

	if err := sev.ValidateTransition(record.Status, toStatus); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	fromStatus := record.Status
	record.Status = toStatus

	now := time.Now()
	applyTransitionTimestamps(record, req, toStatus, now)

	sev.ComputeMetrics(record)
	record.UpdatedAt = now

	transitionerID := req.GetUserId()
	if uc, ok := auth.UserFromContext(ctx); ok {
		transitionerID = uc.UserID
	}

	// Write history before updating the SEV so that a history failure leaves
	// the SEV in its previous state and the caller can safely retry.
	if err := s.history.Create(ctx, &store.SEVStatusHistory{
		SEVID:          record.ID,
		FromStatus:     &fromStatus,
		ToStatus:       toStatus,
		UserID:         transitionerID,
		TransitionedAt: now,
	}); err != nil {
		return nil, internalError(ctx, "failed to record status history", err)
	}

	if err := s.sevs.Update(ctx, record); err != nil {
		return nil, internalError(ctx, "failed to update SEV", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     record.ID,
		UserID:    transitionerID,
		Action:    "sev.status_transitioned",
		CreatedAt: now,
	})

	resp := sevToProto(record)
	if !record.Sensitive {
		publishProto(s.publisher, record.ID, "sev.status_changed", resp)
	}
	switch toStatus {
	case store.SEVStatusMitigated:
		s.dispatchAI(ai.TriggerSEVMitigated, record)
	case store.SEVStatusResolved:
		s.dispatchAI(ai.TriggerSEVResolved, record)
	}

	return resp, nil
}

// validateUnlock returns a gRPC status error when the token is missing or invalid for sevID.
func validateUnlock(u Unlocker, token, sevID string) error {
	if u == nil {
		return status.Error(codes.Internal, "lock enforcement not configured")
	}
	if token == "" {
		return status.Error(codes.PermissionDenied, "SEV is locked; provide an unlock_token")
	}
	if err := u.Validate(token, sevID); err != nil {
		if errors.Is(err, postmortem.ErrUnlockTokenExpired) {
			return status.Error(codes.PermissionDenied, "unlock token has expired")
		}
		if errors.Is(err, postmortem.ErrUnlockTokenSEVMismatch) {
			return status.Error(codes.PermissionDenied, "unlock token is not valid for this SEV")
		}
		return status.Error(codes.PermissionDenied, "invalid unlock token")
	}
	return nil
}

func sevToProto(s *store.SEV) *pb.SEVResponse {
	resp := &pb.SEVResponse{
		Id:               s.ID,
		Title:            s.Title,
		Description:      s.Description,
		SeverityLevel:    int32(s.SeverityLevel),
		Status:           string(s.Status),
		AffectedServices: s.AffectedServices,
		Tags:             s.Tags,
		Locked:           s.Locked,
		Sensitive:        s.Sensitive,
		AiDisabled:       s.AIDisabled,
		CreatedBy:        s.CreatedBy,
		CreatedAt:        timestamppb.New(s.CreatedAt),
		UpdatedAt:        timestamppb.New(s.UpdatedAt),
	}

	if s.RootCauseCategory != nil {
		resp.RootCauseCategory = *s.RootCauseCategory
	}
	if s.RootCauseDescription != nil {
		resp.RootCauseDescription = *s.RootCauseDescription
	}
	if s.Mitigation != nil {
		resp.Mitigation = *s.Mitigation
	}
	if s.Prevention != nil {
		resp.Prevention = *s.Prevention
	}
	if s.BusinessImpact != nil {
		resp.BusinessImpact = *s.BusinessImpact
	}
	if s.DetectionMethod != nil {
		resp.DetectionMethod = *s.DetectionMethod
	}
	if s.AlertName != nil {
		resp.AlertName = *s.AlertName
	}
	if s.MonitoringTool != nil {
		resp.MonitoringTool = *s.MonitoringTool
	}
	if s.AlertURL != nil {
		resp.AlertUrl = *s.AlertURL
	}
	if s.MetricLink != nil {
		resp.MetricLink = *s.MetricLink
	}
	if s.SnapshotURL != nil {
		resp.SnapshotUrl = *s.SnapshotURL
	}
	if s.GitHubRepo != nil {
		resp.GithubRepo = *s.GitHubRepo
	}
	if s.RootCauseReferenceURL != nil {
		resp.RootCauseReferenceUrl = *s.RootCauseReferenceURL
	}
	if s.RightPeoplePresent != nil {
		resp.RightPeoplePresent = wrapperspb.Bool(*s.RightPeoplePresent)
	}
	if s.RightPeopleNotes != nil {
		resp.RightPeopleNotes = *s.RightPeopleNotes
	}
	if s.StartedAt != nil {
		resp.StartedAt = timestamppb.New(*s.StartedAt)
	}
	if s.DetectedAt != nil {
		resp.DetectedAt = timestamppb.New(*s.DetectedAt)
	}
	if s.MitigatedAt != nil {
		resp.MitigatedAt = timestamppb.New(*s.MitigatedAt)
	}
	if s.ResolvedAt != nil {
		resp.ResolvedAt = timestamppb.New(*s.ResolvedAt)
	}
	if s.PostmortemCompletedAt != nil {
		resp.PostmortemCompletedAt = timestamppb.New(*s.PostmortemCompletedAt)
	}
	if s.MTTDSeconds != nil {
		resp.MttdSeconds = *s.MTTDSeconds
	}
	if s.MTTMSeconds != nil {
		resp.MttmSeconds = *s.MTTMSeconds
	}
	if s.MTTRSeconds != nil {
		resp.MttrSeconds = *s.MTTRSeconds
	}
	if s.DTTMSeconds != nil {
		resp.DttmSeconds = *s.DTTMSeconds
	}

	return resp
}
