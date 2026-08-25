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

// TestShareStore covers Create/GetByToken/Revoke/ListBySEVID for §14.1's
// public shareable links. shareable_links.sev_id carries a real FK to
// sevs(id) and token carries a real UNIQUE constraint
// (migrations/000002_schema.up.sql), so every case seeds a real SEV first.
func TestShareStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewShareStore(pool)

	sv := newSEVForTest("shareable link test")
	if err := sevs.Create(ctx, sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}

	link := &store.ShareableLink{
		SEVID:     sv.ID,
		Token:     "tok-abc123",
		CreatedBy: "user-1",
		CreatedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, link); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if link.ID == 0 {
			t.Fatal("ID should be set after Create")
		}
	})

	t.Run("CreateDuplicateToken", func(t *testing.T) {
		dup := &store.ShareableLink{SEVID: sv.ID, Token: "tok-abc123", CreatedBy: "user-1", CreatedAt: time.Now()}
		if err := s.Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("GetByToken", func(t *testing.T) {
		got, err := s.GetByToken(ctx, "tok-abc123")
		if err != nil {
			t.Fatalf("GetByToken: %v", err)
		}
		if got.SEVID != sv.ID {
			t.Fatalf("sev_id = %q, want %q", got.SEVID, sv.ID)
		}
		if got.Revoked {
			t.Fatal("new link should not be revoked")
		}
	})

	t.Run("GetByTokenNotFound", func(t *testing.T) {
		if _, err := s.GetByToken(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("Revoke", func(t *testing.T) {
		if err := s.Revoke(ctx, "tok-abc123", "user-1"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		got, err := s.GetByToken(ctx, "tok-abc123")
		if err != nil {
			t.Fatalf("GetByToken after Revoke: %v", err)
		}
		if !got.Revoked {
			t.Fatal("expected revoked=true")
		}
		if got.RevokedBy == nil || *got.RevokedBy != "user-1" {
			t.Fatal("revoked_by not set")
		}
		if got.RevokedAt == nil {
			t.Fatal("revoked_at not set")
		}
	})

	t.Run("RevokeAlreadyRevokedIsNoop", func(t *testing.T) {
		// Matches the in-memory store: revoking an already-revoked link is a
		// no-op, not an error.
		if err := s.Revoke(ctx, "tok-abc123", "user-2"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		got, err := s.GetByToken(ctx, "tok-abc123")
		if err != nil {
			t.Fatalf("GetByToken: %v", err)
		}
		if got.RevokedBy == nil || *got.RevokedBy != "user-1" {
			t.Fatalf("revoked_by should stay %q (first revoke), got %v", "user-1", got.RevokedBy)
		}
	})

	t.Run("RevokeNotFound", func(t *testing.T) {
		if err := s.Revoke(ctx, "missing", "user-1"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
