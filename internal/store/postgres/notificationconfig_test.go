//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestNotificationConfigStore covers Upsert/Delete/List/ListForEvent for the
// admin-configured notification routing table (docs/roadmap.md Phase 15).
// Unlike RetentionConfigStore/EscalationConfigStore, no rows are pre-seeded —
// a routing rule only exists once an admin creates one.
func TestNotificationConfigStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewNotificationConfigStore(pool)

	t.Run("List_EmptyInitially", func(t *testing.T) {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("want 0 rows before any Upsert, got %d", len(items))
		}
	})

	icRule := &store.NotificationConfig{
		Role: store.OrgRoleIncidentCommander, Event: "sev.created",
		ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents",
	}

	t.Run("Upsert_Insert", func(t *testing.T) {
		if err := s.Upsert(ctx, icRule); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if icRule.ID == 0 {
			t.Fatal("ID should be set after Upsert")
		}
	})

	t.Run("Upsert_UpdateExistingPreservesID", func(t *testing.T) {
		existingID := icRule.ID
		updated := &store.NotificationConfig{
			Role: store.OrgRoleIncidentCommander, Event: "sev.created",
			ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents-v2",
		}
		if err := s.Upsert(ctx, updated); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if updated.ID != existingID {
			t.Errorf("Upsert should preserve the existing ID, got %d want %d", updated.ID, existingID)
		}
	})

	mgmtRule := &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Event: "sev.created",
		ChannelType: store.NotificationChannelEmail, ChannelTarget: "mgmt@example.com", MaxSeverityLevel: int16Ptr(2),
	}

	t.Run("Upsert_SecondRule", func(t *testing.T) {
		if err := s.Upsert(ctx, mgmtRule); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	})

	t.Run("List_ReturnsBoth", func(t *testing.T) {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2 rows, got %d", len(items))
		}
	})

	t.Run("ListForEvent_SeverityFilter", func(t *testing.T) {
		lvl3 := int16(3)
		items, err := s.ListForEvent(ctx, "sev.created", &lvl3)
		if err != nil {
			t.Fatalf("ListForEvent: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want only the unfiltered IC rule to match severity 3, got %d", len(items))
		}

		lvl1 := int16(1)
		items, err = s.ListForEvent(ctx, "sev.created", &lvl1)
		if err != nil {
			t.Fatalf("ListForEvent: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want both rules to match severity 1, got %d", len(items))
		}
	})

	t.Run("ListForEvent_NoMatch", func(t *testing.T) {
		items, err := s.ListForEvent(ctx, "sev.updated", nil)
		if err != nil {
			t.Fatalf("ListForEvent: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("want 0 rows for an unrelated event, got %d", len(items))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, store.OrgRoleIncidentCommander, "sev.created", store.NotificationChannelSlack); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		items, _ := s.List(ctx)
		if len(items) != 1 {
			t.Fatalf("want 1 row after delete, got %d", len(items))
		}
	})

	t.Run("Delete_NotFound", func(t *testing.T) {
		err := s.Delete(ctx, store.OrgRoleIncidentCommander, "sev.created", store.NotificationChannelSlack)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound on second delete, got %v", err)
		}
	})
}

func int16Ptr(v int16) *int16 { return &v }
