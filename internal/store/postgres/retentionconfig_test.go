//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestRetentionConfigStore covers Get/Upsert/List for §18.7's per-severity
// retention policy. Unlike memory.RetentionConfigStore, this store does not
// pre-seed rows itself — migrations/000002_schema.up.sql seeds severities
// 1-4 once, at schema-creation time — and truncateAll clears those seeded
// rows before every test in this package, so Upsert is exercised as both
// the insert and the update path.
func TestRetentionConfigStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewRetentionConfigStore(pool)

	t.Run("GetNotFound_TableStartsEmptyAfterTruncate", func(t *testing.T) {
		if _, err := s.Get(ctx, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	cfg := &store.RetentionConfig{SeverityLevel: 1, RetentionDays: 0, HardDelete: false}

	t.Run("Upsert_Insert", func(t *testing.T) {
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if cfg.ID == 0 {
			t.Fatal("ID should be set after Upsert")
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, 1)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.RetentionDays != 0 || got.HardDelete {
			t.Fatalf("got %+v, want retain-forever default", got)
		}
	})

	t.Run("Upsert_Update", func(t *testing.T) {
		cfg.RetentionDays = 90
		cfg.HardDelete = true
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got, err := s.Get(ctx, 1)
		if err != nil {
			t.Fatalf("Get after Upsert: %v", err)
		}
		if got.RetentionDays != 90 || !got.HardDelete {
			t.Fatalf("update did not persist: got %+v", got)
		}
	})

	t.Run("List", func(t *testing.T) {
		if err := s.Upsert(ctx, &store.RetentionConfig{SeverityLevel: 2, RetentionDays: 30}); err != nil {
			t.Fatalf("Upsert severity 2: %v", err)
		}
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2, got %d", len(items))
		}
	})
}
