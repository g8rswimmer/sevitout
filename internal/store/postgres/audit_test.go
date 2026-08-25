//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// TestAuditStore covers the store behind the immutable, append-only audit
// log (docs/requirements.md §15). Unlike memory.AuditStore, audit_log.sev_id
// carries a real FK to sevs(id) (migrations/000002_schema.up.sql), so each
// case seeds a real SEV first. Immutability itself (INSERT-only audit_writer
// DB role) is covered separately by audit_integration_test.go — this file
// covers Append/ListBySEVID's ordinary read/write behavior.
func TestAuditStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewAuditStore(pool)

	sv := newSEVForTest("audit log test")
	if err := sevs.Create(ctx, sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}

	entry := &store.AuditEntry{
		SEVID:     sv.ID,
		UserID:    "user-1",
		Action:    "create",
		CreatedAt: time.Now(),
	}

	t.Run("Append", func(t *testing.T) {
		if err := s.Append(ctx, entry); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if entry.ID == 0 {
			t.Fatal("ID should be set after Append")
		}
	})

	t.Run("AppendSecond", func(t *testing.T) {
		field, old, new_ := "status", "open", "investigating"
		e2 := &store.AuditEntry{
			SEVID: sv.ID, UserID: "user-1", Action: "update",
			FieldName: &field, OldValue: &old, NewValue: &new_,
			CreatedAt: time.Now(),
		}
		if err := s.Append(ctx, e2); err != nil {
			t.Fatalf("second Append: %v", err)
		}
		if e2.ID <= entry.ID {
			t.Fatal("second ID should be greater than first")
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		entries, err := s.ListBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("want 2 entries, got %d", len(entries))
		}
		var found bool
		for _, e := range entries {
			if e.FieldName != nil && *e.FieldName == "status" {
				found = true
				if e.OldValue == nil || *e.OldValue != "open" {
					t.Errorf("OldValue = %v, want \"open\"", e.OldValue)
				}
				if e.NewValue == nil || *e.NewValue != "investigating" {
					t.Errorf("NewValue = %v, want \"investigating\"", e.NewValue)
				}
			}
		}
		if !found {
			t.Fatal("second entry's field_name/old_value/new_value did not round-trip")
		}
	})

	t.Run("ListBySEVID_OtherSEV", func(t *testing.T) {
		otherSEV := newSEVForTest("unrelated SEV")
		if err := sevs.Create(ctx, otherSEV); err != nil {
			t.Fatalf("seed other SEV: %v", err)
		}
		entries, err := s.ListBySEVID(ctx, otherSEV.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(entries) != 0 {
			t.Fatal("expected empty for a SEV with no audit entries")
		}
	})
}
