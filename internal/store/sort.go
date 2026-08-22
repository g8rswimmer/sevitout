package store

import "sort"

// SortSEVs orders records in place by field, defaulting to CreatedAt
// descending (most recently created first, then ID ascending) when field is
// empty — matching postgres's default ORDER BY so a request with no sort
// specified orders identically regardless of which internal path serves it
// (a single DB-side query vs. the SearchService handler merging results
// pulled from more than one store). Missing values for an explicit field
// (StartedAt, MTTRSeconds) always sort last regardless of direction, with ID
// used as a final, deterministic tie-breaker in every case, including ties
// on an explicit field.
func SortSEVs(records []*SEV, field SEVSortField, desc bool) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if field == "" {
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.After(b.CreatedAt)
		}
		aMissing, bMissing := sortKeyMissing(a, field), sortKeyMissing(b, field)
		if aMissing != bMissing {
			return !aMissing
		}
		if aMissing {
			return a.ID < b.ID
		}
		less := sortLess(a, b, field)
		if !less && !sortLess(b, a, field) {
			// Tied on field: fall back to id, always ascending (matching
			// postgres's unconditional ", id" tiebreak), regardless of desc.
			return a.ID < b.ID
		}
		if desc {
			return !less
		}
		return less
	})
}

func sortKeyMissing(s *SEV, field SEVSortField) bool {
	switch field {
	case SEVSortStartedAt:
		return s.StartedAt == nil
	case SEVSortMTTR:
		return s.MTTRSeconds == nil
	default:
		return false
	}
}

// sortLess must only be called once sortKeyMissing has ruled out nil values
// for both a and b on the given field.
func sortLess(a, b *SEV, field SEVSortField) bool {
	switch field {
	case SEVSortStartedAt:
		return a.StartedAt.Before(*b.StartedAt)
	case SEVSortSeverity:
		return a.SeverityLevel < b.SeverityLevel
	case SEVSortMTTR:
		return *a.MTTRSeconds < *b.MTTRSeconds
	case SEVSortUpdatedAt:
		return a.UpdatedAt.Before(b.UpdatedAt)
	default:
		return a.CreatedAt.Before(b.CreatedAt)
	}
}
