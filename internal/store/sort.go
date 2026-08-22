package store

import "sort"

// SortSEVs orders records in place by field, using CreatedAt (then ID) as the
// legacy default when field is empty. Missing values for the chosen field
// (StartedAt, MTTRSeconds) always sort last regardless of direction, with ID
// used as a final, deterministic tie-breaker. Shared by the memory SEVStore
// (in-process ordering) and the SearchService handler (merging results
// pulled from more than one store).
func SortSEVs(records []*SEV, field SEVSortField, desc bool) {
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if field == "" {
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		aMissing, bMissing := sortKeyMissing(a, field), sortKeyMissing(b, field)
		if aMissing != bMissing {
			return !aMissing
		}
		if aMissing {
			return a.ID < b.ID
		}
		less := sortLess(a, b, field)
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
