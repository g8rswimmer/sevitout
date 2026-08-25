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

// TestOnCallStore covers Create/Get/Update/Delete/List/GetCurrentOnCall for
// §18.3's on-call rotations. oncall_rotations.service_id carries a real
// (nullable) FK to services(id), so cases that set ServiceID seed a real
// service first.
func TestOnCallStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	services := postgres.NewServiceStore(pool)
	s := postgres.NewOnCallStore(pool)

	svc := &store.Service{ID: "svc-api", Name: "API Service", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := services.Create(ctx, svc); err != nil {
		t.Fatalf("seed service: %v", err)
	}

	r := &store.OnCallRotation{
		Name:      "API on-call",
		ServiceID: &svc.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, r); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if r.ID == 0 {
			t.Fatal("ID not set")
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, r.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != r.Name {
			t.Fatal("name mismatch")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("GetCurrentOnCall_PlainRotationFallback", func(t *testing.T) {
		got, err := s.GetCurrentOnCall(ctx, svc.ID)
		if err != nil {
			t.Fatalf("GetCurrentOnCall: %v", err)
		}
		if got.Name != r.Name {
			t.Fatal("name mismatch")
		}
	})

	t.Run("GetCurrentOnCall_PrefersActiveManualOverride", func(t *testing.T) {
		userID, displayName := "user-oncall", "Bob (manual override)"
		now := time.Now()
		start, end := now.Add(-1*time.Hour), now.Add(1*time.Hour)
		override := &store.OnCallRotation{
			Name: "manual override", ServiceID: &svc.ID,
			ManualUserID: &userID, ManualDisplayName: &displayName,
			OverrideStart: &start, OverrideEnd: &end,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.Create(ctx, override); err != nil {
			t.Fatalf("Create override: %v", err)
		}

		got, err := s.GetCurrentOnCall(ctx, svc.ID)
		if err != nil {
			t.Fatalf("GetCurrentOnCall: %v", err)
		}
		if got.ID != override.ID {
			t.Fatalf("want the active manual override (id=%d) preferred over the plain rotation (id=%d), got id=%d", override.ID, r.ID, got.ID)
		}
	})

	t.Run("GetCurrentOnCall_ExpiredOverrideIgnored", func(t *testing.T) {
		userID, displayName := "user-expired", "Expired override"
		now := time.Now()
		start, end := now.Add(-48*time.Hour), now.Add(-24*time.Hour)
		expired := &store.OnCallRotation{
			Name: "expired override", ServiceID: &svc.ID,
			ManualUserID: &userID, ManualDisplayName: &displayName,
			OverrideStart: &start, OverrideEnd: &end,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.Create(ctx, expired); err != nil {
			t.Fatalf("Create expired: %v", err)
		}

		got, err := s.GetCurrentOnCall(ctx, svc.ID)
		if err != nil {
			t.Fatalf("GetCurrentOnCall: %v", err)
		}
		if got.ID == expired.ID {
			t.Fatal("an expired override must not be returned as current")
		}
	})

	t.Run("GetCurrentOnCallNotFound", func(t *testing.T) {
		if _, err := s.GetCurrentOnCall(ctx, "svc-missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("want 3 (plain rotation + active override + expired override), got %d", len(items))
		}
	})

	t.Run("Update", func(t *testing.T) {
		r.Name = "API on-call (updated)"
		r.UpdatedAt = time.Now()
		if err := s.Update(ctx, r); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := s.Get(ctx, r.ID)
		if err != nil {
			t.Fatalf("Get after Update: %v", err)
		}
		if got.Name != "API on-call (updated)" {
			t.Fatal("name not updated")
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := &store.OnCallRotation{ID: 999999, Name: "ghost", UpdatedAt: time.Now()}
		if err := s.Update(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, r.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, r.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := s.Delete(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
