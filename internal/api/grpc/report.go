package grpc

import (
	"bytes"
	"context"
	"encoding/csv"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// reportFanoutLimit bounds the unpaginated SEVStore.List calls
// ReportServer uses to compute aggregate metrics and CSV exports in Go
// rather than in the store layer — mirrors SearchServer's searchFanoutLimit
// (internal/api/grpc/search.go), same rationale: single-org v1 scale (see
// docs/requirements.md §19) makes this acceptable, and hitting the cap fails
// loudly rather than silently returning a partial, misleading aggregate.
const reportFanoutLimit = 10000

// mttrTrendWindows are the reporting windows docs/requirements.md §17
// specifies for the dashboard's MTTR trend.
var mttrTrendWindows = []int{7, 30, 90}

// serviceMetricsWindowDays are the trailing-window choices
// GetServiceMetrics accepts (docs/roadmap.md Phase 13); window_days
// defaults to the first entry when unset or not one of these.
var serviceMetricsWindowDays = []int32{30, 60, 90, 180}

// ReportServer implements pb.ReportServiceServer.
type ReportServer struct {
	pb.UnimplementedReportServiceServer
	sevs        store.SEVStore
	postmortems store.PostmortemStore
	tasks       store.TaskStore
	serviceSLAs store.ServiceSLAStore
}

// NewReportServer returns a ReportServer backed by the given stores.
func NewReportServer(sevs store.SEVStore, postmortems store.PostmortemStore, tasks store.TaskStore, serviceSLAs store.ServiceSLAStore) *ReportServer {
	return &ReportServer{sevs: sevs, postmortems: postmortems, tasks: tasks, serviceSLAs: serviceSLAs}
}

func (s *ReportServer) GetDashboardMetrics(ctx context.Context, _ *pb.GetDashboardMetricsRequest) (*pb.DashboardMetricsResponse, error) {
	records, err := s.fetchAllSEVs(ctx)
	if err != nil {
		return nil, err
	}

	resp := &pb.DashboardMetricsResponse{
		ActiveByLevel:              activeByLevel(records),
		MttrTrends:                 mttrTrends(records, time.Now()),
		FrequencyByServiceAndLevel: frequencyByServiceAndLevel(records),
	}

	counts, err := s.postmortems.CountByStatus(ctx)
	if err != nil {
		return nil, internalError(ctx, "failed to count postmortems", err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total > 0 {
		resp.PostmortemCompletionRate = float64(counts[store.PostmortemStatusApproved]) / float64(total)
	}

	overdue, err := s.tasks.CountOverdue(ctx, time.Now())
	if err != nil {
		return nil, internalError(ctx, "failed to count overdue tasks", err)
	}
	resp.OverdueTaskCount = int32(overdue)

	return resp, nil
}

func (s *ReportServer) GetSEVTrends(ctx context.Context, _ *pb.GetSEVTrendsRequest) (*pb.SEVTrendsResponse, error) {
	records, err := s.fetchAllSEVs(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.SEVTrendsResponse{RecurringPatterns: recurringPatterns(records)}, nil
}

func (s *ReportServer) ExportSEVs(ctx context.Context, req *pb.ExportSEVsRequest) (*httpbody.HttpBody, error) {
	filter := store.SEVFilter{
		RootCauseCategory: req.GetRootCauseCategory(),
		ServiceIDs:        req.GetServiceIds(),
		Limit:             reportFanoutLimit,
		// Reports/exports are Viewer-accessible; there's no sensitive-SEV
		// visibility/ACL mechanism yet (docs/requirements.md §14), so these
		// shouldn't be a way a Sensitive SEV's content leaks out. Same
		// rationale as SearchServer's ExcludeSensitive use (search.go).
		ExcludeSensitive: true,
	}
	for _, l := range req.GetSeverityLevels() {
		filter.SeverityLevels = append(filter.SeverityLevels, int16(l))
	}
	for _, st := range req.GetStatuses() {
		filter.Statuses = append(filter.Statuses, store.SEVStatus(st))
	}
	if req.GetStartedAfter() != nil {
		t := req.GetStartedAfter().AsTime()
		filter.StartedAfter = &t
	}
	if req.GetStartedBefore() != nil {
		t := req.GetStartedBefore().AsTime()
		filter.StartedBefore = &t
	}

	records, err := s.sevs.List(ctx, filter)
	if err != nil {
		return nil, internalError(ctx, "failed to list SEVs", err)
	}
	if len(records) >= reportFanoutLimit {
		return nil, status.Error(codes.ResourceExhausted, "export matched too many SEVs; narrow the filter")
	}

	csvBytes, err := sevsToCSV(records)
	if err != nil {
		return nil, internalError(ctx, "failed to encode CSV", err)
	}

	return &httpbody.HttpBody{
		ContentType: "text/csv; charset=utf-8",
		Data:        csvBytes,
	}, nil
}

// GetServiceMetrics aggregates SEVs opened within the trailing window into
// per-(service, severity level) rollups — docs/roadmap.md Phase 13, the
// aggregate counterpart to Phase 12's per-SEV sla_status.
func (s *ReportServer) GetServiceMetrics(ctx context.Context, req *pb.GetServiceMetricsRequest) (*pb.ServiceMetricsResponse, error) {
	windowDays := resolveWindowDays(req.GetWindowDays())
	cutoff := time.Now().AddDate(0, 0, -int(windowDays))

	records, err := s.sevs.List(ctx, store.SEVFilter{
		StartedAfter:     &cutoff,
		ServiceIDs:       req.GetServiceIds(),
		ExcludeSensitive: true,
		Limit:            reportFanoutLimit,
	})
	if err != nil {
		return nil, internalError(ctx, "failed to list SEVs", err)
	}
	if len(records) >= reportFanoutLimit {
		return nil, status.Error(codes.ResourceExhausted, "too many SEVs to compute an exact report; narrow the filter")
	}

	slaLookup, err := s.buildSLALookup(ctx, records)
	if err != nil {
		return nil, err
	}

	return &pb.ServiceMetricsResponse{
		ServiceLevelMetrics: serviceLevelMetrics(records, slaLookup, time.Now()),
		WindowDays:          windowDays,
	}, nil
}

// resolveWindowDays defaults requested to serviceMetricsWindowDays[0] (30)
// when it isn't one of the accepted values — validation lives here, not in
// the proto, so an unrecognized value degrades to the default instead of
// failing the request.
func resolveWindowDays(requested int32) int32 {
	for _, d := range serviceMetricsWindowDays {
		if requested == d {
			return requested
		}
	}
	return serviceMetricsWindowDays[0]
}

// buildSLALookup batches the SLA-row lookups serviceLevelMetrics needs: one
// ServiceSLAStore.ListForServices call per distinct severity level present
// in records (at most 4 — SEV severity levels are 1-4), regardless of how
// many SEVs are in the window — an improvement on sevToProtoWithSLA's
// already-accepted per-SEV tradeoff (sev.go, Phase 12c) rather than a
// repeat of it.
func (s *ReportServer) buildSLALookup(ctx context.Context, records []*store.SEV) (map[serviceLevelKey]*store.ServiceSLA, error) {
	serviceIDsByLevel := make(map[int16]map[string]struct{})
	for _, r := range records {
		if len(r.AffectedServices) == 0 {
			continue
		}
		set := serviceIDsByLevel[r.SeverityLevel]
		if set == nil {
			set = make(map[string]struct{})
			serviceIDsByLevel[r.SeverityLevel] = set
		}
		for _, svc := range r.AffectedServices {
			set[svc] = struct{}{}
		}
	}

	lookup := make(map[serviceLevelKey]*store.ServiceSLA)
	for level, set := range serviceIDsByLevel {
		serviceIDs := make([]string, 0, len(set))
		for id := range set {
			serviceIDs = append(serviceIDs, id)
		}
		rows, err := s.serviceSLAs.ListForServices(ctx, serviceIDs, level)
		if err != nil {
			return nil, internalError(ctx, "failed to list service SLAs", err)
		}
		for _, row := range rows {
			lookup[serviceLevelKey{row.ServiceID, level}] = row
		}
	}
	return lookup, nil
}

// serviceLevelMetrics groups records by (affected service, severity level)
// — the same grouping frequencyByServiceAndLevel uses — and reduces each
// group to a SEV count, nil-safe MTTD/MTTM/MTTR averages (only SEVs with
// that metric already computed contribute, same discipline as mttrTrends),
// and an SLA compliance breakdown. Per SEV, sev.EvaluateSLA's Overall status
// (evaluated against sev.MostStrictSLA of whatever single row slaLookup has
// for that service+level — an absent entry naturally reduces to "no
// target", MostStrictSLA's own empty-slice behavior; a nil entry is never
// passed in, since MostStrictSLA dereferences each row and a nil element
// would panic) buckets the SEV into ok/at_risk/breached/not_applicable.
func serviceLevelMetrics(records []*store.SEV, slaLookup map[serviceLevelKey]*store.ServiceSLA, now time.Time) []*pb.ServiceLevelMetrics {
	type accum struct {
		count                               int32
		mttdSum, mttmSum, mttrSum           int64
		mttdN, mttmN, mttrN                 int32
		ok, atRisk, breached, notApplicable int32
	}
	groups := make(map[serviceLevelKey]*accum)

	for _, r := range records {
		for _, svc := range r.AffectedServices {
			key := serviceLevelKey{svc, r.SeverityLevel}
			a := groups[key]
			if a == nil {
				a = &accum{}
				groups[key] = a
			}
			a.count++
			if r.MTTDSeconds != nil {
				a.mttdSum += *r.MTTDSeconds
				a.mttdN++
			}
			if r.MTTMSeconds != nil {
				a.mttmSum += *r.MTTMSeconds
				a.mttmN++
			}
			if r.MTTRSeconds != nil {
				a.mttrSum += *r.MTTRSeconds
				a.mttrN++
			}

			var rows []*store.ServiceSLA
			if row, ok := slaLookup[key]; ok {
				rows = []*store.ServiceSLA{row}
			}
			switch sev.EvaluateSLA(r, sev.MostStrictSLA(rows), now).Overall {
			case sev.SLAOnTrack:
				a.ok++
			case sev.SLAAtRisk:
				a.atRisk++
			case sev.SLABreached:
				a.breached++
			default:
				a.notApplicable++
			}
		}
	}

	keys := make([]serviceLevelKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].service != keys[j].service {
			return keys[i].service < keys[j].service
		}
		return keys[i].level < keys[j].level
	})

	out := make([]*pb.ServiceLevelMetrics, 0, len(keys))
	for _, k := range keys {
		a := groups[k]
		m := &pb.ServiceLevelMetrics{
			ServiceId:             k.service,
			SeverityLevel:         int32(k.level),
			SevCount:              a.count,
			SlaOkCount:            a.ok,
			SlaAtRiskCount:        a.atRisk,
			SlaBreachedCount:      a.breached,
			SlaNotApplicableCount: a.notApplicable,
		}
		if a.mttdN > 0 {
			m.AvgMttdSeconds = a.mttdSum / int64(a.mttdN)
		}
		if a.mttmN > 0 {
			m.AvgMttmSeconds = a.mttmSum / int64(a.mttmN)
		}
		if a.mttrN > 0 {
			m.AvgMttrSeconds = a.mttrSum / int64(a.mttrN)
		}
		if measured := a.ok + a.atRisk + a.breached; measured > 0 {
			m.CompliancePct = float64(a.ok) / float64(measured)
		}
		out = append(out, m)
	}
	return out
}

// fetchAllSEVs pulls the full SEV set for in-Go aggregation, failing loudly
// (rather than silently truncating) if it hits reportFanoutLimit — the same
// defensive pattern SearchServer.searchWithAnnouncements uses.
func (s *ReportServer) fetchAllSEVs(ctx context.Context) ([]*store.SEV, error) {
	records, err := s.sevs.List(ctx, store.SEVFilter{Limit: reportFanoutLimit, ExcludeSensitive: true})
	if err != nil {
		return nil, internalError(ctx, "failed to list SEVs", err)
	}
	if len(records) >= reportFanoutLimit {
		return nil, status.Error(codes.ResourceExhausted, "too many SEVs to compute an exact report; contact an admin")
	}
	return records, nil
}

// activeByLevel counts SEVs not yet at Postmortem Complete, grouped by
// severity level. Sorted by severity level for a stable response.
func activeByLevel(records []*store.SEV) []*pb.ActiveSEVCount {
	counts := make(map[int16]int32)
	for _, r := range records {
		if r.Status != store.SEVStatusPostmortemComplete {
			counts[r.SeverityLevel]++
		}
	}
	levels := make([]int16, 0, len(counts))
	for lvl := range counts {
		levels = append(levels, lvl)
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	out := make([]*pb.ActiveSEVCount, 0, len(levels))
	for _, lvl := range levels {
		out = append(out, &pb.ActiveSEVCount{SeverityLevel: int32(lvl), Count: counts[lvl]})
	}
	return out
}

// mttrTrends computes, for each window in mttrTrendWindows, the mean
// MTTRSeconds across SEVs whose ResolvedAt falls within the last N days of
// now. Always returns exactly len(mttrTrendWindows) entries, in the same
// order, so callers don't need to look up by window_days.
func mttrTrends(records []*store.SEV, now time.Time) []*pb.MTTRTrend {
	out := make([]*pb.MTTRTrend, 0, len(mttrTrendWindows))
	for _, days := range mttrTrendWindows {
		cutoff := now.AddDate(0, 0, -days)
		var sum int64
		var n int32
		for _, r := range records {
			if r.ResolvedAt == nil || r.MTTRSeconds == nil {
				continue
			}
			if r.ResolvedAt.Before(cutoff) {
				continue
			}
			sum += *r.MTTRSeconds
			n++
		}
		var avg int64
		if n > 0 {
			avg = sum / int64(n)
		}
		out = append(out, &pb.MTTRTrend{
			WindowDays:         int32(days),
			AverageMttrSeconds: avg,
			SampleSize:         n,
		})
	}
	return out
}

type serviceLevelKey struct {
	service string
	level   int16
}

// frequencyByServiceAndLevel counts SEVs per (affected service, severity
// level) pair. A SEV affecting multiple services is counted once per
// service. Sorted by service ID then severity level for a stable response.
func frequencyByServiceAndLevel(records []*store.SEV) []*pb.ServiceLevelFrequency {
	counts := make(map[serviceLevelKey]int32)
	for _, r := range records {
		for _, svc := range r.AffectedServices {
			counts[serviceLevelKey{svc, r.SeverityLevel}]++
		}
	}
	keys := make([]serviceLevelKey, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].service != keys[j].service {
			return keys[i].service < keys[j].service
		}
		return keys[i].level < keys[j].level
	})
	out := make([]*pb.ServiceLevelFrequency, 0, len(keys))
	for _, k := range keys {
		out = append(out, &pb.ServiceLevelFrequency{
			ServiceId:     k.service,
			SeverityLevel: int32(k.level),
			Count:         counts[k],
		})
	}
	return out
}

type recurringKey struct {
	service   string
	rootCause string
}

// recurringPatterns groups SEVs by (affected service, root cause category)
// and returns every group with 2 or more members — the same "shares same
// service + root cause category" definition sev.go's auto-link logic uses
// (see SEVServer.autoLinkRecurrence), just computed here across the whole
// dataset instead of "prior SEVs matching one new record". SEVs without a
// root cause category set yet are excluded; they can't yet be grouped.
func recurringPatterns(records []*store.SEV) []*pb.RecurringPattern {
	groups := make(map[recurringKey][]*store.SEV)
	for _, r := range records {
		if r.RootCauseCategory == nil || *r.RootCauseCategory == "" {
			continue
		}
		for _, svc := range r.AffectedServices {
			key := recurringKey{svc, *r.RootCauseCategory}
			groups[key] = append(groups[key], r)
		}
	}

	keys := make([]recurringKey, 0, len(groups))
	for k, members := range groups {
		if len(members) >= 2 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].service != keys[j].service {
			return keys[i].service < keys[j].service
		}
		return keys[i].rootCause < keys[j].rootCause
	})

	out := make([]*pb.RecurringPattern, 0, len(keys))
	for _, k := range keys {
		members := groups[k]
		// SortSEVs' zero-value field always orders most-recently-created
		// first regardless of desc, matching every other unspecified-sort
		// default in this codebase (see store/sort.go).
		store.SortSEVs(members, "", false)
		ids := make([]string, len(members))
		for i, m := range members {
			ids[i] = m.ID
		}
		out = append(out, &pb.RecurringPattern{
			ServiceId:         k.service,
			RootCauseCategory: k.rootCause,
			Count:             int32(len(members)),
			SevIds:            ids,
		})
	}
	return out
}

var csvHeader = []string{
	"id", "title", "severity_level", "status", "root_cause_category",
	"affected_services", "started_at", "detected_at", "mitigated_at",
	"resolved_at", "postmortem_completed_at", "mttd_seconds", "mttm_seconds",
	"mttr_seconds", "dttm_seconds", "created_by", "created_at",
}

// sevsToCSV encodes records as CSV with csvHeader as the header row. Values
// are RFC 4180-quoted by encoding/csv wherever needed (e.g. a title
// containing a comma), and timestamps are RFC 3339 in UTC.
func sevsToCSV(records []*store.SEV) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}
	for _, r := range records {
		row := []string{
			r.ID,
			r.Title,
			strconv.Itoa(int(r.SeverityLevel)),
			string(r.Status),
			derefStr(r.RootCauseCategory),
			strings.Join(r.AffectedServices, ";"),
			formatTimePtr(r.StartedAt),
			formatTimePtr(r.DetectedAt),
			formatTimePtr(r.MitigatedAt),
			formatTimePtr(r.ResolvedAt),
			formatTimePtr(r.PostmortemCompletedAt),
			formatInt64Ptr(r.MTTDSeconds),
			formatInt64Ptr(r.MTTMSeconds),
			formatInt64Ptr(r.MTTRSeconds),
			formatInt64Ptr(r.DTTMSeconds),
			r.CreatedBy,
			r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatInt64Ptr(n *int64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatInt(*n, 10)
}
