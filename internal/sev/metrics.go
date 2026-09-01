package sev

import (
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// ComputeMetrics recomputes all derived time metrics on sev in place.
// Only metrics whose inputs are non-nil are updated; others are left unchanged.
func ComputeMetrics(sev *store.SEV) {
	if sev.StartedAt != nil && sev.DetectedAt != nil {
		v := int64(sev.DetectedAt.Sub(*sev.StartedAt) / time.Second)
		sev.MTTDSeconds = &v
	}
	if sev.StartedAt != nil && sev.MitigatedAt != nil {
		v := int64(sev.MitigatedAt.Sub(*sev.StartedAt) / time.Second)
		sev.MTTMSeconds = &v
	}
	if sev.StartedAt != nil && sev.ResolvedAt != nil {
		v := int64(sev.ResolvedAt.Sub(*sev.StartedAt) / time.Second)
		sev.MTTRSeconds = &v
	}
	if sev.DetectedAt != nil && sev.MitigatedAt != nil {
		v := int64(sev.MitigatedAt.Sub(*sev.DetectedAt) / time.Second)
		sev.DTTMSeconds = &v
	}
	if sev.ResolvedAt != nil && sev.PostmortemCompletedAt != nil {
		v := int64(sev.PostmortemCompletedAt.Sub(*sev.ResolvedAt) / time.Second)
		sev.RTPCSeconds = &v
	}
}
