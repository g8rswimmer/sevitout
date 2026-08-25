//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

func newLinkedTaskForTest(sevID, externalSystem, taskID string) *store.LinkedTask {
	return &store.LinkedTask{
		SEVID:            sevID,
		ExternalSystem:   externalSystem,
		TaskID:           taskID,
		URL:              "https://example.com/" + taskID,
		Title:            "task",
		RelationshipType: store.TaskRelationshipActionItem,
		Priority:         store.TaskPriorityCritical,
		CreatedAt:        time.Now(),
		CreatedBy:        "user-1",
	}
}

// TestTaskStore covers Create/Get/Update/Delete/ListBySEVID/CountOverdue.
// sev_linked_tasks.sev_id carries a real FK to sevs(id), and
// (sev_id, external_system, task_id) carries a real UNIQUE constraint
// (migrations/000008_task_unique_constraint.up.sql — added specifically
// because the postgres store silently allowed duplicates the memory fake
// always rejected), so this mirrors the equivalent memory-store cases
// (internal/store/memory/task_test.go) against the real constraint.
func TestTaskStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewTaskStore(pool)

	sevOne := newSEVForTest("task test one")
	sevTwo := newSEVForTest("task test two")
	if err := sevs.Create(ctx, sevOne); err != nil {
		t.Fatalf("seed sevOne: %v", err)
	}
	if err := sevs.Create(ctx, sevTwo); err != nil {
		t.Fatalf("seed sevTwo: %v", err)
	}

	t.Run("Create_DuplicateKeyReturnsConflict", func(t *testing.T) {
		if err := s.Create(ctx, newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#1")); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		err := s.Create(ctx, newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#1"))
		if !errors.Is(err, store.ErrConflict) {
			t.Errorf("want ErrConflict for duplicate (sev_id, external_system, task_id), got %v", err)
		}
	})

	t.Run("Create_SameTaskIDDifferentSEVAllowed", func(t *testing.T) {
		if err := s.Create(ctx, newLinkedTaskForTest(sevTwo.ID, "github", "owner/repo#1")); err != nil {
			t.Errorf("Create for a different SEV with the same external task_id should succeed, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		task := newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#2")
		if err := s.Create(ctx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := s.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.URL != task.URL {
			t.Fatalf("URL = %q, want %q", got.URL, task.URL)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		task := newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#3")
		if err := s.Create(ctx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		task.Title = "updated title"
		task.Priority = store.TaskPriorityNonCritical
		if err := s.Update(ctx, task); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := s.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("Get after Update: %v", err)
		}
		if got.Title != "updated title" || got.Priority != store.TaskPriorityNonCritical {
			t.Fatalf("update did not persist: got %+v", got)
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#ghost")
		ghost.ID = 999999
		if err := s.Update(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("SetDueDateIfUnset", func(t *testing.T) {
		task := newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#4")
		if err := s.Create(ctx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}

		due := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		applied, err := s.SetDueDateIfUnset(ctx, task.ID, due)
		if err != nil {
			t.Fatalf("SetDueDateIfUnset: %v", err)
		}
		if !applied {
			t.Fatal("want applied=true on first call")
		}

		got, err := s.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.DueDate == nil || !got.DueDate.Equal(due) {
			t.Errorf("due date not persisted: got %v, want %v", got.DueDate, due)
		}

		// A second, concurrent-style call must be a no-op, not overwrite with
		// a different value, and must not report applied.
		otherDue := due.AddDate(0, 0, 30)
		applied, err = s.SetDueDateIfUnset(ctx, task.ID, otherDue)
		if err != nil {
			t.Fatalf("SetDueDateIfUnset (second call): %v", err)
		}
		if applied {
			t.Error("want applied=false once a due date is already set")
		}

		got, err = s.Get(ctx, task.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !got.DueDate.Equal(due) {
			t.Errorf("due date should remain the first-applied value: got %v, want %v", got.DueDate, due)
		}
	})

	t.Run("SetDueDateIfUnset_NotFound", func(t *testing.T) {
		if _, err := s.SetDueDateIfUnset(ctx, 999999, time.Now()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Delete_AllowsRecreateWithSameKey", func(t *testing.T) {
		task := newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#5")
		if err := s.Create(ctx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := s.Delete(ctx, task.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if err := s.Create(ctx, newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#5")); err != nil {
			t.Errorf("Create after Delete should succeed (key freed), got %v", err)
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := s.Delete(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, sevOne.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("want at least one task for sevOne")
		}
		for _, item := range items {
			if item.SEVID != sevOne.ID {
				t.Fatalf("got task for wrong SEV: %s", item.SEVID)
			}
		}
	})

	t.Run("CountOverdue", func(t *testing.T) {
		past := time.Now().AddDate(0, 0, -1)
		overdueTask := newLinkedTaskForTest(sevOne.ID, "github", "owner/repo#overdue")
		overdueTask.DueDate = &past
		if err := s.Create(ctx, overdueTask); err != nil {
			t.Fatalf("Create: %v", err)
		}
		n, err := s.CountOverdue(ctx, time.Now())
		if err != nil {
			t.Fatalf("CountOverdue: %v", err)
		}
		if n < 1 {
			t.Fatalf("want at least 1 overdue task, got %d", n)
		}
	})
}
