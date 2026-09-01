package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// CreatedIssue is the shape TaskServer needs back from creating an external
// issue. It is owned by this package (the consumer), not by any tracker
// integration, so IssueClient/JiraIssueClient implementations stay decoupled
// from integration-specific types.
//
// Number and Key are tracker-specific identifiers, populated by whichever
// adapter is in play and ignored otherwise: GitHub issues are numbered
// (Number, used to build "owner/repo#N"); Jira issues have a project-scoped
// string key instead (Key, e.g. "PROJ-7", used directly as the linked
// task's external ID).
type CreatedIssue struct {
	Number int
	Key    string
	Title  string
	Body   string
	URL    string
}

// IssueClient creates issues in an external task tracker (e.g. GitHub
// Issues). assignee is a GitHub login to assign the new issue to, or "" for
// unassigned (docs/roadmap.md Phase 10f). Implementations must be safe for
// concurrent use.
type IssueClient interface {
	CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string, assignee string) (*CreatedIssue, error)
}

// JiraIssueClient creates issues in Jira. A second, Jira-shaped interface
// rather than a generalization of IssueClient above: Jira's create-issue
// call has no owner/repo concept (a project key and issue type instead) and
// the two integrations' error-mapping needs (githubIssueError vs.
// jiraIssueError below) already diverge enough that forcing one shared
// signature would mean one side or the other passing parameters that don't
// mean what their names say. Implementations must be safe for concurrent
// use.
// assigneeAccountID is a Jira Cloud account ID to assign the new issue to,
// or "" for unassigned (docs/roadmap.md Phase 10f).
type JiraIssueClient interface {
	CreateIssue(ctx context.Context, projectKey, issueType, summary, description string, labels []string, assigneeAccountID string) (*CreatedIssue, error)
}

// httpStatusError is implemented by integration errors that carry an
// HTTP-like status code. Declaring it here (rather than importing an
// integration package's concrete error type) keeps this package decoupled
// from any specific tracker implementation.
type httpStatusError interface {
	HTTPStatus() int
}

// ErrIntegrationNotConfigured is returned by an IssueClient/JiraIssueClient
// implementation (see the *Resolver types in cmd/server) when neither a
// datastore-configured nor a static env-var-configured credential is
// currently available for that tracker. CreateGitHubIssue/CreateJiraIssue
// check for it explicitly and map it to codes.Unavailable, rather than
// running it through githubIssueError/jiraIssueError below, which assume a
// real HTTP response from the tracker was involved.
var ErrIntegrationNotConfigured = errors.New("integration not configured")

// TaskServer implements pb.TaskServiceServer.
type TaskServer struct {
	pb.UnimplementedTaskServiceServer
	tasks     store.TaskStore
	sevs      store.SEVStore
	access    store.SEVAccessStore
	audit     store.AuditStore
	users     store.UserStore // resolves assignee_user_id -> a display name for AssigneeName; nil is tolerated (name resolution is skipped)
	github    IssueClient     // nil when GITHUB_TOKEN is not set
	jira      JiraIssueClient // nil when JIRA_CLOUD_ID/JIRA_API_TOKEN are not both set
	publisher Publisher       // nil when WebSocket support is not wired up
}

// TaskServerParams groups NewTaskServer's dependencies. GitHub and Jira may
// each independently be nil (both are optional at deploy time); in that
// case CreateGitHubIssue/CreateJiraIssue returns Unavailable. Publisher and
// Users may also be nil.
type TaskServerParams struct {
	Tasks     store.TaskStore
	SEVs      store.SEVStore
	Access    store.SEVAccessStore
	Audit     store.AuditStore
	Users     store.UserStore
	GitHub    IssueClient
	Jira      JiraIssueClient
	Publisher Publisher
}

// NewTaskServer returns a TaskServer backed by p.
func NewTaskServer(p TaskServerParams) *TaskServer {
	return &TaskServer{
		tasks: p.Tasks, sevs: p.SEVs, access: p.Access, audit: p.Audit, users: p.Users,
		github: p.GitHub, jira: p.Jira, publisher: p.Publisher,
	}
}

func (s *TaskServer) LinkTask(ctx context.Context, req *pb.LinkTaskRequest) (*pb.TaskResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetExternalSystem() == "" {
		return nil, status.Error(codes.InvalidArgument, "external_system is required")
	}
	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.GetUrl() == "" {
		return nil, status.Error(codes.InvalidArgument, "url is required")
	}
	if req.GetTitle() == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if err := validateRelationshipType(req.GetRelationshipType()); err != nil {
		return nil, err
	}
	if err := validatePriority(req.GetPriority()); err != nil {
		return nil, err
	}

	sev, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	priority := store.TaskPriority(req.GetPriority())
	now := time.Now()

	var dueDate *time.Time
	if req.GetDueDate() != nil {
		// Caller provided an explicit due date — use it.
		t := req.GetDueDate().AsTime()
		dueDate = &t
	} else {
		// Compute SLA due date from resolved_at when available.
		dueDate = computeDueDate(priority, sev.ResolvedAt)
	}

	task := &store.LinkedTask{
		SEVID:            req.GetSevId(),
		ExternalSystem:   req.GetExternalSystem(),
		TaskID:           req.GetTaskId(),
		URL:              req.GetUrl(),
		Title:            req.GetTitle(),
		RelationshipType: store.TaskRelationshipType(req.GetRelationshipType()),
		Priority:         priority,
		DueDate:          dueDate,
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	if v := req.GetDescription(); v != "" {
		task.Description = &v
	}

	if err := s.tasks.Create(ctx, task); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Error(codes.AlreadyExists, "this task is already linked to the SEV")
		}
		return nil, internalError(ctx, "failed to create linked task", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.linked",
		NewValue:  strPtr(task.URL),
		CreatedAt: now,
	})

	resp := taskToProto(task, now)
	if !sev.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "task.linked", resp)
	}

	return resp, nil
}

func (s *TaskServer) UnlinkTask(ctx context.Context, req *pb.UnlinkTaskRequest) (*emptypb.Empty, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	task, err := s.tasks.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, internalError(ctx, "failed to get task", err)
	}
	if task.SEVID != req.GetSevId() {
		return nil, status.Error(codes.NotFound, "task not found")
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	if err := s.tasks.Delete(ctx, req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, internalError(ctx, "failed to delete task", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.unlinked",
		OldValue:  strPtr(task.URL),
		CreatedAt: now,
	})

	// Reuse taskToProto (the same shape LinkTask/UpdateTaskDueDate/
	// CreateGitHubIssue publish for task.linked/task.updated) rather than an
	// ad hoc payload, so task.updated always carries one consistent shape
	// regardless of which handler emitted it.
	if sevRecord, err := s.sevs.Get(ctx, req.GetSevId()); err == nil && !sevRecord.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "task.updated", taskToProto(task, now))
	}

	return &emptypb.Empty{}, nil
}

func (s *TaskServer) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	sev, err := loadVisibleSEV(ctx, s.sevs, s.access, req.GetSevId())
	if err != nil {
		return nil, err
	}

	tasks, err := s.tasks.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, internalError(ctx, "failed to list tasks", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	resp := &pb.ListTasksResponse{}
	for _, t := range tasks {
		// If the SEV is now resolved but this task still has no due date,
		// assign and persist it once. SetDueDateIfUnset only applies the
		// write (and reports true) if no other concurrent caller already
		// backfilled it, so ListTasks can't produce duplicate audit entries
		// for a single logical backfill event.
		if t.DueDate == nil && sev.ResolvedAt != nil {
			computed := computeDueDate(t.Priority, sev.ResolvedAt)
			applied, err := s.tasks.SetDueDateIfUnset(ctx, t.ID, *computed)
			if err == nil {
				if applied {
					t.DueDate = computed
					auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
						SEVID:     req.GetSevId(),
						UserID:    callerID,
						Action:    "task.due_date_backfilled",
						NewValue:  strPtr(computed.Format(time.RFC3339)),
						CreatedAt: now,
					})
				} else if fresh, gerr := s.tasks.Get(ctx, t.ID); gerr == nil {
					// Another concurrent call already backfilled it — reflect
					// the persisted value instead of the one we computed.
					t.DueDate = fresh.DueDate
				}
			}
		}
		resp.Tasks = append(resp.Tasks, taskToProto(t, now))
	}
	return resp, nil
}

func (s *TaskServer) UpdateTaskDueDate(ctx context.Context, req *pb.UpdateTaskDueDateRequest) (*pb.TaskResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.GetDueDate() == nil {
		return nil, status.Error(codes.InvalidArgument, "due_date is required")
	}

	task, err := s.tasks.Get(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, internalError(ctx, "failed to get task", err)
	}
	if task.SEVID != req.GetSevId() {
		return nil, status.Error(codes.NotFound, "task not found")
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	t := req.GetDueDate().AsTime()
	task.DueDate = &t

	if err := s.tasks.Update(ctx, task); err != nil {
		return nil, internalError(ctx, "failed to update task", err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.due_date_updated",
		NewValue:  strPtr(t.Format(time.RFC3339)),
		CreatedAt: now,
	})

	resp := taskToProto(task, now)
	if sevRecord, err := s.sevs.Get(ctx, req.GetSevId()); err == nil && !sevRecord.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "task.updated", resp)
	}

	return resp, nil
}

func (s *TaskServer) CreateGitHubIssue(ctx context.Context, req *pb.CreateGitHubIssueRequest) (*pb.TaskResponse, error) {
	if s.github == nil {
		return nil, status.Error(codes.Unavailable, "GitHub integration is not configured (set GITHUB_TOKEN, or add credentials via the Integration Config API)")
	}
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetOwner() == "" {
		return nil, status.Error(codes.InvalidArgument, "owner is required")
	}
	if req.GetRepo() == "" {
		return nil, status.Error(codes.InvalidArgument, "repo is required")
	}
	if req.GetTitle() == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if err := validateRelationshipType(req.GetRelationshipType()); err != nil {
		return nil, err
	}
	if err := validatePriority(req.GetPriority()); err != nil {
		return nil, err
	}

	sev, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	priority := store.TaskPriority(req.GetPriority())
	labels := []string{req.GetSevId(), string(priority)}

	issue, err := s.github.CreateIssue(ctx, req.GetOwner(), req.GetRepo(), req.GetTitle(), req.GetBody(), labels, req.GetAssignee())
	if err != nil && isUnprocessable(err) {
		// A 422 here is most often caused by a label that doesn't already
		// exist and the org restricting who can create new labels. Don't
		// let a cosmetic labeling failure block issue creation.
		issue, err = s.github.CreateIssue(ctx, req.GetOwner(), req.GetRepo(), req.GetTitle(), req.GetBody(), nil, req.GetAssignee())
	}
	if errors.Is(err, ErrIntegrationNotConfigured) {
		return nil, status.Error(codes.Unavailable, "GitHub integration is not configured (set GITHUB_TOKEN, or add credentials via the Integration Config API)")
	}
	if err != nil {
		return nil, githubIssueError(err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	taskID := fmt.Sprintf("%s/%s#%d", req.GetOwner(), req.GetRepo(), issue.Number)
	body := issue.Body

	task := &store.LinkedTask{
		SEVID:            req.GetSevId(),
		ExternalSystem:   "github",
		TaskID:           taskID,
		URL:              issue.URL,
		Title:            issue.Title,
		Description:      &body,
		RelationshipType: store.TaskRelationshipType(req.GetRelationshipType()),
		Priority:         priority,
		DueDate:          computeDueDate(priority, sev.ResolvedAt),
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	if v := req.GetAssignee(); v != "" {
		task.Assignee = &v
	}
	task.AssigneeName = s.resolveAssigneeName(ctx, req.GetAssigneeUserId())

	if err := s.tasks.Create(ctx, task); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Errorf(codes.AlreadyExists,
				"GitHub issue %s was created but is already linked to this SEV", issue.URL)
		}
		// The GitHub issue already exists at this point and cannot be
		// silently retried without risking a duplicate; surface its URL so
		// the caller can link it manually via LinkTask.
		return nil, status.Errorf(codes.Internal,
			"GitHub issue %s was created but could not be linked to the SEV: %v", issue.URL, err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.github_issue_created",
		NewValue:  strPtr(issue.URL),
		CreatedAt: now,
	})

	resp := taskToProto(task, now)
	if !sev.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "task.linked", resp)
	}

	return resp, nil
}

func (s *TaskServer) CreateJiraIssue(ctx context.Context, req *pb.CreateJiraIssueRequest) (*pb.TaskResponse, error) {
	if s.jira == nil {
		return nil, status.Error(codes.Unavailable,
			"Jira integration is not configured (set JIRA_CLOUD_ID/JIRA_API_TOKEN, or add credentials via the Integration Config API)")
	}
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}
	if req.GetProjectKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "project_key is required")
	}
	if req.GetIssueType() == "" {
		return nil, status.Error(codes.InvalidArgument, "issue_type is required")
	}
	if req.GetSummary() == "" {
		return nil, status.Error(codes.InvalidArgument, "summary is required")
	}
	if err := validateRelationshipType(req.GetRelationshipType()); err != nil {
		return nil, err
	}
	if err := validatePriority(req.GetPriority()); err != nil {
		return nil, err
	}

	sev, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, internalError(ctx, "failed to get SEV", err)
	}

	priority := store.TaskPriority(req.GetPriority())
	labels := []string{req.GetSevId(), string(priority)}

	// Unlike CreateGitHubIssue, this doesn't retry without labels on a
	// label-related rejection — GitHub can reject issue creation over a
	// label that doesn't already exist and the org restricting who may
	// create new ones; Jira Cloud auto-creates unrecognized labels on the
	// issue instead of rejecting the request, so that failure mode doesn't
	// apply here.
	issue, err := s.jira.CreateIssue(ctx, req.GetProjectKey(), req.GetIssueType(), req.GetSummary(), req.GetDescription(), labels, req.GetAssigneeAccountId())
	if errors.Is(err, ErrIntegrationNotConfigured) {
		return nil, status.Error(codes.Unavailable, "Jira integration is not configured (set JIRA_CLOUD_ID/JIRA_API_TOKEN, or add credentials via the Integration Config API)")
	}
	if err != nil {
		return nil, jiraIssueError(err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	now := time.Now()
	body := issue.Body

	task := &store.LinkedTask{
		SEVID:            req.GetSevId(),
		ExternalSystem:   "jira",
		TaskID:           issue.Key,
		URL:              issue.URL,
		Title:            issue.Title,
		Description:      &body,
		RelationshipType: store.TaskRelationshipType(req.GetRelationshipType()),
		Priority:         priority,
		DueDate:          computeDueDate(priority, sev.ResolvedAt),
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	if v := req.GetAssigneeAccountId(); v != "" {
		task.Assignee = &v
	}
	task.AssigneeName = s.resolveAssigneeName(ctx, req.GetAssigneeUserId())

	if err := s.tasks.Create(ctx, task); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, status.Errorf(codes.AlreadyExists,
				"Jira issue %s was created but is already linked to this SEV", issue.URL)
		}
		// The Jira issue already exists at this point and cannot be
		// silently retried without risking a duplicate; surface its URL so
		// the caller can link it manually via LinkTask.
		return nil, status.Errorf(codes.Internal,
			"Jira issue %s was created but could not be linked to the SEV: %v", issue.URL, err)
	}

	auditAppendBestEffort(ctx, s.audit, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.jira_issue_created",
		NewValue:  strPtr(issue.URL),
		CreatedAt: now,
	})

	resp := taskToProto(task, now)
	if !sev.Sensitive {
		publishProto(s.publisher, req.GetSevId(), "task.linked", resp)
	}

	return resp, nil
}

// isUnprocessable reports whether err represents an HTTP 422 response.
func isUnprocessable(err error) bool {
	var statusErr httpStatusError
	return errors.As(err, &statusErr) && statusErr.HTTPStatus() == http.StatusUnprocessableEntity
}

// githubIssueError maps an integration error's HTTP status to the most
// accurate gRPC status available, so callers can distinguish a bad
// request/permission problem from a genuine internal failure instead of
// seeing a bare "unexpected status" with no indication of the actual cause.
func githubIssueError(err error) error {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.HTTPStatus() {
		case http.StatusUnauthorized:
			return status.Errorf(codes.Unauthenticated, "GitHub rejected the request: %s", err.Error())
		case http.StatusForbidden:
			return status.Errorf(codes.PermissionDenied, "GitHub rejected the request: %s", err.Error())
		case http.StatusNotFound:
			return status.Errorf(codes.NotFound, "GitHub repository not found: %s", err.Error())
		case http.StatusUnprocessableEntity:
			return status.Errorf(codes.InvalidArgument, "GitHub rejected the request: %s", err.Error())
		case http.StatusTooManyRequests:
			return status.Errorf(codes.ResourceExhausted, "GitHub rate limit exceeded: %s", err.Error())
		}
	}
	return status.Errorf(codes.Internal, "failed to create GitHub issue: %v", err)
}

// jiraIssueError maps a Jira integration error's HTTP status to the most
// accurate gRPC status available — the same mapping strategy as
// githubIssueError above, kept as a separate function since the two
// trackers' actual status-code vocabularies aren't identical enough to
// safely share one (e.g. Jira uses 400 for a validation failure where
// GitHub's equivalent is 422).
func jiraIssueError(err error) error {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.HTTPStatus() {
		case http.StatusUnauthorized:
			return status.Errorf(codes.Unauthenticated, "Jira rejected the request: %s", err.Error())
		case http.StatusForbidden:
			return status.Errorf(codes.PermissionDenied, "Jira rejected the request: %s", err.Error())
		case http.StatusNotFound:
			// Unlike GitHub's create-issue endpoint (whose URL path embeds
			// owner/repo, so a bad one genuinely 404s at GitHub's API),
			// Jira's POST /rest/api/3/issue has a fixed path — project_key
			// and issue_type are validated in the request body, and an
			// invalid one there is a 400 from Jira, not a 404. A 404 here
			// essentially always means the request never reached Jira's
			// own handler at all: the api.atlassian.com gateway itself
			// couldn't route it (an invalid/inaccessible JIRA_CLOUD_ID, or
			// a token not provisioned for gateway access).
			return status.Errorf(codes.NotFound, "Jira API endpoint not found — check that JIRA_CLOUD_ID is correct and the token has gateway access: %s", err.Error())
		case http.StatusBadRequest:
			return status.Errorf(codes.InvalidArgument, "Jira rejected the request: %s", err.Error())
		case http.StatusTooManyRequests:
			return status.Errorf(codes.ResourceExhausted, "Jira rate limit exceeded: %s", err.Error())
		}
	}
	return status.Errorf(codes.Internal, "failed to create Jira issue: %v", err)
}

// computeDueDate returns the SLA due date based on priority and resolved_at.
// Returns nil when the SEV has not yet been resolved (due date deferred).
func computeDueDate(priority store.TaskPriority, resolvedAt *time.Time) *time.Time {
	if resolvedAt == nil {
		return nil
	}
	days := 90
	if priority == store.TaskPriorityCritical {
		days = 30
	}
	t := resolvedAt.AddDate(0, 0, days)
	return &t
}

// isOverdue reports whether a task is past its due date.
// Tasks without a due date are never overdue.
func isOverdue(dueDate *time.Time, now time.Time) bool {
	if dueDate == nil {
		return false
	}
	return dueDate.Before(now)
}

// taskToProto converts a stored task to its wire representation. Overdue is
// always derived from DueDate against now rather than trusted from storage,
// so a stale persisted flag can never leak into a response.
func taskToProto(t *store.LinkedTask, now time.Time) *pb.TaskResponse {
	resp := &pb.TaskResponse{
		Id:               t.ID,
		SevId:            t.SEVID,
		ExternalSystem:   t.ExternalSystem,
		TaskId:           t.TaskID,
		Url:              t.URL,
		Title:            t.Title,
		RelationshipType: string(t.RelationshipType),
		Priority:         string(t.Priority),
		Overdue:          isOverdue(t.DueDate, now),
		CreatedAt:        timestamppb.New(t.CreatedAt),
		CreatedBy:        t.CreatedBy,
	}
	if t.Description != nil {
		resp.Description = *t.Description
	}
	if t.DueDate != nil {
		resp.DueDate = timestamppb.New(*t.DueDate)
	}
	if t.Assignee != nil {
		resp.Assignee = *t.Assignee
	}
	if t.AssigneeName != nil {
		resp.AssigneeName = *t.AssigneeName
	}
	return resp
}

// resolveAssigneeName resolves userID (a Sevitout user ID, from
// CreateGitHubIssueRequest/CreateJiraIssueRequest's assignee_user_id) to
// that user's current display name, for the LinkedTask.AssigneeName
// snapshot. Returns nil — not an error — when userID is empty, s.users
// isn't wired up, or the lookup fails: a display-name resolution failure
// must never block issue creation, which by this point has already
// succeeded against the real tracker (GitHub/Jira).
func (s *TaskServer) resolveAssigneeName(ctx context.Context, userID string) *string {
	if userID == "" || s.users == nil {
		return nil
	}
	user, err := s.users.Get(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "resolve assignee name failed", "user_id", userID, "err", err)
		return nil
	}
	return &user.Name
}

func validateRelationshipType(rt string) error {
	switch store.TaskRelationshipType(rt) {
	case store.TaskRelationshipActionItem,
		store.TaskRelationshipContributingFactor,
		store.TaskRelationshipFollowUp,
		store.TaskRelationshipDependency:
		return nil
	default:
		return status.Error(codes.InvalidArgument,
			"relationship_type must be one of: action-item, contributing-factor, follow-up, dependency")
	}
}

func validatePriority(p string) error {
	switch store.TaskPriority(p) {
	case store.TaskPriorityCritical, store.TaskPriorityNonCritical:
		return nil
	default:
		return status.Error(codes.InvalidArgument, "priority must be one of: critical, non-critical")
	}
}
