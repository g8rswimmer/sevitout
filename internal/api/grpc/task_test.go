package grpc_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// ── fake IssueClient ─────────────────────────────────────────────────────────

// capturedCreateIssue records one CreateIssue invocation on fakeIssueClient.
type capturedCreateIssue struct {
	Owner, Repo, Title, Body string
	Labels                   []string
}

type fakeIssueClient struct {
	issue *grpchandler.CreatedIssue
	err   error

	// errsByCall, if set, returns a per-call error by call index (0-based);
	// once exhausted, subsequent calls fall back to issue/err. Lets tests
	// simulate "fails once, then succeeds on retry."
	errsByCall []error

	calls []capturedCreateIssue
}

func (f *fakeIssueClient) CreateIssue(_ context.Context, owner, repo, title, body string, labels []string) (*grpchandler.CreatedIssue, error) {
	idx := len(f.calls)
	f.calls = append(f.calls, capturedCreateIssue{owner, repo, title, body, labels})
	if idx < len(f.errsByCall) {
		if err := f.errsByCall[idx]; err != nil {
			return nil, err
		}
	}
	return f.issue, f.err
}

// ── helpers ───────────────────────────────────────────────────────────────────

type testTaskServer struct {
	server *grpchandler.TaskServer
	tasks  *memory.TaskStore
	sevs   *memory.SEVStore
	access *memory.SEVAccessStore
	audit  *memory.AuditStore
	pub    *fakePublisher
}

func newTestTaskServer(gh grpchandler.IssueClient) *testTaskServer {
	tasks := memory.NewTaskStore()
	sevs := memory.NewSEVStore()
	access := memory.NewSEVAccessStore()
	audit := memory.NewAuditStore()
	pub := &fakePublisher{}
	return &testTaskServer{
		server: grpchandler.NewTaskServer(grpchandler.TaskServerParams{
			Tasks: tasks, SEVs: sevs, Access: access, Audit: audit, GitHub: gh, Publisher: pub,
		}),
		tasks:  tasks,
		sevs:   sevs,
		access: access,
		audit:  audit,
		pub:    pub,
	}
}

func seedSEVForTask(t *testing.T, ts *testTaskServer, resolvedAt *time.Time) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "test SEV",
		SeverityLevel: 2,
		Status:        store.SEVStatusOpen,
		ResolvedAt:    resolvedAt,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForTask: %v", err)
	}
	return sv.ID
}

func linkTaskReq(sevID, priority, relType string) *pb.LinkTaskRequest {
	return &pb.LinkTaskRequest{
		SevId:            sevID,
		ExternalSystem:   "github",
		TaskId:           "owner/repo#1",
		Url:              "https://github.com/owner/repo/issues/1",
		Title:            "Fix the bug",
		RelationshipType: relType,
		Priority:         priority,
	}
}

// ── SLA due date calculation ──────────────────────────────────────────────────

func TestComputeDueDate_Critical(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	resolved := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	sevID := seedSEVForTask(t, ts, &resolved)

	resp, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if resp.GetDueDate() == nil {
		t.Fatal("due_date should be set when SEV is resolved")
	}
	want := resolved.AddDate(0, 0, 30)
	got := resp.GetDueDate().AsTime().UTC()
	if !got.Equal(want) {
		t.Errorf("critical due date: got %v, want %v", got, want)
	}
}

func TestComputeDueDate_NonCritical(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	resolved := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	sevID := seedSEVForTask(t, ts, &resolved)

	resp, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "non-critical", "follow-up"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if resp.GetDueDate() == nil {
		t.Fatal("due_date should be set when SEV is resolved")
	}
	want := resolved.AddDate(0, 0, 90)
	got := resp.GetDueDate().AsTime().UTC()
	if !got.Equal(want) {
		t.Errorf("non-critical due date: got %v, want %v", got, want)
	}
}

func TestComputeDueDate_UnresolvedSEV(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil) // SEV not yet resolved

	resp, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if resp.GetDueDate() != nil {
		t.Errorf("due_date should be nil when SEV is unresolved, got %v", resp.GetDueDate())
	}
}

func TestComputeDueDate_ManualOverride(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	resolved := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	sevID := seedSEVForTask(t, ts, &resolved)

	override := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	req := linkTaskReq(sevID, "critical", "action-item")
	req.DueDate = timestamppb.New(override)

	resp, err := ts.server.LinkTask(ctx, req)
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	got := resp.GetDueDate().AsTime().UTC()
	if !got.Equal(override) {
		t.Errorf("manual due date: got %v, want %v", got, override)
	}
}

// When a SEV resolves after tasks are linked, ListTasks should back-fill due dates.
func TestListTasks_BackfillsDueDateOnResolve(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-viewing"})

	sevID := seedSEVForTask(t, ts, nil) // unresolved
	req := linkTaskReq(sevID, "critical", "action-item")
	if _, err := ts.server.LinkTask(ctx, req); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	// Now resolve the SEV.
	resolved := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	sv, _ := ts.sevs.Get(ctx, sevID)
	sv.ResolvedAt = &resolved
	sv.Status = store.SEVStatusResolved
	_ = ts.sevs.Update(ctx, sv)

	resp, err := ts.server.ListTasks(ctx, &pb.ListTasksRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(resp.GetTasks()) == 0 {
		t.Fatal("expected 1 task")
	}
	got := resp.GetTasks()[0]
	if got.GetDueDate() == nil {
		t.Fatal("due_date should be set after SEV resolved")
	}
	want := resolved.AddDate(0, 0, 30)
	gotTime := got.GetDueDate().AsTime().UTC()
	if !gotTime.Equal(want) {
		t.Errorf("back-filled due date: got %v, want %v", gotTime, want)
	}

	entries, _ := ts.audit.ListBySEVID(ctx, sevID)
	found := false
	for _, e := range entries {
		if e.Action == "task.due_date_backfilled" {
			found = true
			if e.UserID != "user-viewing" {
				t.Errorf("backfill audit entry UserID: got %q, want %q", e.UserID, "user-viewing")
			}
		}
	}
	if !found {
		t.Error("no audit entry with action task.due_date_backfilled")
	}
}

// Concurrent ListTasks calls for the same just-resolved SEV must not each
// independently backfill and audit the same task.
func TestListTasks_ConcurrentBackfillIsNotDuplicated(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	sevID := seedSEVForTask(t, ts, nil) // unresolved
	if _, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item")); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	resolved := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	sv, _ := ts.sevs.Get(ctx, sevID)
	sv.ResolvedAt = &resolved
	sv.Status = store.SEVStatusResolved
	_ = ts.sevs.Update(ctx, sv)

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			if _, err := ts.server.ListTasks(ctx, &pb.ListTasksRequest{SevId: sevID}); err != nil {
				t.Errorf("ListTasks: %v", err)
			}
		}()
	}
	wg.Wait()

	entries, _ := ts.audit.ListBySEVID(ctx, sevID)
	backfills := 0
	for _, e := range entries {
		if e.Action == "task.due_date_backfilled" {
			backfills++
		}
	}
	if backfills != 1 {
		t.Errorf("want exactly 1 task.due_date_backfilled audit entry across %d concurrent ListTasks calls, got %d", concurrency, backfills)
	}
}

// ── Overdue detection ─────────────────────────────────────────────────────────

func TestOverdue_PastDueDate(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	pastResolved := time.Now().Add(-200 * 24 * time.Hour) // 200 days ago
	sevID := seedSEVForTask(t, ts, &pastResolved)

	resp, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	// resolved 200 days ago + 30 day SLA = 170 days ago → overdue
	if !resp.GetOverdue() {
		t.Error("task should be overdue when due_date is in the past")
	}
}

func TestOverdue_FutureDueDate(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	futureResolved := time.Now().Add(-1 * 24 * time.Hour) // 1 day ago
	sevID := seedSEVForTask(t, ts, &futureResolved)

	resp, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	// resolved 1 day ago + 30 days = 29 days in the future → not overdue
	if resp.GetOverdue() {
		t.Error("task should not be overdue when due_date is in the future")
	}
}

func TestOverdue_NilDueDate(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	resp, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if resp.GetOverdue() {
		t.Error("task without due date should not be overdue")
	}
}

// ── LinkTask ──────────────────────────────────────────────────────────────────

func TestLinkTask_MissingURL(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "critical", "action-item")
	req.Url = ""
	_, err := ts.server.LinkTask(ctx, req)
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkTask_MissingTitle(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "critical", "action-item")
	req.Title = ""
	_, err := ts.server.LinkTask(ctx, req)
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkTask_InvalidRelationshipType(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "critical", "blocks")
	_, err := ts.server.LinkTask(ctx, req)
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkTask_InvalidPriority(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "urgent", "action-item")
	_, err := ts.server.LinkTask(ctx, req)
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkTask_MissingExternalSystem(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "critical", "action-item")
	req.ExternalSystem = ""
	_, err := ts.server.LinkTask(ctx, req)
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkTask_MissingTaskID(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "critical", "action-item")
	req.TaskId = ""
	_, err := ts.server.LinkTask(ctx, req)
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestLinkTask_DuplicateRejected(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	req := linkTaskReq(sevID, "critical", "action-item")
	if _, err := ts.server.LinkTask(ctx, req); err != nil {
		t.Fatalf("first LinkTask: %v", err)
	}

	_, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if grpcCode(err) != codes.AlreadyExists {
		t.Errorf("want AlreadyExists for duplicate (sev_id, external_system, task_id), got %v", grpcCode(err))
	}
}

func TestLinkTask_SEVNotFound(t *testing.T) {
	ts := newTestTaskServer(nil)
	req := linkTaskReq("SEV-9999-0001", "critical", "action-item")
	_, err := ts.server.LinkTask(context.Background(), req)
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestLinkTask_AllRelationshipTypes(t *testing.T) {
	relTypes := []string{"action-item", "contributing-factor", "follow-up", "dependency"}
	for _, rt := range relTypes {
		ts := newTestTaskServer(nil)
		ctx := context.Background()
		sevID := seedSEVForTask(t, ts, nil)
		req := linkTaskReq(sevID, "non-critical", rt)
		if _, err := ts.server.LinkTask(ctx, req); err != nil {
			t.Errorf("LinkTask with relationship %q: %v", rt, err)
		}
	}
}

func TestLinkTask_AuditEntry(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	if _, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item")); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	entries, _ := ts.audit.ListBySEVID(ctx, sevID)
	found := false
	for _, e := range entries {
		if e.Action == "task.linked" {
			found = true
		}
	}
	if !found {
		t.Error("no audit entry with action task.linked")
	}
}

// ── UnlinkTask ────────────────────────────────────────────────────────────────

func TestUnlinkTask_Valid(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	linked, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	if _, err := ts.server.UnlinkTask(ctx, &pb.UnlinkTaskRequest{
		SevId: sevID,
		Id:    linked.GetId(),
	}); err != nil {
		t.Fatalf("UnlinkTask: %v", err)
	}

	resp, _ := ts.server.ListTasks(ctx, &pb.ListTasksRequest{SevId: sevID})
	if len(resp.GetTasks()) != 0 {
		t.Errorf("want 0 tasks after unlink, got %d", len(resp.GetTasks()))
	}
}

func TestUnlinkTask_NotFound(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	_, err := ts.server.UnlinkTask(ctx, &pb.UnlinkTaskRequest{
		SevId: sevID,
		Id:    999,
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestUnlinkTask_WrongSEV(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sev1 := seedSEVForTask(t, ts, nil)
	sev2 := seedSEVForTask(t, ts, nil)

	linked, _ := ts.server.LinkTask(ctx, linkTaskReq(sev1, "critical", "action-item"))

	_, err := ts.server.UnlinkTask(ctx, &pb.UnlinkTaskRequest{
		SevId: sev2,
		Id:    linked.GetId(),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound for wrong sev_id, got %v", grpcCode(err))
	}
}

// ── UpdateTaskDueDate ─────────────────────────────────────────────────────────

func TestUpdateTaskDueDate_Valid(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	linked, _ := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))

	newDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	resp, err := ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      linked.GetId(),
		DueDate: timestamppb.New(newDate),
	})
	if err != nil {
		t.Fatalf("UpdateTaskDueDate: %v", err)
	}
	got := resp.GetDueDate().AsTime().UTC()
	if !got.Equal(newDate) {
		t.Errorf("due date: got %v, want %v", got, newDate)
	}
}

func TestUpdateTaskDueDate_MarkOverdue(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	linked, _ := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))

	pastDate := time.Now().Add(-24 * time.Hour)
	resp, err := ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      linked.GetId(),
		DueDate: timestamppb.New(pastDate),
	})
	if err != nil {
		t.Fatalf("UpdateTaskDueDate: %v", err)
	}
	if !resp.GetOverdue() {
		t.Error("task should be overdue after setting past due date")
	}
}

func TestUpdateTaskDueDate_NotFound(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	_, err := ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      999,
		DueDate: timestamppb.New(time.Now()),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// ── CreateGitHubIssue ─────────────────────────────────────────────────────────

func TestCreateGitHubIssue_Valid(t *testing.T) {
	gh := &fakeIssueClient{
		issue: &grpchandler.CreatedIssue{
			Number: 42,
			Title:  "SEV follow-up",
			Body:   "details",
			URL:    "https://github.com/acme/api/issues/42",
		},
	}
	ts := newTestTaskServer(gh)
	ctx := context.Background()

	resolved := time.Now().Add(-24 * time.Hour)
	sevID := seedSEVForTask(t, ts, &resolved)

	resp, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId:            sevID,
		Owner:            "acme",
		Repo:             "api",
		Title:            "SEV follow-up",
		Body:             "details",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if err != nil {
		t.Fatalf("CreateGitHubIssue: %v", err)
	}
	if resp.GetExternalSystem() != "github" {
		t.Errorf("external_system: got %q, want github", resp.GetExternalSystem())
	}
	if resp.GetTaskId() != "acme/api#42" {
		t.Errorf("task_id: got %q, want acme/api#42", resp.GetTaskId())
	}
	if resp.GetUrl() != "https://github.com/acme/api/issues/42" {
		t.Errorf("url mismatch: got %q", resp.GetUrl())
	}
	// resolved 1 day ago + 30 day critical SLA = future → not overdue
	if resp.GetOverdue() {
		t.Error("task should not be overdue immediately after creation")
	}
	wantLabels := []string{sevID, "critical"}
	if len(gh.calls) != 1 {
		t.Fatalf("want 1 CreateIssue call, got %d", len(gh.calls))
	}
	if !reflect.DeepEqual(gh.calls[0].Labels, wantLabels) {
		t.Errorf("labels: got %v, want %v", gh.calls[0].Labels, wantLabels)
	}
}

func TestCreateGitHubIssue_RetriesWithoutLabelsOn422(t *testing.T) {
	gh := &fakeIssueClient{
		issue:      &grpchandler.CreatedIssue{Number: 7, Title: "t", URL: "https://github.com/acme/api/issues/7"},
		errsByCall: []error{&github.APIError{StatusCode: http.StatusUnprocessableEntity, Message: "label not found"}},
	}
	ts := newTestTaskServer(gh)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	resp, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId:            sevID,
		Owner:            "acme",
		Repo:             "api",
		Title:            "issue",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if err != nil {
		t.Fatalf("CreateGitHubIssue should succeed after retrying without labels: %v", err)
	}
	if resp.GetUrl() != "https://github.com/acme/api/issues/7" {
		t.Errorf("url mismatch: got %q", resp.GetUrl())
	}
	if len(gh.calls) != 2 {
		t.Fatalf("want 2 CreateIssue calls (initial + retry), got %d", len(gh.calls))
	}
	if len(gh.calls[0].Labels) == 0 {
		t.Error("first call should have included labels")
	}
	if len(gh.calls[1].Labels) != 0 {
		t.Errorf("retry call should omit labels, got %v", gh.calls[1].Labels)
	}
}

func TestCreateGitHubIssue_NotConfigured(t *testing.T) {
	ts := newTestTaskServer(nil) // nil IssueClient
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	_, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId:            sevID,
		Owner:            "acme",
		Repo:             "api",
		Title:            "issue",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("want Unavailable when GitHub not configured, got %v", grpcCode(err))
	}
}

func TestCreateGitHubIssue_GitHubAPIError_MapsToStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       codes.Code
	}{
		{"forbidden", http.StatusForbidden, codes.PermissionDenied},
		{"unauthorized", http.StatusUnauthorized, codes.Unauthenticated},
		{"not found", http.StatusNotFound, codes.NotFound},
		{"unprocessable", http.StatusUnprocessableEntity, codes.InvalidArgument},
		{"rate limited", http.StatusTooManyRequests, codes.ResourceExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := &fakeIssueClient{err: &github.APIError{StatusCode: tt.statusCode, Message: "denied"}}
			ts := newTestTaskServer(gh)
			ctx := context.Background()
			sevID := seedSEVForTask(t, ts, nil)

			_, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
				SevId:            sevID,
				Owner:            "acme",
				Repo:             "api",
				Title:            "issue",
				RelationshipType: "action-item",
				Priority:         "critical",
			})
			if grpcCode(err) != tt.want {
				t.Errorf("status %d: want %v, got %v (%v)", tt.statusCode, tt.want, grpcCode(err), err)
			}
		})
	}
}

func TestCreateGitHubIssue_GitHubError(t *testing.T) {
	gh := &fakeIssueClient{err: errors.New("403 forbidden")}
	ts := newTestTaskServer(gh)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	_, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId:            sevID,
		Owner:            "acme",
		Repo:             "api",
		Title:            "issue",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if grpcCode(err) != codes.Internal {
		t.Errorf("want Internal on GitHub error, got %v", grpcCode(err))
	}
}

func TestCreateGitHubIssue_DuplicateLinkReturnsAlreadyExists(t *testing.T) {
	gh := &fakeIssueClient{issue: &grpchandler.CreatedIssue{Number: 1, Title: "t", URL: "https://github.com/acme/api/issues/1"}}
	ts := newTestTaskServer(gh)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	// Pre-register the same (sev_id, "github", "acme/api#1") reference via
	// LinkTask so CreateGitHubIssue's Create call hits the same conflict
	// memory.TaskStore already enforces for LinkTask.
	if _, err := ts.server.LinkTask(ctx, &pb.LinkTaskRequest{
		SevId:            sevID,
		ExternalSystem:   "github",
		TaskId:           "acme/api#1",
		Url:              "https://github.com/acme/api/issues/1",
		Title:            "pre-existing",
		RelationshipType: "action-item",
		Priority:         "critical",
	}); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	_, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId:            sevID,
		Owner:            "acme",
		Repo:             "api",
		Title:            "issue",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if grpcCode(err) != codes.AlreadyExists {
		t.Errorf("want AlreadyExists for duplicate (sev_id, external_system, task_id), got %v", grpcCode(err))
	}
}

func TestCreateGitHubIssue_SEVNotFound(t *testing.T) {
	gh := &fakeIssueClient{issue: &grpchandler.CreatedIssue{Number: 1, Title: "t", URL: "u"}}
	ts := newTestTaskServer(gh)

	_, err := ts.server.CreateGitHubIssue(context.Background(), &pb.CreateGitHubIssueRequest{
		SevId:            "SEV-9999-0001",
		Owner:            "acme",
		Repo:             "api",
		Title:            "issue",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// ── ListTasks ─────────────────────────────────────────────────────────────────

func TestListTasks_Empty(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	resp, err := ts.server.ListTasks(ctx, &pb.ListTasksRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(resp.GetTasks()) != 0 {
		t.Errorf("want 0 tasks, got %d", len(resp.GetTasks()))
	}
}

func TestListTasks_SEVNotFound(t *testing.T) {
	ts := newTestTaskServer(nil)
	_, err := ts.server.ListTasks(context.Background(), &pb.ListTasksRequest{SevId: "SEV-9999-0001"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// Integration: link → verify due date → override → verify overdue after date passes.
func TestTask_IntegrationFlow(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()

	// 1. Link task to unresolved SEV → no due date.
	sevID := seedSEVForTask(t, ts, nil)
	linked, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if linked.GetDueDate() != nil {
		t.Errorf("step 1: want nil due date, got %v", linked.GetDueDate())
	}

	// 2. Override due date manually.
	futureDate := time.Now().Add(10 * 24 * time.Hour)
	updated, err := ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      linked.GetId(),
		DueDate: timestamppb.New(futureDate),
	})
	if err != nil {
		t.Fatalf("UpdateTaskDueDate: %v", err)
	}
	if updated.GetOverdue() {
		t.Error("step 2: task should not yet be overdue")
	}

	// 3. Simulate overdue by setting a past due date.
	pastDate := time.Now().Add(-24 * time.Hour)
	overdue, err := ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      linked.GetId(),
		DueDate: timestamppb.New(pastDate),
	})
	if err != nil {
		t.Fatalf("UpdateTaskDueDate (past): %v", err)
	}
	if !overdue.GetOverdue() {
		t.Error("step 3: task should be overdue after past due date set")
	}
}

// ── WebSocket event publishing ────────────────────────────────────────────────

func TestLinkTask_PublishesEvent(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	if _, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item")); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "task.linked" {
		t.Errorf("event = %+v, want sev_id=%q type=task.linked", events[0], sevID)
	}
}

func TestUnlinkTask_PublishesEvent(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	linked, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	if _, err := ts.server.UnlinkTask(ctx, &pb.UnlinkTaskRequest{SevId: sevID, Id: linked.GetId()}); err != nil {
		t.Fatalf("UnlinkTask: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 2 {
		t.Fatalf("published events = %d, want 2 (link + unlink): %+v", len(events), events)
	}
	last := events[1]
	if last.sevID != sevID || last.eventType != "task.updated" {
		t.Errorf("event = %+v, want sev_id=%q type=task.updated", last, sevID)
	}
}

func TestUpdateTaskDueDate_PublishesEvent(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	linked, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	_, err = ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      linked.GetId(),
		DueDate: timestamppb.New(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("UpdateTaskDueDate: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 2 {
		t.Fatalf("published events = %d, want 2 (link + due-date update): %+v", len(events), events)
	}
	last := events[1]
	if last.sevID != sevID || last.eventType != "task.updated" {
		t.Errorf("event = %+v, want sev_id=%q type=task.updated", last, sevID)
	}
}

func TestCreateGitHubIssue_PublishesEvent(t *testing.T) {
	gh := &fakeIssueClient{
		issue: &grpchandler.CreatedIssue{
			Number: 42,
			Title:  "SEV follow-up",
			Body:   "details",
			URL:    "https://github.com/acme/api/issues/42",
		},
	}
	ts := newTestTaskServer(gh)
	ctx := context.Background()
	sevID := seedSEVForTask(t, ts, nil)

	_, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId:            sevID,
		Owner:            "acme",
		Repo:             "api",
		Title:            "SEV follow-up",
		Body:             "details",
		RelationshipType: "action-item",
		Priority:         "critical",
	})
	if err != nil {
		t.Fatalf("CreateGitHubIssue: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "task.linked" {
		t.Errorf("event = %+v, want sev_id=%q type=task.linked", events[0], sevID)
	}
}

func seedSensitiveSEVForTask(t *testing.T, ts *testTaskServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title: "Sensitive SEV", SeverityLevel: 2, Status: store.SEVStatusOpen,
		Sensitive: true, CreatedBy: "user-1", CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSensitiveSEVForTask: %v", err)
	}
	return sv.ID
}

func TestLinkTask_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSensitiveSEVForTask(t, ts)

	if _, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item")); err != nil {
		t.Fatalf("LinkTask: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestUnlinkTask_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSensitiveSEVForTask(t, ts)

	linked, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	if _, err := ts.server.UnlinkTask(ctx, &pb.UnlinkTaskRequest{SevId: sevID, Id: linked.GetId()}); err != nil {
		t.Fatalf("UnlinkTask: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestUpdateTaskDueDate_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestTaskServer(nil)
	ctx := context.Background()
	sevID := seedSensitiveSEVForTask(t, ts)

	linked, err := ts.server.LinkTask(ctx, linkTaskReq(sevID, "critical", "action-item"))
	if err != nil {
		t.Fatalf("LinkTask: %v", err)
	}
	_, err = ts.server.UpdateTaskDueDate(ctx, &pb.UpdateTaskDueDateRequest{
		SevId:   sevID,
		Id:      linked.GetId(),
		DueDate: timestamppb.New(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("UpdateTaskDueDate: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestCreateGitHubIssue_SensitiveSEVDoesNotPublish(t *testing.T) {
	gh := &fakeIssueClient{
		issue: &grpchandler.CreatedIssue{
			Number: 42, Title: "SEV follow-up", Body: "details",
			URL: "https://github.com/acme/api/issues/42",
		},
	}
	ts := newTestTaskServer(gh)
	ctx := context.Background()
	sevID := seedSensitiveSEVForTask(t, ts)

	_, err := ts.server.CreateGitHubIssue(ctx, &pb.CreateGitHubIssueRequest{
		SevId: sevID, Owner: "acme", Repo: "api", Title: "SEV follow-up", Body: "details",
		RelationshipType: "action-item", Priority: "critical",
	})
	if err != nil {
		t.Fatalf("CreateGitHubIssue: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}
