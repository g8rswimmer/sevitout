//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestSEVLinkStore covers Create/Delete/ListBySEVID. sev_links carries real
// FKs on both source_sev_id and target_sev_id to sevs(id), plus a UNIQUE
// (source, target, type) constraint (migrations/000002_schema.up.sql), so
// every case seeds real SEVs first.
func TestSEVLinkStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewSEVLinkStore(pool)

	source := newSEVForTest("source incident")
	target := newSEVForTest("target incident")
	if err := sevs.Create(ctx, source); err != nil {
		t.Fatalf("seed source SEV: %v", err)
	}
	if err := sevs.Create(ctx, target); err != nil {
		t.Fatalf("seed target SEV: %v", err)
	}

	link := &store.SEVLink{
		SourceSEVID:      source.ID,
		TargetSEVID:      target.ID,
		RelationshipType: store.SEVRelationshipRelated,
		CreatedBy:        "user-1",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, link); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if link.ID == 0 {
			t.Fatal("ID should be set after Create")
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		dup := &store.SEVLink{SourceSEVID: source.ID, TargetSEVID: target.ID, RelationshipType: store.SEVRelationshipRelated, CreatedBy: "user-1"}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("CreateDifferentRelationshipTypeAllowed", func(t *testing.T) {
		// The UNIQUE constraint is on (source, target, type) together, so the
		// same pair with a different relationship type is a distinct link.
		other := &store.SEVLink{SourceSEVID: source.ID, TargetSEVID: target.ID, RelationshipType: store.SEVRelationshipDuplicate, CreatedBy: "user-1"}
		if err := s.Create(ctx, other); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("ListBySEVID_Source", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, source.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2, got %d", len(items))
		}
	})

	t.Run("ListBySEVID_Target", func(t *testing.T) {
		// A link must appear when queried from either side.
		items, err := s.ListBySEVID(ctx, target.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("link should appear on both sides: want 2, got %d", len(items))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, source.ID, target.ID, store.SEVRelationshipRelated); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		items, err := s.ListBySEVID(ctx, source.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1 remaining (Duplicate link), got %d", len(items))
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := s.Delete(ctx, source.ID, target.ID, store.SEVRelationshipCausedBy); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
