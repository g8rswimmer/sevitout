//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestEscalationConfigStore covers Get/Upsert/List for the per-severity-level
// escalation threshold (docs/roadmap.md Phase 15). Like RetentionConfigStore,
// migrations/000020_notification_config.up.sql seeds severities 1-4 once, at
// migration time — but truncateAll clears those seeded rows before every
// test in this package, so Upsert is exercised as both the insert and the
// update path.
func TestEscalationConfigStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewEscalationConfigStore(pool)

	t.Run("GetNotFound_TableStartsEmptyAfterTruncate", func(t *testing.T) {
		if _, err := s.Get(ctx, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	cfg := &store.EscalationConfig{SeverityLevel: 1, ThresholdMinutes: 0, Enabled: false}

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
		if got.ThresholdMinutes != 0 || got.Enabled {
			t.Fatalf("got %+v, want disabled default", got)
		}
	})

	t.Run("Upsert_Update", func(t *testing.T) {
		cfg.ThresholdMinutes = 30
		cfg.Enabled = true
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got, err := s.Get(ctx, 1)
		if err != nil {
			t.Fatalf("Get after Upsert: %v", err)
		}
		if got.ThresholdMinutes != 30 || !got.Enabled {
			t.Fatalf("update did not persist: got %+v", got)
		}
	})

	t.Run("List", func(t *testing.T) {
		if err := s.Upsert(ctx, &store.EscalationConfig{SeverityLevel: 2, ThresholdMinutes: 60, Enabled: false}); err != nil {
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
