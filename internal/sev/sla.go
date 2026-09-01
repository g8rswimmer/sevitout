package sev

import (
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// SLAMetricStatus is the live breach status of one metric against its SLA
// target — docs/roadmap.md Phase 12. Derived on every read from `now`
// against the target, the same discipline internal/api/grpc/task.go's
// isOverdue already applies to task due dates ("never trusted from
// storage"), extended here to a SEV that hasn't hit its terminal timestamp
// yet.
type SLAMetricStatus string

const (
	// SLANotApplicable means no target is configured for this metric (no
	// attached service has one at this SEV's severity level), or the metric
	// has no baseline timestamp yet to measure elapsed time from.
	SLANotApplicable SLAMetricStatus = "not_applicable"
	// SLAOnTrack means the metric is within its target — either already
	// finalized under target, or still running with elapsed time under target.
	SLAOnTrack SLAMetricStatus = "ok"
	// SLAAtRisk means the metric hasn't finalized yet, but elapsed time since
	// its baseline already exceeds the target.
	SLAAtRisk SLAMetricStatus = "at_risk"
	// SLABreached means the metric's final value exceeded the target.
	SLABreached SLAMetricStatus = "breached"
)

// slaSeverity ranks each status so Overall can take the worst of several —
// higher is worse, matching the visual escalation a caller should show.
var slaSeverity = map[SLAMetricStatus]int{
	SLANotApplicable: 0,
	SLAOnTrack:       1,
	SLAAtRisk:        2,
	SLABreached:      3,
}

// SLATargets is the effective SLA for one SEV, already reduced across every
// attached service via MostStrictSLA. A nil field means no target applies
// to that metric.
type SLATargets struct {
	MTTDTargetSeconds *int64
	MTTMTargetSeconds *int64
	MTTRTargetSeconds *int64
	// MTTPCTargetSeconds targets MTTPCSeconds (Mitigation to Postmortem
	// Complete) — see store.SEV.MTTPCSeconds' doc comment.
	MTTPCTargetSeconds *int64
}

// MostStrictSLA reduces every attached service's SLA row at a SEV's
// severity level to one effective target per metric, taking the minimum
// (strictest) non-nil value per metric — "if a SEV has multiple services,
// the most strict SLAs should be used." A service with no row for that
// severity level (or a row with that particular metric's target unset)
// simply doesn't participate in that metric's reduction.
func MostStrictSLA(rows []*store.ServiceSLA) SLATargets {
	var targets SLATargets
	for _, row := range rows {
		targets.MTTDTargetSeconds = stricter(targets.MTTDTargetSeconds, row.MTTDTargetSeconds)
		targets.MTTMTargetSeconds = stricter(targets.MTTMTargetSeconds, row.MTTMTargetSeconds)
		targets.MTTRTargetSeconds = stricter(targets.MTTRTargetSeconds, row.MTTRTargetSeconds)
		targets.MTTPCTargetSeconds = stricter(targets.MTTPCTargetSeconds, row.MTTPCTargetSeconds)
	}
	return targets
}

// stricter returns whichever of current/candidate is smaller, treating nil
// as "no constraint yet" rather than as zero.
func stricter(current, candidate *int64) *int64 {
	if candidate == nil {
		return current
	}
	if current == nil || *candidate < *current {
		v := *candidate
		return &v
	}
	return current
}

// SLAEvaluation is the live per-metric breach status for one SEV, plus the
// worst of the four as Overall.
type SLAEvaluation struct {
	MTTD, MTTM, MTTR, MTTPC SLAMetricStatus
	Overall                 SLAMetricStatus
}

// EvaluateSLA derives s's live SLA status against targets as of now. Mirrors
// ComputeMetrics' own nil-safety: a metric with no baseline timestamp yet is
// SLANotApplicable, not an error. MTTD/MTTM/MTTR are measured from
// StartedAt; MTTPC is measured from MitigatedAt instead, matching
// MTTPCSeconds' own "point A to point B" shape (see its doc comment) rather
// than "from incident start."
func EvaluateSLA(s *store.SEV, targets SLATargets, now time.Time) SLAEvaluation {
	eval := SLAEvaluation{
		MTTD:  evalMetric(s.StartedAt, s.MTTDSeconds, targets.MTTDTargetSeconds, now),
		MTTM:  evalMetric(s.StartedAt, s.MTTMSeconds, targets.MTTMTargetSeconds, now),
		MTTR:  evalMetric(s.StartedAt, s.MTTRSeconds, targets.MTTRTargetSeconds, now),
		MTTPC: evalMetric(s.MitigatedAt, s.MTTPCSeconds, targets.MTTPCTargetSeconds, now),
	}
	eval.Overall = eval.MTTD
	if slaSeverity[eval.MTTM] > slaSeverity[eval.Overall] {
		eval.Overall = eval.MTTM
	}
	if slaSeverity[eval.MTTR] > slaSeverity[eval.Overall] {
		eval.Overall = eval.MTTR
	}
	if slaSeverity[eval.MTTPC] > slaSeverity[eval.Overall] {
		eval.Overall = eval.MTTPC
	}
	return eval
}

// evalMetric derives one metric's live status: baseline is the timestamp
// elapsed time is measured from (StartedAt, for all three headline
// metrics); actualSeconds is the already-computed final value (nil until
// the corresponding lifecycle timestamp lands); target is the resolved SLA
// target for this metric (nil = not configured).
func evalMetric(baseline *time.Time, actualSeconds, target *int64, now time.Time) SLAMetricStatus {
	if target == nil {
		return SLANotApplicable
	}
	if actualSeconds != nil {
		if *actualSeconds > *target {
			return SLABreached
		}
		return SLAOnTrack
	}
	if baseline == nil {
		return SLANotApplicable
	}
	elapsed := int64(now.Sub(*baseline) / time.Second)
	if elapsed > *target {
		return SLAAtRisk
	}
	return SLAOnTrack
}
