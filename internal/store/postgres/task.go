package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.TaskStore = (*TaskStore)(nil)

// TaskStore implements store.TaskStore against PostgreSQL.
type TaskStore struct {
	pool *pgxpool.Pool
}

// NewTaskStore returns a TaskStore backed by pool.
func NewTaskStore(pool *pgxpool.Pool) *TaskStore {
	return &TaskStore{pool: pool}
}

func (s *TaskStore) Create(ctx context.Context, task *store.LinkedTask) error {
	q := queries.New(s.pool)

	id, err := q.InsertLinkedTask(ctx, queries.InsertLinkedTaskParams{
		SevID:            task.SEVID,
		ExternalSystem:   task.ExternalSystem,
		TaskID:           task.TaskID,
		Url:              task.URL,
		Title:            task.Title,
		Description:      task.Description,
		RelationshipType: string(task.RelationshipType),
		Priority:         string(task.Priority),
		DueDate:          dateToDB(task.DueDate),
		Overdue:          task.Overdue,
		CreatedAt:        pgtype.Timestamptz{Time: task.CreatedAt.UTC(), Valid: true},
		CreatedBy:        task.CreatedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres task: insert: %w", err)
	}
	task.ID = id
	return nil
}

func (s *TaskStore) Get(ctx context.Context, id int64) (*store.LinkedTask, error) {
	q := queries.New(s.pool)

	row, err := q.GetLinkedTask(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres task: get: %w", err)
	}
	return mapLinkedTaskRow(row), nil
}

func (s *TaskStore) Update(ctx context.Context, task *store.LinkedTask) error {
	q := queries.New(s.pool)

	// Pre-check so callers get a clean ErrNotFound rather than a silent no-op.
	if _, err := q.GetLinkedTask(ctx, task.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres task: pre-update get: %w", err)
	}

	if err := q.UpdateLinkedTask(ctx, queries.UpdateLinkedTaskParams{
		ID:               task.ID,
		Title:            task.Title,
		Description:      task.Description,
		RelationshipType: string(task.RelationshipType),
		Priority:         string(task.Priority),
		DueDate:          dateToDB(task.DueDate),
		Overdue:          task.Overdue,
	}); err != nil {
		return fmt.Errorf("postgres task: update: %w", err)
	}
	return nil
}

// SetDueDateIfUnset atomically sets a task's due date only if it doesn't
// already have one, reporting whether the write was applied.
func (s *TaskStore) SetDueDateIfUnset(ctx context.Context, id int64, dueDate time.Time) (bool, error) {
	q := queries.New(s.pool)

	n, err := q.SetTaskDueDateIfUnset(ctx, queries.SetTaskDueDateIfUnsetParams{
		ID:      id,
		DueDate: dateToDB(&dueDate),
	})
	if err != nil {
		return false, fmt.Errorf("postgres task: set due date if unset: %w", err)
	}
	if n == 0 {
		// Either the task doesn't exist, or it already has a due date.
		// Distinguish the two so callers get a clean ErrNotFound.
		if _, err := q.GetLinkedTask(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, store.ErrNotFound
			}
			return false, fmt.Errorf("postgres task: set due date if unset: pre-check get: %w", err)
		}
		return false, nil
	}
	return true, nil
}

func (s *TaskStore) Delete(ctx context.Context, id int64) error {
	q := queries.New(s.pool)

	if _, err := q.GetLinkedTask(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres task: pre-delete get: %w", err)
	}
	if err := q.DeleteLinkedTask(ctx, id); err != nil {
		return fmt.Errorf("postgres task: delete: %w", err)
	}
	return nil
}

func (s *TaskStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.LinkedTask, error) {
	q := queries.New(s.pool)

	rows, err := q.ListLinkedTasksBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres task: list: %w", err)
	}

	out := make([]*store.LinkedTask, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapLinkedTaskRow(r))
	}
	return out, nil
}

func (s *TaskStore) CountOverdue(ctx context.Context, now time.Time) (int, error) {
	q := queries.New(s.pool)

	n, err := q.CountOverdueTasks(ctx, dateToDBValue(now))
	if err != nil {
		return 0, fmt.Errorf("postgres task: count overdue: %w", err)
	}
	return int(n), nil
}

func mapLinkedTaskRow(r queries.SevLinkedTask) *store.LinkedTask {
	return &store.LinkedTask{
		ID:               r.ID,
		SEVID:            r.SevID,
		ExternalSystem:   r.ExternalSystem,
		TaskID:           r.TaskID,
		URL:              r.Url,
		Title:            r.Title,
		Description:      r.Description,
		RelationshipType: store.TaskRelationshipType(r.RelationshipType),
		Priority:         store.TaskPriority(r.Priority),
		DueDate:          dateFromDB(r.DueDate),
		Overdue:          r.Overdue,
		CreatedAt:        r.CreatedAt.Time,
		CreatedBy:        r.CreatedBy,
	}
}

// dateToDB converts a *time.Time to a pgtype.Date, truncating any
// time-of-day component (the sev_linked_tasks.due_date column is DATE, not
// TIMESTAMPTZ).
func dateToDB(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{Valid: false}
	}
	return dateToDBValue(*t)
}

func dateToDBValue(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t.UTC(), Valid: true}
}

func dateFromDB(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}
