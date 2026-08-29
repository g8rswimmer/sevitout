package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func newLinkedTask(sevID, externalSystem, taskID string) *store.LinkedTask {
	return &store.LinkedTask{
		SEVID:            sevID,
		ExternalSystem:   externalSystem,
		TaskID:           taskID,
		URL:              "https://example.com/" + taskID,
		Title:            "task",
		RelationshipType: store.TaskRelationshipActionItem,
		Priority:         store.TaskPriorityCritical,
		CreatedAt:        time.Now(),
	}
}

func TestTaskStore_Create_DuplicateKeyReturnsConflict(t *testing.T) {
	s := memory.NewTaskStore()
	ctx := context.Background()

	if err := s.Create(ctx, newLinkedTask("SEV-1", "github", "owner/repo#1")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := s.Create(ctx, newLinkedTask("SEV-1", "github", "owner/repo#1"))
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("want ErrConflict for duplicate (sev_id, external_system, task_id), got %v", err)
	}
}

func TestTaskStore_Create_SameTaskIDDifferentSEVAllowed(t *testing.T) {
	s := memory.NewTaskStore()
	ctx := context.Background()

	if err := s.Create(ctx, newLinkedTask("SEV-1", "github", "owner/repo#1")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := s.Create(ctx, newLinkedTask("SEV-2", "github", "owner/repo#1")); err != nil {
		t.Errorf("Create for a different SEV with the same external task_id should succeed, got %v", err)
	}
}

func TestTaskStore_Delete_AllowsRecreateWithSameKey(t *testing.T) {
	s := memory.NewTaskStore()
	ctx := context.Background()

	task := newLinkedTask("SEV-1", "github", "owner/repo#1")
	if err := s.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx, task.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Create(ctx, newLinkedTask("SEV-1", "github", "owner/repo#1")); err != nil {
		t.Errorf("Create after Delete should succeed (key freed), got %v", err)
	}
}

func TestTaskStore_SetDueDateIfUnset(t *testing.T) {
	s := memory.NewTaskStore()
	ctx := context.Background()

	task := newLinkedTask("SEV-1", "github", "owner/repo#1")
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

	// A second concurrent-style call must be a no-op, not overwrite with a
	// different value, and must not report applied.
	otherDue := due.AddDate(0, 0, 30)
	applied, err = s.SetDueDateIfUnset(ctx, task.ID, otherDue)
	if err != nil {
		t.Fatalf("SetDueDateIfUnset (second call): %v", err)
	}
	if applied {
		t.Error("want applied=false once a due date is already set")
	}

	got, _ = s.Get(ctx, task.ID)
	if !got.DueDate.Equal(due) {
		t.Errorf("due date should remain the first-applied value: got %v, want %v", got.DueDate, due)
	}
}

func TestTaskStore_SetDueDateIfUnset_NotFound(t *testing.T) {
	s := memory.NewTaskStore()
	_, err := s.SetDueDateIfUnset(context.Background(), 999, time.Now())
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestTaskStore_Update_NotFound(t *testing.T) {
	s := memory.NewTaskStore()
	ghost := newLinkedTask("SEV-1", "github", "owner/repo#1")
	ghost.ID = 999
	if err := s.Update(context.Background(), ghost); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestTaskStore_Delete_NotFound(t *testing.T) {
	s := memory.NewTaskStore()
	if err := s.Delete(context.Background(), 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
