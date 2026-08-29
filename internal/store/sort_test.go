package store_test

import (
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// sortTestSEV builds a minimal *store.SEV for SortSEVs table tests — only
// id, createdAt, and whichever single sort-relevant field a given test case
// exercises matter; every other field is left zero.
func sortTestSEV(id string, createdAt time.Time) *store.SEV {
	return &store.SEV{ID: id, CreatedAt: createdAt}
}

func TestSortSEVs_DefaultField_DescendingByCreatedAtRegardlessOfDesc(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := sortTestSEV("a", base)
	b := sortTestSEV("b", base.AddDate(0, 0, 1))
	c := sortTestSEV("c", base.AddDate(0, 0, 2))

	for _, desc := range []bool{false, true} {
		records := []*store.SEV{a, b, c}
		// SortSEVs' empty-field branch always orders most-recently-created
		// first, ignoring desc entirely — this locks in that documented
		// behavior (see sort.go's doc comment) for both desc values.
		store.SortSEVs(records, "", desc)
		if records[0].ID != "c" || records[1].ID != "b" || records[2].ID != "a" {
			t.Fatalf("desc=%v: want [c b a], got %v", desc, ids(records))
		}
	}
}

func TestSortSEVs_DefaultField_TiesBrokenByIDAscending(t *testing.T) {
	same := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := sortTestSEV("b", same)
	a := sortTestSEV("a", same)
	records := []*store.SEV{b, a}
	store.SortSEVs(records, "", false)
	if records[0].ID != "a" || records[1].ID != "b" {
		t.Fatalf("want [a b] (id ascending on a CreatedAt tie), got %v", ids(records))
	}
}

func TestSortSEVs_Severity(t *testing.T) {
	low := &store.SEV{ID: "low", SeverityLevel: 4}
	high := &store.SEV{ID: "high", SeverityLevel: 1}

	t.Run("Ascending", func(t *testing.T) {
		records := []*store.SEV{low, high}
		store.SortSEVs(records, store.SEVSortSeverity, false)
		if records[0].ID != "high" || records[1].ID != "low" {
			t.Fatalf("want [high low] (1 before 4), got %v", ids(records))
		}
	})

	t.Run("Descending", func(t *testing.T) {
		records := []*store.SEV{high, low}
		store.SortSEVs(records, store.SEVSortSeverity, true)
		if records[0].ID != "low" || records[1].ID != "high" {
			t.Fatalf("want [low high] (4 before 1), got %v", ids(records))
		}
	})
}

func TestSortSEVs_UpdatedAt(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := &store.SEV{ID: "older", UpdatedAt: base}
	newer := &store.SEV{ID: "newer", UpdatedAt: base.Add(time.Hour)}

	records := []*store.SEV{newer, older}
	store.SortSEVs(records, store.SEVSortUpdatedAt, false)
	if records[0].ID != "older" || records[1].ID != "newer" {
		t.Fatalf("want [older newer] ascending, got %v", ids(records))
	}
}

func TestSortSEVs_StartedAt_NilAlwaysSortsLast(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	withDate := &store.SEV{ID: "dated", StartedAt: &base}
	noDate := &store.SEV{ID: "undated"}

	for _, desc := range []bool{false, true} {
		records := []*store.SEV{noDate, withDate}
		store.SortSEVs(records, store.SEVSortStartedAt, desc)
		if records[len(records)-1].ID != "undated" {
			t.Fatalf("desc=%v: want the nil-StartedAt record last, got %v", desc, ids(records))
		}
	}
}

func TestSortSEVs_StartedAt_BothSet(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	earlier := &store.SEV{ID: "earlier", StartedAt: &base}
	laterTime := base.AddDate(0, 0, 1)
	later := &store.SEV{ID: "later", StartedAt: &laterTime}

	records := []*store.SEV{later, earlier}
	store.SortSEVs(records, store.SEVSortStartedAt, false)
	if records[0].ID != "earlier" || records[1].ID != "later" {
		t.Fatalf("want [earlier later] ascending, got %v", ids(records))
	}
}

func TestSortSEVs_BothMissingExplicitField_TiedBrokenByID(t *testing.T) {
	b := &store.SEV{ID: "b"} // no StartedAt on either record
	a := &store.SEV{ID: "a"}

	records := []*store.SEV{b, a}
	store.SortSEVs(records, store.SEVSortStartedAt, true)
	if records[0].ID != "a" || records[1].ID != "b" {
		t.Fatalf("want [a b] (id ascending when both are missing the sort field), got %v", ids(records))
	}
}

func TestSortSEVs_UnknownField_FallsBackToCreatedAt(t *testing.T) {
	// SortSEVs/sortKeyMissing/sortLess's switches all have a default case
	// that isn't reachable through any of the SEVSortField constants
	// SortSEVs is normally called with — this exercises it directly via an
	// unrecognized field value, documenting that fallback as CreatedAt
	// ordering rather than a panic or a no-op.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := &store.SEV{ID: "older", CreatedAt: base}
	newer := &store.SEV{ID: "newer", CreatedAt: base.Add(time.Hour)}

	records := []*store.SEV{newer, older}
	store.SortSEVs(records, store.SEVSortField("bogus"), false)
	if records[0].ID != "older" || records[1].ID != "newer" {
		t.Fatalf("want [older newer] (CreatedAt ascending fallback), got %v", ids(records))
	}
}

func TestSortSEVs_MTTR_NilAlwaysSortsLast(t *testing.T) {
	fast := int64(60)
	withMTTR := &store.SEV{ID: "measured", MTTRSeconds: &fast}
	noMTTR := &store.SEV{ID: "unmeasured"}

	for _, desc := range []bool{false, true} {
		records := []*store.SEV{noMTTR, withMTTR}
		store.SortSEVs(records, store.SEVSortMTTR, desc)
		if records[len(records)-1].ID != "unmeasured" {
			t.Fatalf("desc=%v: want the nil-MTTR record last, got %v", desc, ids(records))
		}
	}
}

func TestSortSEVs_MTTR_BothSet(t *testing.T) {
	fast, slow := int64(60), int64(3600)
	quicker := &store.SEV{ID: "quicker", MTTRSeconds: &fast}
	slower := &store.SEV{ID: "slower", MTTRSeconds: &slow}

	records := []*store.SEV{slower, quicker}
	store.SortSEVs(records, store.SEVSortMTTR, false)
	if records[0].ID != "quicker" || records[1].ID != "slower" {
		t.Fatalf("want [quicker slower] ascending, got %v", ids(records))
	}
}

func TestSortSEVs_TiedOnExplicitField_BrokenByIDAscendingRegardlessOfDirection(t *testing.T) {
	b := &store.SEV{ID: "b", SeverityLevel: 2}
	a := &store.SEV{ID: "a", SeverityLevel: 2}

	for _, desc := range []bool{false, true} {
		records := []*store.SEV{b, a}
		store.SortSEVs(records, store.SEVSortSeverity, desc)
		if records[0].ID != "a" || records[1].ID != "b" {
			t.Fatalf("desc=%v: want [a b] (id ascending on a severity tie), got %v", desc, ids(records))
		}
	}
}

func ids(records []*store.SEV) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.ID
	}
	return out
}
