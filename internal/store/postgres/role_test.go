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

// TestRoleStore covers Assign/Remove/ListBySEVID/ListSEVIDsByUser against a
// real database. sev_roles.sev_id carries a real FK to sevs(id) and
// role_type is constrained by a CHECK clause to the enumerated
// store.SEVRole* values (migrations/000002_schema.up.sql), neither of which
// the memory fake enforces, so this is also a check that the store.SEVRole*
// constants actually match the migration's CHECK list.
func TestRoleStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewRoleStore(pool)

	sv := newSEVForTest("role assignment test")
	if err := sevs.Create(ctx, sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}

	userID := "user-alice"
	role := &store.SEVRole{
		SEVID:       sv.ID,
		RoleType:    store.SEVRoleIncidentCommander,
		UserID:      &userID,
		DisplayName: "Alice",
		CreatedAt:   time.Now(),
		CreatedBy:   "user-admin",
	}

	t.Run("Assign", func(t *testing.T) {
		if err := s.Assign(ctx, role); err != nil {
			t.Fatalf("Assign: %v", err)
		}
		if role.ID == 0 {
			t.Fatal("ID should be set after Assign")
		}
	})

	t.Run("AssignEveryRoleType", func(t *testing.T) {
		// Confirms store.SEVRole* matches every value in the migration's
		// role_type CHECK constraint, not just IncidentCommander.
		types := []store.SEVRoleType{
			store.SEVRoleOnCall, store.SEVRoleDetectedBy, store.SEVRoleIncidentCommander,
			store.SEVRoleCommsLead, store.SEVRoleRecorder, store.SEVRoleResponder,
		}
		for _, rt := range types {
			r := &store.SEVRole{
				SEVID: sv.ID, RoleType: rt, DisplayName: string(rt),
				CreatedAt: time.Now(), CreatedBy: "user-admin",
			}
			if err := s.Assign(ctx, r); err != nil {
				t.Errorf("Assign(%s): %v", rt, err)
			}
		}
	})

	t.Run("AssignWithoutUserID", func(t *testing.T) {
		// §5: roles may reference free-form text (e.g. an external party or
		// automated system) instead of a system user.
		r := &store.SEVRole{
			SEVID: sv.ID, RoleType: store.SEVRoleDetectedBy, DisplayName: "Datadog alert",
			CreatedAt: time.Now(), CreatedBy: "user-admin",
		}
		if err := s.Assign(ctx, r); err != nil {
			t.Fatalf("Assign: %v", err)
		}
		got, err := s.ListBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		var found bool
		for _, item := range got {
			if item.ID == r.ID {
				found = true
				if item.UserID != nil {
					t.Errorf("UserID = %v, want nil", item.UserID)
				}
			}
		}
		if !found {
			t.Fatal("assigned role not found in ListBySEVID")
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		got, err := s.ListBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		// 1 (Assign) + 6 (AssignEveryRoleType) + 1 (AssignWithoutUserID) = 8
		if len(got) != 8 {
			t.Fatalf("want 8, got %d", len(got))
		}
	})

	t.Run("ListBySEVID_OtherSEV", func(t *testing.T) {
		otherSEV := newSEVForTest("unrelated SEV")
		if err := sevs.Create(ctx, otherSEV); err != nil {
			t.Fatalf("seed other SEV: %v", err)
		}
		got, err := s.ListBySEVID(ctx, otherSEV.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(got) != 0 {
			t.Fatal("expected empty for a SEV with no role assignments")
		}
	})

	t.Run("ListSEVIDsByUser_MatchesUserID", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "user-alice", nil)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 1 || ids[0] != sv.ID {
			t.Fatalf("want [%s], got %v", sv.ID, ids)
		}
	})

	t.Run("ListSEVIDsByUser_MatchesDisplayName", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "Datadog alert", nil)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 1 || ids[0] != sv.ID {
			t.Fatalf("want [%s], got %v", sv.ID, ids)
		}
	})

	t.Run("ListSEVIDsByUser_FilteredByRoleType", func(t *testing.T) {
		ic := store.SEVRoleIncidentCommander
		ids, err := s.ListSEVIDsByUser(ctx, "user-alice", &ic)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 1 || ids[0] != sv.ID {
			t.Fatalf("want [%s], got %v", sv.ID, ids)
		}

		onCall := store.SEVRoleOnCall
		ids, err = s.ListSEVIDsByUser(ctx, "user-alice", &onCall)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("want none (user-alice never held on-call), got %v", ids)
		}
	})

	t.Run("ListSEVIDsByUser_EmptyUser", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "", nil)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if ids != nil {
			t.Fatalf("want nil, got %v", ids)
		}
	})

	t.Run("Remove", func(t *testing.T) {
		if err := s.Remove(ctx, sv.ID, role.ID); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		got, err := s.ListBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		for _, item := range got {
			if item.ID == role.ID {
				t.Fatal("removed role still present")
			}
		}
	})

	t.Run("RemoveNotFound", func(t *testing.T) {
		if err := s.Remove(ctx, sv.ID, 999999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
