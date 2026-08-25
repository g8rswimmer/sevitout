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

// TestSEVAccessStore covers the store behind §14's sensitive-SEV visibility
// check (sensitiveSEVVisible / loadVisibleSEV in internal/api/grpc) — the
// highest-risk file in this package, since a bug here would silently
// undermine that check regardless of how correctly the gRPC layer calls it.
// Unlike memory.SEVAccessStore, sev_access.sev_id carries a real FK to
// sevs(id) (migrations/000010_sev_access.up.sql), so every case here seeds
// real SEV rows first.
func TestSEVAccessStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewSEVAccessStore(pool)

	sevOne := newSEVForTest("sensitive incident one")
	sevOne.Sensitive = true
	if err := sevs.Create(ctx, sevOne); err != nil {
		t.Fatalf("seed sevOne: %v", err)
	}
	sevTwo := newSEVForTest("sensitive incident two")
	sevTwo.Sensitive = true
	if err := sevs.Create(ctx, sevTwo); err != nil {
		t.Fatalf("seed sevTwo: %v", err)
	}

	grant := &store.SEVAccess{
		SEVID:     sevOne.ID,
		UserID:    "user-alice",
		CreatedBy: "user-admin",
		CreatedAt: time.Now(),
	}

	t.Run("Grant", func(t *testing.T) {
		if err := s.Grant(ctx, grant); err != nil {
			t.Fatalf("Grant: %v", err)
		}
		if grant.ID == 0 {
			t.Fatal("ID should be set after Grant")
		}
	})

	grantID := grant.ID

	t.Run("GrantDuplicate", func(t *testing.T) {
		dup := &store.SEVAccess{SEVID: sevOne.ID, UserID: "user-alice", CreatedBy: "user-admin", CreatedAt: time.Now()}
		if err := s.Grant(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("GrantUnknownSEV", func(t *testing.T) {
		// sev_id's FK to sevs(id) should surface as a plain error (not a
		// panic, not misreported as ErrConflict) when the referenced SEV
		// doesn't exist.
		bad := &store.SEVAccess{SEVID: "missing", UserID: "user-x", CreatedBy: "user-admin", CreatedAt: time.Now()}
		err := s.Grant(ctx, bad)
		if err == nil {
			t.Fatal("want an error for a nonexistent sev_id, got nil")
		}
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want a plain FK-violation error, got sentinel %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, sevOne.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
		if items[0].UserID != "user-alice" {
			t.Errorf("user_id = %q, want user-alice", items[0].UserID)
		}
	})

	t.Run("ListBySEVID_OtherSEV", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, sevTwo.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 0 {
			t.Fatal("expected empty for a SEV with no grants")
		}
	})

	t.Run("HasAccess_True", func(t *testing.T) {
		ok, err := s.HasAccess(ctx, sevOne.ID, "user-alice")
		if err != nil {
			t.Fatalf("HasAccess: %v", err)
		}
		if !ok {
			t.Fatal("expected true")
		}
	})

	t.Run("HasAccess_False_WrongUser", func(t *testing.T) {
		ok, err := s.HasAccess(ctx, sevOne.ID, "user-bob")
		if err != nil {
			t.Fatalf("HasAccess: %v", err)
		}
		if ok {
			t.Fatal("expected false")
		}
	})

	t.Run("HasAccess_False_WrongSEV", func(t *testing.T) {
		// A grant on one Sensitive SEV must never be read as access to
		// another — this is the exact property loadVisibleSEV depends on.
		ok, err := s.HasAccess(ctx, sevTwo.ID, "user-alice")
		if err != nil {
			t.Fatalf("HasAccess: %v", err)
		}
		if ok {
			t.Fatal("expected false: grant was scoped to sevOne, not sevTwo")
		}
	})

	t.Run("ListSEVIDsByUser", func(t *testing.T) {
		if err := s.Grant(ctx, &store.SEVAccess{SEVID: sevTwo.ID, UserID: "user-alice", CreatedBy: "user-admin", CreatedAt: time.Now()}); err != nil {
			t.Fatalf("Grant second: %v", err)
		}
		gotIDs, err := s.ListSEVIDsByUser(ctx, "user-alice")
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(gotIDs) != 2 {
			t.Fatalf("want 2, got %d", len(gotIDs))
		}
	})

	t.Run("ListSEVIDsByUser_EmptyUser", func(t *testing.T) {
		gotIDs, err := s.ListSEVIDsByUser(ctx, "")
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if gotIDs != nil {
			t.Fatalf("want nil, got %v", gotIDs)
		}
	})

	t.Run("Revoke", func(t *testing.T) {
		if err := s.Revoke(ctx, sevOne.ID, grantID); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		ok, err := s.HasAccess(ctx, sevOne.ID, "user-alice")
		if err != nil {
			t.Fatalf("HasAccess: %v", err)
		}
		if ok {
			t.Fatal("expected access revoked")
		}
	})

	t.Run("RevokeNotFound", func(t *testing.T) {
		if err := s.Revoke(ctx, sevOne.ID, 999999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
