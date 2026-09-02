package grpc_test

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
	"time"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testReportServer struct {
	server      *grpchandler.ReportServer
	sevs        *memory.SEVStore
	postmortems *memory.PostmortemStore
	tasks       *memory.TaskStore
	serviceSLAs *memory.ServiceSLAStore
}

func newTestReportServer() *testReportServer {
	sevs := memory.NewSEVStore()
	postmortems := memory.NewPostmortemStore()
	tasks := memory.NewTaskStore()
	serviceSLAs := memory.NewServiceSLAStore()
	return &testReportServer{
		server:      grpchandler.NewReportServer(sevs, postmortems, tasks, serviceSLAs),
		sevs:        sevs,
		postmortems: postmortems,
		tasks:       tasks,
		serviceSLAs: serviceSLAs,
	}
}

func mustCreateSEV(t *testing.T, sevs *memory.SEVStore, sv *store.SEV) *store.SEV {
	t.Helper()
	if sv.CreatedAt.IsZero() {
		sv.CreatedAt = time.Now()
	}
	if sv.UpdatedAt.IsZero() {
		sv.UpdatedAt = sv.CreatedAt
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("Create SEV: %v", err)
	}
	return sv
}

func ptrInt64(n int64) *int64        { return &n }
func ptrTime(t time.Time) *time.Time { return &t }

// ── GetDashboardMetrics ──────────────────────────────────────────────────────

func TestGetDashboardMetrics_ActiveByLevel(t *testing.T) {
	ts := newTestReportServer()
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "B", SeverityLevel: 1, Status: store.SEVStatusInvestigating})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "C", SeverityLevel: 2, Status: store.SEVStatusResolved})
	// Postmortem Complete SEVs are not "active".
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "D", SeverityLevel: 2, Status: store.SEVStatusPostmortemComplete})

	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}

	got := map[int32]int32{}
	for _, c := range resp.GetActiveByLevel() {
		got[c.GetSeverityLevel()] = c.GetCount()
	}
	if got[1] != 2 {
		t.Errorf("severity 1 active count = %d, want 2", got[1])
	}
	if got[2] != 1 {
		t.Errorf("severity 2 active count = %d, want 1 (postmortem-complete SEV excluded)", got[2])
	}
}

func TestGetDashboardMetrics_ExcludesSensitiveSEVs(t *testing.T) {
	ts := newTestReportServer()
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "Secret", SeverityLevel: 1, Status: store.SEVStatusOpen, Sensitive: true})

	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}

	got := map[int32]int32{}
	for _, c := range resp.GetActiveByLevel() {
		got[c.GetSeverityLevel()] = c.GetCount()
	}
	if got[1] != 1 {
		t.Errorf("severity 1 active count = %d, want 1 (sensitive SEV excluded)", got[1])
	}
}

func TestGetSEVTrends_ExcludesSensitiveSEVs(t *testing.T) {
	ts := newTestReportServer()
	cat := "deployment"
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 2, AffectedServices: []string{"checkout"}, RootCauseCategory: &cat})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "Secret", SeverityLevel: 2, AffectedServices: []string{"checkout"}, RootCauseCategory: &cat, Sensitive: true})

	resp, err := ts.server.GetSEVTrends(context.Background(), &pb.GetSEVTrendsRequest{})
	if err != nil {
		t.Fatalf("GetSEVTrends: %v", err)
	}
	if len(resp.GetRecurringPatterns()) != 0 {
		t.Errorf("recurring patterns = %v, want none (only 1 non-sensitive SEV in the group)", resp.GetRecurringPatterns())
	}
}

func TestExportSEVs_ExcludesSensitiveSEVs(t *testing.T) {
	ts := newTestReportServer()
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "Secret", SeverityLevel: 1, Status: store.SEVStatusOpen, Sensitive: true})

	body, err := ts.server.ExportSEVs(context.Background(), &pb.ExportSEVsRequest{})
	if err != nil {
		t.Fatalf("ExportSEVs: %v", err)
	}
	if strings.Contains(string(body.GetData()), "Secret") {
		t.Errorf("CSV export contains a sensitive SEV's title:\n%s", body.GetData())
	}
}

func TestGetDashboardMetrics_MTTRTrend(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()

	// Resolved 3 days ago, MTTR 100s: falls inside all three windows.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "recent", SeverityLevel: 1, Status: store.SEVStatusResolved,
		ResolvedAt: ptrTime(now.AddDate(0, 0, -3)), MTTRSeconds: ptrInt64(100),
	})
	// Resolved 60 days ago, MTTR 200s: falls inside the 90-day window only.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "old", SeverityLevel: 1, Status: store.SEVStatusResolved,
		ResolvedAt: ptrTime(now.AddDate(0, 0, -60)), MTTRSeconds: ptrInt64(200),
	})

	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}

	trends := map[int32]*pb.MTTRTrend{}
	for _, tr := range resp.GetMttrTrends() {
		trends[tr.GetWindowDays()] = tr
	}
	if len(trends) != 3 {
		t.Fatalf("want 3 trend windows, got %d", len(trends))
	}
	if trends[7].GetSampleSize() != 1 || trends[7].GetAverageMttrSeconds() != 100 {
		t.Errorf("7-day trend = %+v, want sample_size=1 avg=100", trends[7])
	}
	if trends[90].GetSampleSize() != 2 || trends[90].GetAverageMttrSeconds() != 150 {
		t.Errorf("90-day trend = %+v, want sample_size=2 avg=150", trends[90])
	}
}

func TestGetDashboardMetrics_FrequencyByServiceAndLevel(t *testing.T) {
	ts := newTestReportServer()
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "B", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api", "svc-db"}})

	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}

	got := map[string]int32{}
	for _, f := range resp.GetFrequencyByServiceAndLevel() {
		got[f.GetServiceId()] = f.GetCount()
	}
	if got["svc-api"] != 2 {
		t.Errorf("svc-api count = %d, want 2", got["svc-api"])
	}
	if got["svc-db"] != 1 {
		t.Errorf("svc-db count = %d, want 1", got["svc-db"])
	}
}

func TestGetDashboardMetrics_PostmortemCompletionRate(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()
	for i, s := range []store.PostmortemStatus{store.PostmortemStatusDraft, store.PostmortemStatusInReview, store.PostmortemStatusApproved, store.PostmortemStatusApproved} {
		if err := ts.postmortems.Create(context.Background(), &store.Postmortem{
			SEVID: fmt.Sprintf("SEV-2026-%04d", i+1), Status: s, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("Create postmortem: %v", err)
		}
	}

	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}
	if resp.GetPostmortemCompletionRate() != 0.5 {
		t.Errorf("completion rate = %v, want 0.5 (2 approved of 4)", resp.GetPostmortemCompletionRate())
	}
}

func TestGetDashboardMetrics_PostmortemCompletionRate_NoData(t *testing.T) {
	ts := newTestReportServer()
	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}
	if resp.GetPostmortemCompletionRate() != 0 {
		t.Errorf("completion rate = %v, want 0 with no postmortems", resp.GetPostmortemCompletionRate())
	}
}

func TestGetDashboardMetrics_OverdueTaskCount(t *testing.T) {
	ts := newTestReportServer()
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	_ = ts.tasks.Create(context.Background(), &store.LinkedTask{
		SEVID: "SEV-2026-0001", ExternalSystem: "github", TaskID: "1", URL: "https://x", Title: "overdue",
		RelationshipType: store.TaskRelationshipActionItem, Priority: store.TaskPriorityCritical, DueDate: &past,
	})
	_ = ts.tasks.Create(context.Background(), &store.LinkedTask{
		SEVID: "SEV-2026-0001", ExternalSystem: "github", TaskID: "2", URL: "https://x", Title: "not overdue",
		RelationshipType: store.TaskRelationshipActionItem, Priority: store.TaskPriorityCritical, DueDate: &future,
	})

	resp, err := ts.server.GetDashboardMetrics(context.Background(), &pb.GetDashboardMetricsRequest{})
	if err != nil {
		t.Fatalf("GetDashboardMetrics: %v", err)
	}
	if resp.GetOverdueTaskCount() != 1 {
		t.Errorf("overdue_task_count = %d, want 1", resp.GetOverdueTaskCount())
	}
}

// ── GetSEVTrends ──────────────────────────────────────────────────────────────

func TestGetSEVTrends_SameServiceAndCategoryFlagged(t *testing.T) {
	ts := newTestReportServer()
	cat := "deployment"
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}, RootCauseCategory: &cat})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "B", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}, RootCauseCategory: &cat})

	resp, err := ts.server.GetSEVTrends(context.Background(), &pb.GetSEVTrendsRequest{})
	if err != nil {
		t.Fatalf("GetSEVTrends: %v", err)
	}
	if len(resp.GetRecurringPatterns()) != 1 {
		t.Fatalf("want 1 recurring pattern, got %d", len(resp.GetRecurringPatterns()))
	}
	p := resp.GetRecurringPatterns()[0]
	if p.GetServiceId() != "svc-api" || p.GetRootCauseCategory() != "deployment" || p.GetCount() != 2 {
		t.Errorf("pattern = %+v, want svc-api/deployment/2", p)
	}
}

func TestGetSEVTrends_DifferentServiceNotFlagged(t *testing.T) {
	ts := newTestReportServer()
	cat := "deployment"
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}, RootCauseCategory: &cat})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "B", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-db"}, RootCauseCategory: &cat})

	resp, err := ts.server.GetSEVTrends(context.Background(), &pb.GetSEVTrendsRequest{})
	if err != nil {
		t.Fatalf("GetSEVTrends: %v", err)
	}
	if len(resp.GetRecurringPatterns()) != 0 {
		t.Errorf("want 0 recurring patterns for different services, got %d: %+v", len(resp.GetRecurringPatterns()), resp.GetRecurringPatterns())
	}
}

func TestGetSEVTrends_DifferentCategoryNotFlagged(t *testing.T) {
	ts := newTestReportServer()
	catA, catB := "deployment", "hardware"
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}, RootCauseCategory: &catA})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "B", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}, RootCauseCategory: &catB})

	resp, err := ts.server.GetSEVTrends(context.Background(), &pb.GetSEVTrendsRequest{})
	if err != nil {
		t.Fatalf("GetSEVTrends: %v", err)
	}
	if len(resp.GetRecurringPatterns()) != 0 {
		t.Errorf("want 0 recurring patterns for different categories, got %d", len(resp.GetRecurringPatterns()))
	}
}

func TestGetSEVTrends_NoRootCauseExcluded(t *testing.T) {
	ts := newTestReportServer()
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "B", SeverityLevel: 1, Status: store.SEVStatusOpen, AffectedServices: []string{"svc-api"}})

	resp, err := ts.server.GetSEVTrends(context.Background(), &pb.GetSEVTrendsRequest{})
	if err != nil {
		t.Fatalf("GetSEVTrends: %v", err)
	}
	if len(resp.GetRecurringPatterns()) != 0 {
		t.Errorf("want 0 recurring patterns without a root cause category, got %d", len(resp.GetRecurringPatterns()))
	}
}

// ── ExportSEVs ────────────────────────────────────────────────────────────────

func TestExportSEVs_CSVFormat(t *testing.T) {
	ts := newTestReportServer()
	cat := "deployment"
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "Outage, big one", SeverityLevel: 1, Status: store.SEVStatusResolved,
		RootCauseCategory: &cat, AffectedServices: []string{"svc-api", "svc-db"},
		CreatedBy: "user-1",
	})

	body, err := ts.server.ExportSEVs(context.Background(), &pb.ExportSEVsRequest{})
	if err != nil {
		t.Fatalf("ExportSEVs: %v", err)
	}
	if body.GetContentType() != "text/csv; charset=utf-8" {
		t.Errorf("content_type = %q, want text/csv", body.GetContentType())
	}

	r := csv.NewReader(strings.NewReader(string(body.GetData())))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want header + 1 row, got %d rows", len(rows))
	}
	header := rows[0]
	if header[0] != "id" || header[1] != "title" {
		t.Errorf("header = %v, want id,title,...", header)
	}
	row := rows[1]
	if row[1] != "Outage, big one" {
		t.Errorf("title cell = %q, want %q (comma preserved via CSV quoting)", row[1], "Outage, big one")
	}
	if row[4] != "deployment" {
		t.Errorf("root_cause_category cell = %q, want deployment", row[4])
	}
	if row[5] != "svc-api;svc-db" {
		t.Errorf("affected_services cell = %q, want svc-api;svc-db", row[5])
	}
}

func TestExportSEVs_EmptyResult(t *testing.T) {
	ts := newTestReportServer()
	body, err := ts.server.ExportSEVs(context.Background(), &pb.ExportSEVsRequest{})
	if err != nil {
		t.Fatalf("ExportSEVs: %v", err)
	}
	r := csv.NewReader(strings.NewReader(string(body.GetData())))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want header row only, got %d rows", len(rows))
	}
}

// ── GetServiceMetrics ────────────────────────────────────────────────────────

func TestGetServiceMetrics_MultipleServicesAndSeverities(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "A", SeverityLevel: 1, Status: store.SEVStatusOpen,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -5)),
	})
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "B", SeverityLevel: 1, Status: store.SEVStatusOpen,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -6)),
	})
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "C", SeverityLevel: 2, Status: store.SEVStatusOpen,
		AffectedServices: []string{"payments-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -7)),
	})

	resp, err := ts.server.GetServiceMetrics(context.Background(), &pb.GetServiceMetricsRequest{})
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}
	if resp.GetWindowDays() != 30 {
		t.Errorf("window_days = %d, want default 30", resp.GetWindowDays())
	}

	got := map[string]int32{}
	for _, m := range resp.GetServiceLevelMetrics() {
		got[fmt.Sprintf("%s:%d", m.GetServiceId(), m.GetSeverityLevel())] = m.GetSevCount()
	}
	if got["checkout-api:1"] != 2 {
		t.Errorf("checkout-api SEV-1 count = %d, want 2", got["checkout-api:1"])
	}
	if got["payments-api:2"] != 1 {
		t.Errorf("payments-api SEV-2 count = %d, want 1", got["payments-api:2"])
	}
}

func TestGetServiceMetrics_PartialMetricsDontSkewAverages(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "A", SeverityLevel: 1, Status: store.SEVStatusResolved,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -1)),
		MTTDSeconds: ptrInt64(100), MTTMSeconds: ptrInt64(200), MTTRSeconds: ptrInt64(300),
	})
	// No MTTM yet (still in progress past detection) — must not pull the
	// group's average MTTM toward 0, and must not be excluded from the
	// MTTD/MTTR averages it does have.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "B", SeverityLevel: 1, Status: store.SEVStatusInvestigating,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -2)),
		MTTDSeconds: ptrInt64(300), MTTRSeconds: ptrInt64(900),
	})

	resp, err := ts.server.GetServiceMetrics(context.Background(), &pb.GetServiceMetricsRequest{})
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}
	if len(resp.GetServiceLevelMetrics()) != 1 {
		t.Fatalf("want 1 group, got %d", len(resp.GetServiceLevelMetrics()))
	}
	m := resp.GetServiceLevelMetrics()[0]
	if m.GetAvgMttdSeconds() != 200 {
		t.Errorf("avg_mttd_seconds = %d, want 200 ((100+300)/2)", m.GetAvgMttdSeconds())
	}
	if m.GetAvgMttmSeconds() != 200 {
		t.Errorf("avg_mttm_seconds = %d, want 200 (only A has MTTM, not skewed by B's nil)", m.GetAvgMttmSeconds())
	}
	if m.GetAvgMttrSeconds() != 600 {
		t.Errorf("avg_mttr_seconds = %d, want 600 ((300+900)/2)", m.GetAvgMttrSeconds())
	}
}

func TestGetServiceMetrics_NoSLAConfiguredLandsInNotApplicable(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "A", SeverityLevel: 3, Status: store.SEVStatusOpen,
		AffectedServices: []string{"notification-service"}, StartedAt: ptrTime(now.AddDate(0, 0, -1)),
	})
	// No ServiceSLA row upserted for notification-service/SEV-3 at all.

	resp, err := ts.server.GetServiceMetrics(context.Background(), &pb.GetServiceMetricsRequest{})
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}
	if len(resp.GetServiceLevelMetrics()) != 1 {
		t.Fatalf("want 1 group, got %d", len(resp.GetServiceLevelMetrics()))
	}
	m := resp.GetServiceLevelMetrics()[0]
	if m.GetSlaNotApplicableCount() != 1 {
		t.Errorf("sla_not_applicable_count = %d, want 1", m.GetSlaNotApplicableCount())
	}
	if m.GetCompliancePct() != 0 {
		t.Errorf("compliance_pct = %v, want 0 (not a real 0%% — every SEV is not_applicable)", m.GetCompliancePct())
	}
}

func TestGetServiceMetrics_ComplianceBreakdown(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()
	if err := ts.serviceSLAs.Upsert(context.Background(), &store.ServiceSLA{
		ServiceID: "checkout-api", SeverityLevel: 1, MTTRTargetSeconds: ptrInt64(3600),
	}); err != nil {
		t.Fatalf("Upsert ServiceSLA: %v", err)
	}
	// Under target: on track.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "A", SeverityLevel: 1, Status: store.SEVStatusResolved,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -1)),
		MTTRSeconds: ptrInt64(1800),
	})
	// Over target: breached.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "B", SeverityLevel: 1, Status: store.SEVStatusResolved,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -2)),
		MTTRSeconds: ptrInt64(7200),
	})

	resp, err := ts.server.GetServiceMetrics(context.Background(), &pb.GetServiceMetricsRequest{})
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}
	if len(resp.GetServiceLevelMetrics()) != 1 {
		t.Fatalf("want 1 group, got %d", len(resp.GetServiceLevelMetrics()))
	}
	m := resp.GetServiceLevelMetrics()[0]
	if m.GetSlaOkCount() != 1 || m.GetSlaBreachedCount() != 1 {
		t.Errorf("ok/breached = %d/%d, want 1/1", m.GetSlaOkCount(), m.GetSlaBreachedCount())
	}
	if m.GetCompliancePct() != 0.5 {
		t.Errorf("compliance_pct = %v, want 0.5", m.GetCompliancePct())
	}
}

func TestGetServiceMetrics_WindowCutoffBoundary(t *testing.T) {
	ts := newTestReportServer()
	now := time.Now()
	// Well inside a 30-day window.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "in-window", SeverityLevel: 1, Status: store.SEVStatusOpen,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -10)),
	})
	// Just outside a 30-day window.
	mustCreateSEV(t, ts.sevs, &store.SEV{
		Title: "out-of-window", SeverityLevel: 1, Status: store.SEVStatusOpen,
		AffectedServices: []string{"checkout-api"}, StartedAt: ptrTime(now.AddDate(0, 0, -40)),
	})

	resp, err := ts.server.GetServiceMetrics(context.Background(), &pb.GetServiceMetricsRequest{WindowDays: 30})
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}
	if len(resp.GetServiceLevelMetrics()) != 1 {
		t.Fatalf("want 1 group, got %d", len(resp.GetServiceLevelMetrics()))
	}
	if got := resp.GetServiceLevelMetrics()[0].GetSevCount(); got != 1 {
		t.Errorf("sev_count = %d, want 1 (out-of-window SEV excluded)", got)
	}
}

func TestGetServiceMetrics_UnrecognizedWindowDefaultsTo30(t *testing.T) {
	ts := newTestReportServer()
	resp, err := ts.server.GetServiceMetrics(context.Background(), &pb.GetServiceMetricsRequest{WindowDays: 45})
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}
	if resp.GetWindowDays() != 30 {
		t.Errorf("window_days = %d, want 30 (45 isn't one of 30/60/90/180)", resp.GetWindowDays())
	}
}

func TestExportSEVs_FilterBySeverity(t *testing.T) {
	ts := newTestReportServer()
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "SEV1", SeverityLevel: 1, Status: store.SEVStatusOpen})
	mustCreateSEV(t, ts.sevs, &store.SEV{Title: "SEV2", SeverityLevel: 2, Status: store.SEVStatusOpen})

	body, err := ts.server.ExportSEVs(context.Background(), &pb.ExportSEVsRequest{SeverityLevels: []int32{1}})
	if err != nil {
		t.Fatalf("ExportSEVs: %v", err)
	}
	r := csv.NewReader(strings.NewReader(string(body.GetData())))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want header + 1 row for severity filter, got %d", len(rows))
	}
	if rows[1][1] != "SEV1" {
		t.Errorf("title = %q, want SEV1", rows[1][1])
	}
}
