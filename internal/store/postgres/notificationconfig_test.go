//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestNotificationConfigStore covers Create/Update/Delete/List/ListForEvent
// for the admin-configured notification routing table (docs/roadmap.md
// Phase 15). Unlike RetentionConfigStore/EscalationConfigStore, no rows are
// pre-seeded — a routing rule only exists once an admin creates one. Rules
// are ID-identified (see store.NotificationConfig's doc comment for why a
// rule that can cover several events can no longer be keyed by its field
// values).
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
			t.Fatalf("want 0 rows before any Create, got %d", len(items))
		}
	})

	icRule := &store.NotificationConfig{
		Role: store.OrgRoleIncidentCommander, Events: []string{"sev.created"},
		ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, icRule); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if icRule.ID == 0 {
			t.Fatal("ID should be set after Create")
		}
	})

	t.Run("Update_PreservesID_CoversSecondEvent", func(t *testing.T) {
		existingID := icRule.ID
		updated := &store.NotificationConfig{
			ID: existingID, Role: store.OrgRoleIncidentCommander,
			Events:      []string{"sev.created", "sev.sla_breached"},
			ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents-v2",
		}
		if err := s.Update(ctx, updated); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.ID != existingID {
			t.Errorf("Update should preserve the existing ID, got %d want %d", updated.ID, existingID)
		}
	})

	t.Run("Update_NotFound", func(t *testing.T) {
		err := s.Update(ctx, &store.NotificationConfig{
			ID: 999999, Role: store.OrgRoleAdmin, Events: []string{"sev.created"},
			ChannelType: store.NotificationChannelSlack, ChannelTarget: "#x",
		})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound updating a nonexistent id, got %v", err)
		}
	})

	mgmtRule := &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Events: []string{"sev.created"},
		ChannelType: store.NotificationChannelEmail, ChannelTarget: "mgmt@example.com", MaxSeverityLevel: int16Ptr(2),
	}

	t.Run("Create_SecondRule", func(t *testing.T) {
		if err := s.Create(ctx, mgmtRule); err != nil {
			t.Fatalf("Create: %v", err)
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

	t.Run("ListForEvent_MatchesSecondEventInMultiEventRule", func(t *testing.T) {
		items, err := s.ListForEvent(ctx, "sev.sla_breached", nil)
		if err != nil {
			t.Fatalf("ListForEvent: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want the IC rule to match sev.sla_breached (its second event), got %d", len(items))
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
		if err := s.Delete(ctx, icRule.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		items, _ := s.List(ctx)
		if len(items) != 1 {
			t.Fatalf("want 1 row after delete, got %d", len(items))
		}
	})

	t.Run("Delete_NotFound", func(t *testing.T) {
		err := s.Delete(ctx, icRule.ID)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound on second delete, got %v", err)
		}
	})
}

func int16Ptr(v int16) *int16 { return &v }
