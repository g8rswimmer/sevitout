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

// ReportServer implements pb.ReportServiceServer.
type ReportServer struct {
	pb.UnimplementedReportServiceServer
	sevs        store.SEVStore
	postmortems store.PostmortemStore
	tasks       store.TaskStore
}

// NewReportServer returns a ReportServer backed by the given stores.
func NewReportServer(sevs store.SEVStore, postmortems store.PostmortemStore, tasks store.TaskStore) *ReportServer {
	return &ReportServer{sevs: sevs, postmortems: postmortems, tasks: tasks}
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
