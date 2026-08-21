package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// IssueClient can get and create GitHub Issues.
// Implementations must be safe for concurrent use.
type IssueClient interface {
	GetIssue(ctx context.Context, owner, repo string, number int) (*GitHubIssue, error)
	CreateIssue(ctx context.Context, owner, repo, title, body string) (*GitHubIssue, error)
}

// GitHubIssue carries the fields we use from a GitHub Issue.
type GitHubIssue struct {
	Number  int
	Title   string
	Body    string
	State   string
	HTMLURL string
}

// TaskServer implements pb.TaskServiceServer.
type TaskServer struct {
	pb.UnimplementedTaskServiceServer
	tasks  store.TaskStore
	sevs   store.SEVStore
	audit  store.AuditStore
	github IssueClient // nil when GITHUB_TOKEN is not set
}

// NewTaskServer returns a TaskServer. github may be nil; in that case
// CreateGitHubIssue returns Unavailable.
func NewTaskServer(tasks store.TaskStore, sevs store.SEVStore, audit store.AuditStore, github IssueClient) *TaskServer {
	return &TaskServer{tasks: tasks, sevs: sevs, audit: audit, github: github}
}

func (s *TaskServer) LinkTask(ctx context.Context, req *pb.LinkTaskRequest) (*pb.TaskResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
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
		return nil, status.Error(codes.Internal, "failed to get SEV")
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
		Overdue:          isOverdue(dueDate, now),
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	if v := req.GetDescription(); v != "" {
		task.Description = &v
	}

	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, status.Error(codes.Internal, "failed to create linked task")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.linked",
		NewValue:  strPtr(task.URL),
		CreatedAt: now,
	})

	return taskToProto(task), nil
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
		return nil, status.Error(codes.Internal, "failed to get task")
	}
	if task.SEVID != req.GetSevId() {
		return nil, status.Error(codes.NotFound, "task not found")
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	if err := s.tasks.Delete(ctx, req.GetId()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, status.Error(codes.Internal, "failed to delete task")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.unlinked",
		OldValue:  strPtr(task.URL),
		CreatedAt: time.Now(),
	})

	return &emptypb.Empty{}, nil
}

func (s *TaskServer) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	if req.GetSevId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sev_id is required")
	}

	sev, err := s.sevs.Get(ctx, req.GetSevId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "SEV not found")
		}
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	tasks, err := s.tasks.ListBySEVID(ctx, req.GetSevId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list tasks")
	}

	now := time.Now()
	resp := &pb.ListTasksResponse{}
	for _, t := range tasks {
		// If the SEV is now resolved but this task still has no due date,
		// compute and persist it.
		if t.DueDate == nil && sev.ResolvedAt != nil {
			computed := computeDueDate(t.Priority, sev.ResolvedAt)
			t.DueDate = computed
			t.Overdue = isOverdue(computed, now)
			_ = s.tasks.Update(ctx, t)
		} else {
			// Refresh the overdue flag on every list to reflect the passage of time.
			overdue := isOverdue(t.DueDate, now)
			if overdue != t.Overdue {
				t.Overdue = overdue
				_ = s.tasks.Update(ctx, t)
			}
		}
		resp.Tasks = append(resp.Tasks, taskToProto(t))
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
		return nil, status.Error(codes.Internal, "failed to get task")
	}
	if task.SEVID != req.GetSevId() {
		return nil, status.Error(codes.NotFound, "task not found")
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	t := req.GetDueDate().AsTime()
	task.DueDate = &t
	task.Overdue = isOverdue(&t, time.Now())

	if err := s.tasks.Update(ctx, task); err != nil {
		return nil, status.Error(codes.Internal, "failed to update task")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.due_date_updated",
		NewValue:  strPtr(t.Format(time.RFC3339)),
		CreatedAt: time.Now(),
	})

	return taskToProto(task), nil
}

func (s *TaskServer) CreateGitHubIssue(ctx context.Context, req *pb.CreateGitHubIssueRequest) (*pb.TaskResponse, error) {
	if s.github == nil {
		return nil, status.Error(codes.Unavailable, "GitHub integration is not configured (GITHUB_TOKEN not set)")
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
		return nil, status.Error(codes.Internal, "failed to get SEV")
	}

	issue, err := s.github.CreateIssue(ctx, req.GetOwner(), req.GetRepo(), req.GetTitle(), req.GetBody())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create GitHub issue: %v", err)
	}

	callerID := ""
	if uc, ok := auth.UserFromContext(ctx); ok {
		callerID = uc.UserID
	}

	priority := store.TaskPriority(req.GetPriority())
	now := time.Now()
	taskID := fmt.Sprintf("%s/%s#%d", req.GetOwner(), req.GetRepo(), issue.Number)
	body := issue.Body

	task := &store.LinkedTask{
		SEVID:            req.GetSevId(),
		ExternalSystem:   "github",
		TaskID:           taskID,
		URL:              issue.HTMLURL,
		Title:            issue.Title,
		Description:      &body,
		RelationshipType: store.TaskRelationshipType(req.GetRelationshipType()),
		Priority:         priority,
		DueDate:          computeDueDate(priority, sev.ResolvedAt),
		CreatedAt:        now,
		CreatedBy:        callerID,
	}
	task.Overdue = isOverdue(task.DueDate, now)

	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, status.Error(codes.Internal, "failed to link created GitHub issue")
	}

	_ = s.audit.Append(ctx, &store.AuditEntry{
		SEVID:     req.GetSevId(),
		UserID:    callerID,
		Action:    "task.github_issue_created",
		NewValue:  strPtr(issue.HTMLURL),
		CreatedAt: now,
	})

	return taskToProto(task), nil
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

func taskToProto(t *store.LinkedTask) *pb.TaskResponse {
	resp := &pb.TaskResponse{
		Id:               t.ID,
		SevId:            t.SEVID,
		ExternalSystem:   t.ExternalSystem,
		TaskId:           t.TaskID,
		Url:              t.URL,
		Title:            t.Title,
		RelationshipType: string(t.RelationshipType),
		Priority:         string(t.Priority),
		Overdue:          t.Overdue,
		CreatedAt:        timestamppb.New(t.CreatedAt),
		CreatedBy:        t.CreatedBy,
	}
	if t.Description != nil {
		resp.Description = *t.Description
	}
	if t.DueDate != nil {
		resp.DueDate = timestamppb.New(*t.DueDate)
	}
	return resp
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
