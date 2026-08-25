//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

// sevIDPattern matches internal/sev.FormatID's "SEV-<year>-<seq>" shape.
var sevIDPattern = regexp.MustCompile(`^SEV-\d{4}-\d{4,}$`)

// newSEVForTest returns a minimal valid *store.SEV, ready for Create.
func newSEVForTest(title string) *store.SEV {
	now := time.Now()
	return &store.SEV{
		Title:         title,
		Description:   "test description",
		SeverityLevel: 2,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestSEVStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewSEVStore(pool)

	sv := newSEVForTest("API latency spike")

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, sv); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if sv.ID == "" {
			t.Fatal("Create must assign a non-empty ID")
		}
	})

	t.Run("CreateAssignsFormatIDShape", func(t *testing.T) {
		// Guards against the ID format drifting between the postgres and
		// memory store implementations (internal/sev.FormatID is the single
		// source of truth both should use) — a regexp match here is a cheap
		// backstop against that drift going unnoticed.
		if !sevIDPattern.MatchString(sv.ID) {
			t.Fatalf("ID %q does not match expected SEV-<year>-<seq> shape", sv.ID)
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, sv.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Title != sv.Title {
			t.Fatalf("title mismatch: want %q got %q", sv.Title, got.Title)
		}
		if got.SeverityLevel != sv.SeverityLevel {
			t.Fatalf("severity mismatch: want %d got %d", sv.SeverityLevel, got.SeverityLevel)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		sv.Status = store.SEVStatusInvestigating
		sv.Sensitive = true
		if err := s.Update(ctx, sv); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, err := s.Get(ctx, sv.ID)
		if err != nil {
			t.Fatalf("Get after Update: %v", err)
		}
		if got.Status != store.SEVStatusInvestigating {
			t.Fatalf("status not updated: got %q", got.Status)
		}
		if !got.Sensitive {
			t.Fatal("sensitive not updated")
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := newSEVForTest("ghost")
		ghost.ID = "missing"
		if err := s.Update(ctx, ghost); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		items, err := s.List(ctx, store.SEVFilter{})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("ListFilterBySeverity", func(t *testing.T) {
		items, err := s.List(ctx, store.SEVFilter{SeverityLevels: []int16{1}})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 0 {
			t.Fatal("expected empty result for severity 1")
		}
	})

	t.Run("ListFilterByStatus", func(t *testing.T) {
		items, err := s.List(ctx, store.SEVFilter{Statuses: []store.SEVStatus{store.SEVStatusOpen}})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 0 {
			t.Fatal("expected empty: status was changed to investigating")
		}
	})

	t.Run("ListSearch", func(t *testing.T) {
		// Exercises the tsvector trigger (migrations/000002_schema.up.sql) —
		// this only passes if search_vector was actually populated on
		// insert/update, not merely if the column exists.
		items, err := s.List(ctx, store.SEVFilter{Search: "latency"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("search: want 1, got %d", len(items))
		}
	})

	t.Run("Count", func(t *testing.T) {
		n, err := s.Count(ctx, store.SEVFilter{})
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if n != 1 {
			t.Fatalf("want 1, got %d", n)
		}
	})

	t.Run("UpdateLocked", func(t *testing.T) {
		if err := s.UpdateLocked(ctx, sv.ID, true); err != nil {
			t.Fatalf("UpdateLocked: %v", err)
		}
		got, err := s.Get(ctx, sv.ID)
		if err != nil {
			t.Fatalf("Get after UpdateLocked: %v", err)
		}
		if !got.Locked {
			t.Fatal("expected locked=true")
		}
	})

	t.Run("UpdateLockedNotFound", func(t *testing.T) {
		if err := s.UpdateLocked(ctx, "missing", true); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

func TestSEVStore_ExcludeSensitive(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewSEVStore(pool)

	normal := newSEVForTest("normal incident")
	sensitive := newSEVForTest("security incident")
	sensitive.Sensitive = true

	if err := s.Create(ctx, normal); err != nil {
		t.Fatalf("Create normal: %v", err)
	}
	if err := s.Create(ctx, sensitive); err != nil {
		t.Fatalf("Create sensitive: %v", err)
	}

	t.Run("ExcludeSensitive_DropsSensitiveSEV", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{ExcludeSensitive: true})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 || got[0].ID != normal.ID {
			t.Fatalf("want only %s, got %v", normal.ID, got)
		}
		n, err := s.Count(ctx, store.SEVFilter{ExcludeSensitive: true})
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if n != 1 {
			t.Fatalf("want count=1, got %d", n)
		}
	})

	t.Run("ExcludeSensitive_FalseIncludesBoth", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 (both normal and sensitive), got %d", len(got))
		}
	})
}

func TestSEVStore_ListSortAndLimit(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	s := postgres.NewSEVStore(pool)

	low := newSEVForTest("low severity")
	low.SeverityLevel = 4
	high := newSEVForTest("high severity")
	high.SeverityLevel = 1

	if err := s.Create(ctx, low); err != nil {
		t.Fatalf("Create low: %v", err)
	}
	if err := s.Create(ctx, high); err != nil {
		t.Fatalf("Create high: %v", err)
	}

	t.Run("SortBySeverityAscending", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{Sort: store.SEVSortSeverity})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 2 || got[0].ID != high.ID || got[1].ID != low.ID {
			t.Fatalf("want [high, low], got %v", ids(got))
		}
	})

	t.Run("SortBySeverityDescending", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{Sort: store.SEVSortSeverity, SortDesc: true})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 2 || got[0].ID != low.ID || got[1].ID != high.ID {
			t.Fatalf("want [low, high], got %v", ids(got))
		}
	})

	t.Run("Limit", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{Limit: 1})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1, got %d", len(got))
		}
	})
}

func ids(records []*store.SEV) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.ID
	}
	return out
}

func TestStatusHistoryStore(t *testing.T) {
	pool := newTestPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	sevs := postgres.NewSEVStore(pool)
	s := postgres.NewStatusHistoryStore(pool)

	sv := newSEVForTest("state machine test")
	if err := sevs.Create(ctx, sv); err != nil {
		t.Fatalf("seed SEV: %v", err)
	}

	fromOpen := store.SEVStatusOpen
	h := &store.SEVStatusHistory{
		SEVID:          sv.ID,
		FromStatus:     &fromOpen,
		ToStatus:       store.SEVStatusInvestigating,
		UserID:         "user-1",
		TransitionedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, h); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if h.ID == 0 {
			t.Fatal("Create must assign a non-zero ID")
		}
	})

	t.Run("CreateWithNilFromStatus", func(t *testing.T) {
		// The very first transition (SEV creation) has no prior status.
		initial := &store.SEVStatusHistory{
			SEVID: sv.ID, ToStatus: store.SEVStatusOpen, UserID: "user-1", TransitionedAt: time.Now(),
		}
		if err := s.Create(ctx, initial); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		got, err := s.ListBySEVID(ctx, sv.ID)
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2, got %d", len(got))
		}
	})

	t.Run("ListBySEVID_Unknown", func(t *testing.T) {
		got, err := s.ListBySEVID(ctx, "missing")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("want 0, got %d", len(got))
		}
	})
}
