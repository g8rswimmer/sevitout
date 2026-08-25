package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// Compile-time interface compliance checks.
var (
	_ store.SEVStore               = (*memory.SEVStore)(nil)
	_ store.StatusHistoryStore     = (*memory.StatusHistoryStore)(nil)
	_ store.PostmortemStore        = (*memory.PostmortemStore)(nil)
	_ store.AuditStore             = (*memory.AuditStore)(nil)
	_ store.AnnouncementStore      = (*memory.AnnouncementStore)(nil)
	_ store.ChatStore              = (*memory.ChatStore)(nil)
	_ store.TaskStore              = (*memory.TaskStore)(nil)
	_ store.SEVLinkStore           = (*memory.SEVLinkStore)(nil)
	_ store.SLIStore               = (*memory.SLIStore)(nil)
	_ store.UserStore              = (*memory.UserStore)(nil)
	_ store.ServiceStore           = (*memory.ServiceStore)(nil)
	_ store.OnCallStore            = (*memory.OnCallStore)(nil)
	_ store.AIPluginStore          = (*memory.AIPluginStore)(nil)
	_ store.IntegrationConfigStore = (*memory.IntegrationConfigStore)(nil)
	_ store.RetentionConfigStore   = (*memory.RetentionConfigStore)(nil)
	_ store.ShareStore             = (*memory.ShareStore)(nil)
	_ store.RoleStore              = (*memory.RoleStore)(nil)
	_ store.SEVAccessStore         = (*memory.SEVAccessStore)(nil)
)

var ctx = context.Background()

// ── SEVStore ──────────────────────────────────────────────────────────────────

func TestSEVStore(t *testing.T) {
	s := memory.NewSEVStore()

	sev := &store.SEV{
		Title:         "API latency spike",
		Description:   "P99 latency exceeded 5 s",
		SeverityLevel: 2,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-1",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, sev); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if sev.ID == "" {
			t.Fatal("Create must assign a non-empty ID")
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, sev.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Title != sev.Title {
			t.Fatalf("title mismatch: want %q got %q", sev.Title, got.Title)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "missing"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		sev.Status = store.SEVStatusInvestigating
		if err := s.Update(ctx, sev); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.Get(ctx, sev.ID)
		if got.Status != store.SEVStatusInvestigating {
			t.Fatalf("status not updated")
		}
	})

	t.Run("UpdateNotFound", func(t *testing.T) {
		ghost := &store.SEV{ID: "missing"}
		if err := s.Update(ctx, ghost); err != store.ErrNotFound {
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
		items, _ := s.List(ctx, store.SEVFilter{SeverityLevels: []int16{1}})
		if len(items) != 0 {
			t.Fatal("expected empty result for severity 1")
		}
	})

	t.Run("ListFilterByStatus", func(t *testing.T) {
		items, _ := s.List(ctx, store.SEVFilter{Statuses: []store.SEVStatus{store.SEVStatusOpen}})
		if len(items) != 0 {
			t.Fatal("expected empty: status was changed to investigating")
		}
	})

	t.Run("ListSearch", func(t *testing.T) {
		items, _ := s.List(ctx, store.SEVFilter{Search: "latency"})
		if len(items) != 1 {
			t.Fatalf("search: want 1, got %d", len(items))
		}
	})

	t.Run("UpdateLocked", func(t *testing.T) {
		if err := s.UpdateLocked(ctx, sev.ID, true); err != nil {
			t.Fatalf("UpdateLocked: %v", err)
		}
		got, _ := s.Get(ctx, sev.ID)
		if !got.Locked {
			t.Fatal("expected locked=true")
		}
	})

	t.Run("UpdateLockedNotFound", func(t *testing.T) {
		if err := s.UpdateLocked(ctx, "missing", true); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

// ── SEVStore filter/sort combinations (M08 search) ─────────────────────────────

func TestSEVStore_FilterAndSort(t *testing.T) {
	s := memory.NewSEVStore()

	strPtr := func(v string) *string { return &v }
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t0, t1, t2, t3 := base, base.AddDate(0, 0, 1), base.AddDate(0, 0, 2), base.AddDate(0, 0, 3)
	sec := func(n int64) *int64 { return &n }

	sevs := []*store.SEV{
		{
			Title: "Checkout revenue drop", Description: "checkout failing",
			SeverityLevel: 1, Status: store.SEVStatusOpen,
			AffectedServices: []string{"checkout"},
			Tags:             map[string]string{"team": "payments"},
			BusinessImpact:   strPtr("revenue drop across checkout"),
			StartedAt:        &t0,
			CreatedAt:        t0, UpdatedAt: t0, CreatedBy: "user-1",
		},
		{
			Title: "Billing errors", Description: "billing 500s",
			SeverityLevel: 2, Status: store.SEVStatusInvestigating,
			AffectedServices:  []string{"checkout", "billing"},
			Tags:              map[string]string{"team": "payments", "region": "us"},
			RootCauseCategory: strPtr("deployment"),
			StartedAt:         &t1,
			CreatedAt:         t1, UpdatedAt: t1, CreatedBy: "user-1",
		},
		{
			Title: "Auth outage", Description: "login failing",
			SeverityLevel: 3, Status: store.SEVStatusResolved,
			AffectedServices:     []string{"auth"},
			Tags:                 map[string]string{"team": "identity"},
			RootCauseDescription: strPtr("bad config pushed to prod"),
			StartedAt:            &t2,
			MTTRSeconds:          sec(3600),
			CreatedAt:            t2, UpdatedAt: t2, CreatedBy: "user-1",
		},
		{
			Title: "Auth flakiness", Description: "intermittent 401s",
			SeverityLevel: 1, Status: store.SEVStatusPostmortemInProgress,
			AffectedServices: []string{"auth"},
			StartedAt:        &t3,
			MTTRSeconds:      sec(7200),
			CreatedAt:        t3, UpdatedAt: t3, CreatedBy: "user-1",
		},
		{
			Title: "Notification delay", Description: "delayed pushes",
			SeverityLevel: 4, Status: store.SEVStatusPostmortemComplete,
			AffectedServices: []string{"notifications"},
			CreatedAt:        base.AddDate(0, 0, 4), UpdatedAt: base.AddDate(0, 0, 4), CreatedBy: "user-1",
		},
	}
	for _, sv := range sevs {
		if err := s.Create(ctx, sv); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	checkout, billing, auth1, auth2, notif := sevs[0], sevs[1], sevs[2], sevs[3], sevs[4]

	wantIDs := func(t *testing.T, got []*store.SEV, want ...*store.SEV) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("want %d results, got %d", len(want), len(got))
		}
		wantSet := make(map[string]bool, len(want))
		for _, w := range want {
			wantSet[w.ID] = true
		}
		for _, g := range got {
			if !wantSet[g.ID] {
				t.Errorf("unexpected result %s (%s)", g.ID, g.Title)
			}
		}
	}

	t.Run("ServiceIDs", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{ServiceIDs: []string{"checkout"}})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		wantIDs(t, got, checkout, billing)
	})

	t.Run("Tags_SingleKey", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{Tags: map[string]string{"team": "payments"}})
		wantIDs(t, got, checkout, billing)
	})

	t.Run("Tags_MultipleKeysAllMustMatch", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{Tags: map[string]string{"team": "payments", "region": "us"}})
		wantIDs(t, got, billing)
	})

	t.Run("RootCauseCategory", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{RootCauseCategory: "deployment"})
		wantIDs(t, got, billing)
	})

	t.Run("StartedDateRange", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{StartedAfter: &t1, StartedBefore: &t2})
		wantIDs(t, got, billing, auth1)
	})

	t.Run("IDs_Allowlist", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{IDs: []string{checkout.ID, auth1.ID}})
		wantIDs(t, got, checkout, auth1)
	})

	t.Run("IDs_EmptyAllowlistMatchesNothing", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{IDs: []string{}})
		if len(got) != 0 {
			t.Fatalf("want 0 results for empty (non-nil) allowlist, got %d", len(got))
		}
	})

	t.Run("Search_MatchesBusinessImpact", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{Search: "revenue"})
		wantIDs(t, got, checkout)
	})

	t.Run("Search_MatchesRootCauseDescription", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{Search: "bad config"})
		wantIDs(t, got, auth1)
	})

	t.Run("Combination_StatusAndService", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{
			Statuses:   []store.SEVStatus{store.SEVStatusInvestigating},
			ServiceIDs: []string{"billing"},
		})
		wantIDs(t, got, billing)
	})

	t.Run("Sort_SeverityAscending", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{Sort: store.SEVSortSeverity})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 5 {
			t.Fatalf("want 5, got %d", len(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i].SeverityLevel < got[i-1].SeverityLevel {
				t.Fatalf("results not ascending by severity: %v", severities(got))
			}
		}
		if got[len(got)-1].ID != notif.ID {
			t.Fatalf("want highest severity (notif) last, got %v", severities(got))
		}
	})

	t.Run("Sort_MTTRDescending_NullsLast", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{Sort: store.SEVSortMTTR, SortDesc: true})
		if got[0].ID != auth2.ID || got[1].ID != auth1.ID {
			t.Fatalf("want auth2 (7200s) then auth1 (3600s) first, got %s, %s", got[0].ID, got[1].ID)
		}
		lastThree := map[string]bool{got[2].ID: true, got[3].ID: true, got[4].ID: true}
		for _, want := range []*store.SEV{checkout, billing, notif} {
			if !lastThree[want.ID] {
				t.Errorf("expected %s (no MTTR) to sort after all set MTTR values", want.ID)
			}
		}
	})

	t.Run("Sort_StartedAtNullsLastRegardlessOfDirection", func(t *testing.T) {
		got, _ := s.List(ctx, store.SEVFilter{Sort: store.SEVSortStartedAt, SortDesc: true})
		if got[len(got)-1].ID != notif.ID {
			t.Fatalf("want notif (nil StartedAt) last even when sorting descending, got last=%s", got[len(got)-1].ID)
		}
	})

	// checkout and auth2 are both severity 1 (tied); checkout was created
	// first so it must sort before auth2 in both directions.
	t.Run("Sort_TiesBrokenByIDRegardlessOfDirection", func(t *testing.T) {
		indexOfID := func(records []*store.SEV, id string) int {
			for i, r := range records {
				if r.ID == id {
					return i
				}
			}
			return -1
		}
		for _, desc := range []bool{false, true} {
			got, err := s.List(ctx, store.SEVFilter{Sort: store.SEVSortSeverity, SortDesc: desc})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			i1, i2 := indexOfID(got, checkout.ID), indexOfID(got, auth2.ID)
			if i1 < 0 || i2 < 0 {
				t.Fatalf("expected both tied severity=1 records present")
			}
			if i1 > i2 {
				t.Errorf("desc=%v: want checkout (%s) before auth2 (%s) among tied records, got reversed", desc, checkout.ID, auth2.ID)
			}
		}
	})

	t.Run("Sort_DefaultIsDescendingByCreatedAt", func(t *testing.T) {
		got, err := s.List(ctx, store.SEVFilter{})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got[0].ID != notif.ID {
			t.Fatalf("want most-recently-created (notif) first by default, got %s", got[0].ID)
		}
		if got[len(got)-1].ID != checkout.ID {
			t.Fatalf("want earliest-created (checkout) last by default, got %s", got[len(got)-1].ID)
		}
	})
}

func TestSEVStore_ExcludeSensitive(t *testing.T) {
	s := memory.NewSEVStore()
	ctx := context.Background()

	normal := &store.SEV{Title: "normal incident", SeverityLevel: 2, Status: store.SEVStatusOpen, CreatedAt: time.Now(), UpdatedAt: time.Now(), CreatedBy: "user-1"}
	sensitive := &store.SEV{Title: "security incident", SeverityLevel: 2, Status: store.SEVStatusOpen, Sensitive: true, CreatedAt: time.Now(), UpdatedAt: time.Now(), CreatedBy: "user-1"}
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

func severities(records []*store.SEV) []int16 {
	out := make([]int16, len(records))
	for i, r := range records {
		out[i] = r.SeverityLevel
	}
	return out
}

// ── PostmortemStore ───────────────────────────────────────────────────────────

func TestPostmortemStore(t *testing.T) {
	s := memory.NewPostmortemStore()

	pm := &store.Postmortem{
		SEVID:     "SEV-2026-0001",
		Status:    store.PostmortemStatusDraft,
		Content:   "## Summary\n",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, pm); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if pm.ID == 0 {
			t.Fatal("ID should be set after Create")
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		if err := s.Create(ctx, pm); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("GetBySEVID", func(t *testing.T) {
		got, err := s.GetBySEVID(ctx, pm.SEVID)
		if err != nil {
			t.Fatalf("GetBySEVID: %v", err)
		}
		if got.Status != store.PostmortemStatusDraft {
			t.Fatal("status mismatch")
		}
	})

	t.Run("GetBySEVIDNotFound", func(t *testing.T) {
		if _, err := s.GetBySEVID(ctx, "missing"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		pm.Status = store.PostmortemStatusInReview
		if err := s.Update(ctx, pm); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.GetBySEVID(ctx, pm.SEVID)
		if got.Status != store.PostmortemStatusInReview {
			t.Fatal("status not updated")
		}
	})

	t.Run("CountByStatus", func(t *testing.T) {
		if err := s.Create(ctx, &store.Postmortem{
			SEVID:     "SEV-2026-0002",
			Status:    store.PostmortemStatusApproved,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		counts, err := s.CountByStatus(ctx)
		if err != nil {
			t.Fatalf("CountByStatus: %v", err)
		}
		// pm was moved to InReview above; the freshly-created one is Approved.
		if counts[store.PostmortemStatusInReview] != 1 {
			t.Errorf("InReview count = %d, want 1", counts[store.PostmortemStatusInReview])
		}
		if counts[store.PostmortemStatusApproved] != 1 {
			t.Errorf("Approved count = %d, want 1", counts[store.PostmortemStatusApproved])
		}
	})
}

// ── AuditStore ────────────────────────────────────────────────────────────────

func TestAuditStore(t *testing.T) {
	s := memory.NewAuditStore()

	entry := &store.AuditEntry{
		SEVID:     "SEV-2026-0001",
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
		e2 := &store.AuditEntry{SEVID: "SEV-2026-0001", UserID: "user-1", Action: "update"}
		if err := s.Append(ctx, e2); err != nil {
			t.Fatalf("second Append: %v", err)
		}
		if e2.ID <= entry.ID {
			t.Fatal("second ID should be greater than first")
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		entries, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("want 2 entries, got %d", len(entries))
		}
	})

	t.Run("ListBySEVIDOtherSEV", func(t *testing.T) {
		entries, _ := s.ListBySEVID(ctx, "SEV-2026-9999")
		if len(entries) != 0 {
			t.Fatal("expected empty for unknown SEV")
		}
	})
}

// ── AnnouncementStore ─────────────────────────────────────────────────────────

func TestAnnouncementStore(t *testing.T) {
	s := memory.NewAnnouncementStore()

	a := &store.Announcement{
		SEVID:    "SEV-2026-0001",
		AuthorID: "user-1",
		Message:  "SEV opened",
		Audience: store.AudienceInternal,
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, a); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if a.ID == 0 {
			t.Fatal("ID not set")
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("SearchSEVIDs_Match", func(t *testing.T) {
		ids, err := s.SearchSEVIDs(ctx, "opened")
		if err != nil {
			t.Fatalf("SearchSEVIDs: %v", err)
		}
		if len(ids) != 1 || ids[0] != "SEV-2026-0001" {
			t.Fatalf("want [SEV-2026-0001], got %v", ids)
		}
	})

	t.Run("SearchSEVIDs_NoMatch", func(t *testing.T) {
		ids, err := s.SearchSEVIDs(ctx, "nonexistent-term")
		if err != nil {
			t.Fatalf("SearchSEVIDs: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("want 0 matches, got %v", ids)
		}
		// Must be non-nil (a real, empty query result), not nil (which the
		// caller in internal/api/grpc/search.go's intersectIDs treats as
		// "unconstrained" rather than "matched nothing").
		if ids == nil {
			t.Fatal("want non-nil empty slice for a real query with zero matches, got nil")
		}
	})

	t.Run("SearchSEVIDs_EmptyQuery", func(t *testing.T) {
		ids, err := s.SearchSEVIDs(ctx, "")
		if err != nil {
			t.Fatalf("SearchSEVIDs: %v", err)
		}
		if ids != nil {
			t.Fatalf("want nil for empty query, got %v", ids)
		}
	})
}

// ── ChatStore ─────────────────────────────────────────────────────────────────

func TestChatStore(t *testing.T) {
	s := memory.NewChatStore()

	entry := &store.ChatEntry{
		SEVID:      "SEV-2026-0001",
		OccurredAt: time.Now(),
		Source:     "slack",
		Author:     "alice",
		Content:    "working on it",
		AddedBy:    "user-1",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, entry); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})
}

// ── TaskStore ─────────────────────────────────────────────────────────────────

func TestTaskStore(t *testing.T) {
	s := memory.NewTaskStore()

	task := &store.LinkedTask{
		SEVID:            "SEV-2026-0001",
		ExternalSystem:   "github",
		TaskID:           "42",
		URL:              "https://github.com/org/repo/issues/42",
		Title:            "Fix the thing",
		RelationshipType: store.TaskRelationshipActionItem,
		Priority:         store.TaskPriorityCritical,
		CreatedBy:        "user-1",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if task.ID == 0 {
			t.Fatal("ID not set")
		}
	})

	taskID := task.ID

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Title != task.Title {
			t.Fatal("title mismatch")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, 9999); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		task.Overdue = true
		if err := s.Update(ctx, task); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.Get(ctx, taskID)
		if !got.Overdue {
			t.Fatal("overdue not updated")
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("CountOverdue", func(t *testing.T) {
		past := time.Now().Add(-24 * time.Hour)
		if err := s.Create(ctx, &store.LinkedTask{
			SEVID:            "SEV-2026-0002",
			ExternalSystem:   "github",
			TaskID:           "99",
			URL:              "https://github.com/org/repo/issues/99",
			Title:            "Overdue task",
			RelationshipType: store.TaskRelationshipActionItem,
			Priority:         store.TaskPriorityCritical,
			DueDate:          &past,
			CreatedBy:        "user-1",
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		n, err := s.CountOverdue(ctx, time.Now())
		if err != nil {
			t.Fatalf("CountOverdue: %v", err)
		}
		// task (no due date) is not overdue; the freshly-created one is.
		if n != 1 {
			t.Errorf("CountOverdue = %d, want 1", n)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, taskID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, taskID); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound after delete, got %v", err)
		}
	})
}

// ── SEVLinkStore ──────────────────────────────────────────────────────────────

func TestSEVLinkStore(t *testing.T) {
	s := memory.NewSEVLinkStore()

	link := &store.SEVLink{
		SourceSEVID:      "SEV-2026-0001",
		TargetSEVID:      "SEV-2026-0002",
		RelationshipType: store.SEVRelationshipRelated,
		CreatedBy:        "user-1",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, link); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		if err := s.Create(ctx, link); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("ListBySEVIDTarget", func(t *testing.T) {
		items, _ := s.ListBySEVID(ctx, "SEV-2026-0002")
		if len(items) != 1 {
			t.Fatal("link should appear on both sides")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, "SEV-2026-0001", "SEV-2026-0002", store.SEVRelationshipRelated); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		items, _ := s.ListBySEVID(ctx, "SEV-2026-0001")
		if len(items) != 0 {
			t.Fatal("expected empty after delete")
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := s.Delete(ctx, "a", "b", store.SEVRelationshipRelated); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

// ── SLIStore ──────────────────────────────────────────────────────────────────

func TestSLIStore(t *testing.T) {
	s := memory.NewSLIStore()

	sli := &store.SLI{
		SEVID:         "SEV-2026-0001",
		SLIName:       "availability",
		SLOThreshold:  "99.9%",
		MeasuredValue: "98.1%",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, sli); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if sli.ID == 0 {
			t.Fatal("ID not set")
		}
	})

	sliID := sli.ID

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, sliID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		items, _ := s.ListBySEVID(ctx, "SEV-2026-0001")
		if len(items) != 0 {
			t.Fatal("expected empty after delete")
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := s.Delete(ctx, 9999); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

// ── UserStore ─────────────────────────────────────────────────────────────────

func TestUserStore(t *testing.T) {
	s := memory.NewUserStore()

	user := &store.User{
		ID:      "usr-abc",
		Email:   "alice@example.com",
		Name:    "Alice",
		OrgRole: store.OrgRoleResponder,
		Active:  true,
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		if err := s.Create(ctx, user); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("CreateEmailConflict", func(t *testing.T) {
		dup := &store.User{ID: "usr-xyz", Email: "alice@example.com"}
		if err := s.Create(ctx, dup); err != store.ErrConflict {
			t.Fatalf("want ErrConflict on email dup, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, user.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Email != user.Email {
			t.Fatal("email mismatch")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "missing"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("GetByEmail", func(t *testing.T) {
		got, err := s.GetByEmail(ctx, user.Email)
		if err != nil {
			t.Fatalf("GetByEmail: %v", err)
		}
		if got.ID != user.ID {
			t.Fatal("id mismatch")
		}
	})

	t.Run("GetByEmailNotFound", func(t *testing.T) {
		if _, err := s.GetByEmail(ctx, "nobody@example.com"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Update", func(t *testing.T) {
		user.OrgRole = store.OrgRoleAdmin
		if err := s.Update(ctx, user); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.Get(ctx, user.ID)
		if got.OrgRole != store.OrgRoleAdmin {
			t.Fatal("role not updated")
		}
	})

	t.Run("List", func(t *testing.T) {
		users, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(users) != 1 {
			t.Fatalf("want 1, got %d", len(users))
		}
	})
}

// ── ServiceStore ──────────────────────────────────────────────────────────────

func TestServiceStore(t *testing.T) {
	s := memory.NewServiceStore()

	svc := &store.Service{
		ID:     "svc-api",
		Name:   "API Service",
		Active: true,
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, svc); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		if err := s.Create(ctx, svc); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, svc.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != svc.Name {
			t.Fatal("name mismatch")
		}
	})

	t.Run("Update", func(t *testing.T) {
		svc.Active = false
		if err := s.Update(ctx, svc); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.Get(ctx, svc.ID)
		if got.Active {
			t.Fatal("active should be false")
		}
	})

	t.Run("ListActiveOnly", func(t *testing.T) {
		items, _ := s.List(ctx, true)
		if len(items) != 0 {
			t.Fatal("inactive service should be excluded")
		}
	})

	t.Run("ListAll", func(t *testing.T) {
		items, _ := s.List(ctx, false)
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, svc.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, svc.ID); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound after delete, got %v", err)
		}
	})
}

// ── OnCallStore ───────────────────────────────────────────────────────────────

func TestOnCallStore(t *testing.T) {
	s := memory.NewOnCallStore()

	svcID := "svc-api"
	r := &store.OnCallRotation{
		Name:      "API on-call",
		ServiceID: &svcID,
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

	t.Run("GetCurrentOnCall", func(t *testing.T) {
		got, err := s.GetCurrentOnCall(ctx, svcID)
		if err != nil {
			t.Fatalf("GetCurrentOnCall: %v", err)
		}
		if got.Name != r.Name {
			t.Fatal("name mismatch")
		}
	})

	t.Run("GetCurrentOnCallNotFound", func(t *testing.T) {
		if _, err := s.GetCurrentOnCall(ctx, "svc-missing"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("Update", func(t *testing.T) {
		r.Name = "API on-call (updated)"
		if err := s.Update(ctx, r); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.Get(ctx, r.ID)
		if got.Name != "API on-call (updated)" {
			t.Fatal("name not updated")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, r.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, r.ID); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound after delete, got %v", err)
		}
	})
}

// ── AIPluginStore ─────────────────────────────────────────────────────────────

func TestAIPluginStore(t *testing.T) {
	s := memory.NewAIPluginStore()

	plugin := &store.AIPlugin{
		Name:        "anthropic",
		Version:     "1.0.0",
		HandlerType: store.AIHandlerBuiltin,
		Enabled:     false,
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, plugin); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	t.Run("CreateDuplicateName", func(t *testing.T) {
		dup := &store.AIPlugin{Name: "anthropic", Version: "2.0.0", HandlerType: store.AIHandlerBuiltin}
		if err := s.Create(ctx, dup); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, plugin.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != plugin.Name {
			t.Fatal("name mismatch")
		}
	})

	t.Run("Update", func(t *testing.T) {
		plugin.Enabled = true
		if err := s.Update(ctx, plugin); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := s.Get(ctx, plugin.ID)
		if !got.Enabled {
			t.Fatal("enabled not updated")
		}
	})

	t.Run("List", func(t *testing.T) {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Delete(ctx, plugin.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		items, _ := s.List(ctx)
		if len(items) != 0 {
			t.Fatal("expected empty after delete")
		}
	})
}

// ── AIOutputStore ─────────────────────────────────────────────────────────────

func TestAIOutputStore(t *testing.T) {
	s := memory.NewAIOutputStore()

	t.Run("ListEmpty", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("want 0, got %d", len(items))
		}
	})

	t.Run("CreateAndList", func(t *testing.T) {
		out1 := &store.AIOutput{SEVID: "SEV-2026-0001", PluginID: 1, TriggerEvent: "manual", Action: "summarize", Content: "first"}
		out2 := &store.AIOutput{SEVID: "SEV-2026-0001", PluginID: 1, TriggerEvent: "sev.resolved", Action: "draft_postmortem", Content: "second"}
		if err := s.Create(ctx, out1); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := s.Create(ctx, out2); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if out1.ID == 0 || out2.ID == 0 || out1.ID == out2.ID {
			t.Fatalf("expected distinct assigned IDs, got %d and %d", out1.ID, out2.ID)
		}

		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2, got %d", len(items))
		}
		if items[0].Content != "first" || items[1].Content != "second" {
			t.Fatal("expected insertion order preserved")
		}
	})

	t.Run("ScopedBySEVID", func(t *testing.T) {
		other := &store.AIOutput{SEVID: "SEV-2026-0002", PluginID: 1, TriggerEvent: "manual", Action: "summarize", Content: "unrelated"}
		if err := s.Create(ctx, other); err != nil {
			t.Fatalf("Create: %v", err)
		}
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2 (unaffected by other SEV's output), got %d", len(items))
		}
	})
}

// ── IntegrationConfigStore ────────────────────────────────────────────────────

func TestIntegrationConfigStore(t *testing.T) {
	s := memory.NewIntegrationConfigStore()

	cfg := &store.IntegrationConfig{
		IntegrationType: "slack",
		Settings:        map[string]any{"default_channel": "#incidents"},
	}

	t.Run("Upsert (insert)", func(t *testing.T) {
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if cfg.ID == 0 {
			t.Fatal("ID not set on insert")
		}
	})

	firstID := cfg.ID

	t.Run("Get", func(t *testing.T) {
		got, err := s.Get(ctx, "slack")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != firstID {
			t.Fatal("id mismatch")
		}
	})

	t.Run("Upsert (update)", func(t *testing.T) {
		cfg.Settings = map[string]any{"default_channel": "#sev-events"}
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert update: %v", err)
		}
		if cfg.ID != firstID {
			t.Fatal("ID should not change on update")
		}
		got, _ := s.Get(ctx, "slack")
		if got.Settings["default_channel"] != "#sev-events" {
			t.Fatal("settings not updated")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		if _, err := s.Get(ctx, "pagerduty"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
	})
}

// ── RetentionConfigStore ──────────────────────────────────────────────────────

func TestRetentionConfigStore(t *testing.T) {
	t.Run("PreSeededDefaults", func(t *testing.T) {
		s := memory.NewRetentionConfigStore()
		items, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 4 {
			t.Fatalf("want 4 pre-seeded levels, got %d", len(items))
		}
		for i, cfg := range items {
			wantLevel := int16(i + 1)
			if cfg.SeverityLevel != wantLevel {
				t.Errorf("List[%d].SeverityLevel = %d, want %d (ordered ascending)", i, cfg.SeverityLevel, wantLevel)
			}
			if cfg.RetentionDays != 0 || cfg.HardDelete {
				t.Errorf("severity %d: want retain-forever defaults, got %+v", cfg.SeverityLevel, cfg)
			}
		}
	})

	t.Run("Get", func(t *testing.T) {
		s := memory.NewRetentionConfigStore()
		got, err := s.Get(ctx, 1)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.SeverityLevel != 1 {
			t.Errorf("SeverityLevel = %d, want 1", got.SeverityLevel)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		s := memory.NewRetentionConfigStore()
		if _, err := s.Get(ctx, 9); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("Upsert (update existing)", func(t *testing.T) {
		s := memory.NewRetentionConfigStore()
		existing, _ := s.Get(ctx, 3)

		cfg := &store.RetentionConfig{SeverityLevel: 3, RetentionDays: 90, HardDelete: true}
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if cfg.ID != existing.ID {
			t.Errorf("Upsert should preserve the pre-seeded ID, got %d want %d", cfg.ID, existing.ID)
		}

		got, err := s.Get(ctx, 3)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.RetentionDays != 90 || !got.HardDelete {
			t.Errorf("severity 3 = %+v, want retention_days=90 hard_delete=true", got)
		}
		// Other levels are untouched.
		other, _ := s.Get(ctx, 4)
		if other.RetentionDays != 0 || other.HardDelete {
			t.Errorf("severity 4 should be unaffected by updating severity 3, got %+v", other)
		}
	})

	t.Run("Upsert (severity level not pre-seeded)", func(t *testing.T) {
		// RetentionConfigStore does not restrict severity levels itself — the
		// gRPC handler enforces the 1-4 range (see internal/api/grpc.validateSeverityLevel).
		s := memory.NewRetentionConfigStore()
		cfg := &store.RetentionConfig{SeverityLevel: 5, RetentionDays: 30}
		if err := s.Upsert(ctx, cfg); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if cfg.ID == 0 {
			t.Fatal("ID should be set on insert")
		}
		got, err := s.Get(ctx, 5)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.RetentionDays != 30 {
			t.Errorf("RetentionDays = %d, want 30", got.RetentionDays)
		}
	})
}

// ── RoleStore ─────────────────────────────────────────────────────────────────

func TestRoleStore(t *testing.T) {
	s := memory.NewRoleStore()

	role := &store.SEVRole{
		SEVID:       "SEV-2026-0001",
		RoleType:    store.SEVRoleIncidentCommander,
		DisplayName: "Alice",
		CreatedBy:   "user-1",
		CreatedAt:   time.Now(),
	}

	t.Run("Assign", func(t *testing.T) {
		if err := s.Assign(ctx, role); err != nil {
			t.Fatalf("Assign: %v", err)
		}
		if role.ID == 0 {
			t.Fatal("ID should be set after Assign")
		}
	})

	roleID := role.ID

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
		if err != nil {
			t.Fatalf("ListBySEVID: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("want 1, got %d", len(items))
		}
		if items[0].RoleType != store.SEVRoleIncidentCommander {
			t.Errorf("role_type = %q, want incident-commander", items[0].RoleType)
		}
	})

	t.Run("ListBySEVID_OtherSEV", func(t *testing.T) {
		items, _ := s.ListBySEVID(ctx, "SEV-2026-9999")
		if len(items) != 0 {
			t.Fatal("expected empty for unknown SEV")
		}
	})

	t.Run("AssignMultipleRoles", func(t *testing.T) {
		r2 := &store.SEVRole{
			SEVID:       "SEV-2026-0001",
			RoleType:    store.SEVRoleResponder,
			DisplayName: "Bob",
			CreatedBy:   "user-1",
			CreatedAt:   time.Now(),
		}
		if err := s.Assign(ctx, r2); err != nil {
			t.Fatalf("Assign second: %v", err)
		}
		items, _ := s.ListBySEVID(ctx, "SEV-2026-0001")
		if len(items) != 2 {
			t.Fatalf("want 2, got %d", len(items))
		}
	})

	t.Run("Remove", func(t *testing.T) {
		if err := s.Remove(ctx, "SEV-2026-0001", roleID); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		items, _ := s.ListBySEVID(ctx, "SEV-2026-0001")
		if len(items) != 1 {
			t.Fatalf("want 1 remaining, got %d", len(items))
		}
	})

	t.Run("RemoveNotFound", func(t *testing.T) {
		if err := s.Remove(ctx, "SEV-2026-0001", 9999); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("RemoveWrongSEV", func(t *testing.T) {
		items, _ := s.ListBySEVID(ctx, "SEV-2026-0001")
		if len(items) == 0 {
			t.Skip("no roles to test with")
		}
		if err := s.Remove(ctx, "SEV-WRONG", items[0].ID); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound when sev_id doesn't match, got %v", err)
		}
	})

	// At this point only Bob's responder role remains (Alice's IC role was
	// removed above).
	t.Run("ListSEVIDsByUser_MatchesDisplayName", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "Bob", nil)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 1 || ids[0] != "SEV-2026-0001" {
			t.Fatalf("want [SEV-2026-0001], got %v", ids)
		}
	})

	t.Run("ListSEVIDsByUser_FilteredByRoleType", func(t *testing.T) {
		responder := store.SEVRoleResponder
		ids, err := s.ListSEVIDsByUser(ctx, "Bob", &responder)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 1 || ids[0] != "SEV-2026-0001" {
			t.Fatalf("want [SEV-2026-0001], got %v", ids)
		}

		ic := store.SEVRoleIncidentCommander
		ids, err = s.ListSEVIDsByUser(ctx, "Bob", &ic)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("want no results for mismatched role type, got %v", ids)
		}
	})

	t.Run("ListSEVIDsByUser_UnknownUser", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "nobody", nil)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("want no results, got %v", ids)
		}
		// Must be non-nil: a real user was queried and matched nothing, which
		// intersectIDs (internal/api/grpc/search.go) must distinguish from
		// "no user given" (nil) or it would treat this as unconstrained.
		if ids == nil {
			t.Fatal("want non-nil empty slice for a real user with zero matches, got nil")
		}
	})

	t.Run("ListSEVIDsByUser_EmptyUser", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "", nil)
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if ids != nil {
			t.Fatalf("want nil for empty user, got %v", ids)
		}
	})
}

// ── ShareStore ────────────────────────────────────────────────────────────────

func TestShareStore(t *testing.T) {
	s := memory.NewShareStore()

	link := &store.ShareableLink{
		SEVID:     "SEV-2026-0001",
		Token:     "tok-abc123",
		CreatedBy: "user-1",
	}

	t.Run("Create", func(t *testing.T) {
		if err := s.Create(ctx, link); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if link.ID == 0 {
			t.Fatal("ID not set")
		}
	})

	t.Run("CreateDuplicate", func(t *testing.T) {
		if err := s.Create(ctx, link); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("GetByToken", func(t *testing.T) {
		got, err := s.GetByToken(ctx, "tok-abc123")
		if err != nil {
			t.Fatalf("GetByToken: %v", err)
		}
		if got.SEVID != link.SEVID {
			t.Fatal("sev_id mismatch")
		}
	})

	t.Run("GetByTokenNotFound", func(t *testing.T) {
		if _, err := s.GetByToken(ctx, "missing"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
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
		got, _ := s.GetByToken(ctx, "tok-abc123")
		if !got.Revoked {
			t.Fatal("expected revoked=true")
		}
		if got.RevokedBy == nil || *got.RevokedBy != "user-1" {
			t.Fatal("revoked_by not set")
		}
	})

	t.Run("RevokeNotFound", func(t *testing.T) {
		if err := s.Revoke(ctx, "missing", "user-1"); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

func TestSEVAccessStore(t *testing.T) {
	s := memory.NewSEVAccessStore()

	grant := &store.SEVAccess{
		SEVID:     "SEV-2026-0001",
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
		dup := &store.SEVAccess{SEVID: "SEV-2026-0001", UserID: "user-alice", CreatedBy: "user-admin", CreatedAt: time.Now()}
		if err := s.Grant(ctx, dup); err != store.ErrConflict {
			t.Fatalf("want ErrConflict, got %v", err)
		}
	})

	t.Run("ListBySEVID", func(t *testing.T) {
		items, err := s.ListBySEVID(ctx, "SEV-2026-0001")
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
		items, _ := s.ListBySEVID(ctx, "SEV-2026-9999")
		if len(items) != 0 {
			t.Fatal("expected empty for unknown SEV")
		}
	})

	t.Run("HasAccess_True", func(t *testing.T) {
		ok, err := s.HasAccess(ctx, "SEV-2026-0001", "user-alice")
		if err != nil {
			t.Fatalf("HasAccess: %v", err)
		}
		if !ok {
			t.Fatal("expected true")
		}
	})

	t.Run("HasAccess_False", func(t *testing.T) {
		ok, err := s.HasAccess(ctx, "SEV-2026-0001", "user-bob")
		if err != nil {
			t.Fatalf("HasAccess: %v", err)
		}
		if ok {
			t.Fatal("expected false")
		}
	})

	t.Run("ListSEVIDsByUser", func(t *testing.T) {
		if err := s.Grant(ctx, &store.SEVAccess{SEVID: "SEV-2026-0002", UserID: "user-alice", CreatedBy: "user-admin", CreatedAt: time.Now()}); err != nil {
			t.Fatalf("Grant second: %v", err)
		}
		ids, err := s.ListSEVIDsByUser(ctx, "user-alice")
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("want 2, got %d", len(ids))
		}
	})

	t.Run("ListSEVIDsByUser_EmptyUser", func(t *testing.T) {
		ids, err := s.ListSEVIDsByUser(ctx, "")
		if err != nil {
			t.Fatalf("ListSEVIDsByUser: %v", err)
		}
		if ids != nil {
			t.Fatalf("want nil, got %v", ids)
		}
	})

	t.Run("Revoke", func(t *testing.T) {
		if err := s.Revoke(ctx, "SEV-2026-0001", grantID); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		ok, _ := s.HasAccess(ctx, "SEV-2026-0001", "user-alice")
		if ok {
			t.Fatal("expected access revoked")
		}
	})

	t.Run("RevokeNotFound", func(t *testing.T) {
		if err := s.Revoke(ctx, "SEV-2026-0001", 9999); err != store.ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}
