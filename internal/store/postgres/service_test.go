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

// TestServiceStore covers Create/Get/Update/Delete/List for §18.1's service
// registry. services.id is the primary key and services.name carries a real
// UNIQUE constraint (migrations/000002_schema.up.sql); Create's own comment
// says both collisions map to the same ErrConflict, so CreateDuplicateName
// exercises that specifically.
func TestServiceStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewServiceStore(pool)

	svc := &store.Service{
		ID:        "svc-api",
		Name:      "API Service",
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, svc); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("CreateDuplicateID", func(t *testing.T) {
		dup := &store.Service{ID: "svc-api", Name: "Different Name", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("CreateDuplicateName", func(t *testing.T) {
		dup := &store.Service{ID: "svc-different-id", Name: "API Service", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict on name dup, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, svc.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != svc.Name {
			t.Fatal("name mismatch")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		svc.Active = false
		svc.UpdatedAt = time.Now()
		if err := s.Update(ctx, svc); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := s.Get(ctx, svc.ID)
		if err != nil {
			t.Fatalf("Get after Update: %v", err)
		}
		if got.Active {
			t.Fatal("active should be false")
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := &store.Service{ID: "missing", Name: "ghost", UpdatedAt: time.Now()}
		if err := s.Update(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("ListActiveOnly", func(t *testing.T) {
		items, err := s.List(ctx, true)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 0 {
			t.Fatal("inactive service should be excluded")
		}
	})

	t.Run("ListAll", func(t *testing.T) {
		items, err := s.List(ctx, false)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, svc.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, svc.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := s.Delete(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
