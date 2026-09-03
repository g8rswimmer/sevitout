package sev

import (
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// EvaluateEscalations is pure, table-testable domain logic (same shape as
// sla.go's EvaluateSLA) for cmd/server's escalation scanner
// (docs/requirements.md §16, docs/roadmap.md Phase 15): "alert if a SEV has
// been open longer than its severity level's configured threshold with no
// Incident Commander assigned."
//
// sevs is the candidate set — normally every SEV currently in an open status
// (Open/Investigating); hasIC reports, per SEV ID, whether an
// SEVRoleIncidentCommander is currently assigned; configs is keyed by
// severity level (0-4 rows; a missing or disabled entry means that severity
// level is never scanned). A SEV already marked EscalatedAt is skipped — the
// scanner notifies once per incident, not every scan interval — as is one
// with no StartedAt baseline yet to measure elapsed time from.
func EvaluateEscalations(sevs []*store.SEV, hasIC map[string]bool, configs map[int16]*store.EscalationConfig, now time.Time) []*store.SEV {
	var due []*store.SEV
	for _, s := range sevs {
		if s.EscalatedAt != nil || s.StartedAt == nil || hasIC[s.ID] {
			continue
		}
		cfg, ok := configs[s.SeverityLevel]
		if !ok || !cfg.Enabled {
			continue
		}
		threshold := time.Duration(cfg.ThresholdMinutes) * time.Minute
		if now.Sub(*s.StartedAt) > threshold {
			due = append(due, s)
		}
	}
	return due
}
