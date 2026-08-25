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

// TestPostmortemStore covers Create/GetBySEVID/Update/CountByStatus.
// postmortems.sev_id carries both a real FK to sevs(id) and a UNIQUE
// constraint (migrations/000002_schema.up.sql — one postmortem per SEV),
// so every case seeds a real SEV first and CreateDuplicate exercises that
// UNIQUE constraint specifically (the FK alone wouldn't produce ErrConflict).
func TestPostmortemStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewPostmortemStore(pool)

	sv := newSEVForTest("postmortem test")
	if err := sevs.Create(ctx, sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}

	pm := &store.Postmortem{
		SEVID:     sv.ID,
		Status:    store.PostmortemStatusDraft,
		Content:   "## Summary\n",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, pm); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if pm.ID == 0 {
			t.Fatal("ID should be set after Create")
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		dup := &store.Postmortem{SEVID: sv.ID, Status: store.PostmortemStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("GetBySEVID", func(t *testing.T) {
		got, err := s.GetBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("GetBySEVID: %v", err)
		}
		if got.Status != store.PostmortemStatusDraft {
			t.Fatalf("status = %q, want draft", got.Status)
		}
		if got.Content != pm.Content {
			t.Fatalf("content = %q, want %q", got.Content, pm.Content)
		}
	})

	t.Run("GetBySEVIDNotFound", func(t *testing.T) {
		if _, err := s.GetBySEVID(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		pm.Status = store.PostmortemStatusInReview
		pm.Content = "## Summary\nUpdated.\n"
		if err := s.Update(ctx, pm); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := s.GetBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("GetBySEVID after Update: %v", err)
		}
		if got.Status != store.PostmortemStatusInReview {
			t.Fatal("status not updated")
		}
		if got.Content != pm.Content {
			t.Fatal("content not updated")
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := &store.Postmortem{SEVID: "missing", Status: store.PostmortemStatusDraft, UpdatedAt: time.Now()}
		if err := s.Update(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("CountByStatus", func(t *testing.T) {
		sv2 := newSEVForTest("second postmortem test")
		if err := sevs.Create(ctx, sv2); err != nil {
			t.Fatalf("seed second SEV: %v", err)
		}
		if err := s.Create(ctx, &store.Postmortem{
			SEVID: sv2.ID, Status: store.PostmortemStatusApproved, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		counts, err := s.CountByStatus(ctx)
		if err != nil {
			t.Fatalf("CountByStatus: %v", err)
		}
		// pm was moved to InReview above; the freshly-created one is Approved.
		if counts[store.PostmortemStatusInReview] != 1 {
			t.Errorf("InReview count = %d, want 1", counts[store.PostmortemStatusInReview])
		}
		if counts[store.PostmortemStatusApproved] != 1 {
			t.Errorf("Approved count = %d, want 1", counts[store.PostmortemStatusApproved])
		}
	})
}
