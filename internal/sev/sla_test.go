package sev_test

import (
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

func int64p(v int64) *int64 { return &v }

func TestMostStrictSLA_TakesMinimumPerMetric(t *testing.T) {
	rows := []*store.ServiceSLA{
		{ServiceID: "checkout", MTTDTargetSeconds: int64p(600), MTTMTargetSeconds: int64p(1800), RTPCTargetSeconds: int64p(172800)},
		{ServiceID: "payments", MTTDTargetSeconds: int64p(300), MTTRTargetSeconds: int64p(3600), RTPCTargetSeconds: int64p(86400)},
	}
	targets := sev.MostStrictSLA(rows)

	if targets.MTTDTargetSeconds == nil || *targets.MTTDTargetSeconds != 300 {
		t.Fatalf("MTTDTargetSeconds = %v, want 300 (strictest of 600, 300)", targets.MTTDTargetSeconds)
	}
	if targets.MTTMTargetSeconds == nil || *targets.MTTMTargetSeconds != 1800 {
		t.Fatalf("MTTMTargetSeconds = %v, want 1800 (only checkout configures it)", targets.MTTMTargetSeconds)
	}
	if targets.MTTRTargetSeconds == nil || *targets.MTTRTargetSeconds != 3600 {
		t.Fatalf("MTTRTargetSeconds = %v, want 3600 (only payments configures it)", targets.MTTRTargetSeconds)
	}
	if targets.RTPCTargetSeconds == nil || *targets.RTPCTargetSeconds != 86400 {
		t.Fatalf("RTPCTargetSeconds = %v, want 86400 (strictest of 172800, 86400)", targets.RTPCTargetSeconds)
	}
}

func TestMostStrictSLA_NoRows(t *testing.T) {
	targets := sev.MostStrictSLA(nil)
	if targets.MTTDTargetSeconds != nil || targets.MTTMTargetSeconds != nil || targets.MTTRTargetSeconds != nil ||
		targets.RTPCTargetSeconds != nil {
		t.Fatalf("expected all-nil targets with no rows, got %+v", targets)
	}
}

var slaStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestEvaluateSLA_NotApplicableWithoutTarget(t *testing.T) {
	s := &store.SEV{StartedAt: &slaStart}
	eval := sev.EvaluateSLA(s, sev.SLATargets{}, slaStart.Add(time.Hour))

	if eval.MTTD != sev.SLANotApplicable || eval.Overall != sev.SLANotApplicable {
		t.Fatalf("got %+v, want all not_applicable with no targets configured", eval)
	}
}

func TestEvaluateSLA_NotApplicableWithoutBaseline(t *testing.T) {
	s := &store.SEV{} // no StartedAt yet
	targets := sev.SLATargets{MTTDTargetSeconds: int64p(300)}
	eval := sev.EvaluateSLA(s, targets, slaStart)

	if eval.MTTD != sev.SLANotApplicable {
		t.Fatalf("MTTD = %v, want not_applicable with no baseline timestamp", eval.MTTD)
	}
}

func TestEvaluateSLA_AtRiskStillInProgress(t *testing.T) {
	s := &store.SEV{StartedAt: &slaStart} // DetectedAt not set yet
	targets := sev.SLATargets{MTTDTargetSeconds: int64p(300)}
	now := slaStart.Add(10 * time.Minute) // 600s elapsed > 300s target

	eval := sev.EvaluateSLA(s, targets, now)
	if eval.MTTD != sev.SLAAtRisk {
		t.Fatalf("MTTD = %v, want at_risk (elapsed 600s > target 300s, not yet detected)", eval.MTTD)
	}
	if eval.Overall != sev.SLAAtRisk {
		t.Fatalf("Overall = %v, want at_risk", eval.Overall)
	}
}

func TestEvaluateSLA_OnTrackStillInProgress(t *testing.T) {
	s := &store.SEV{StartedAt: &slaStart}
	targets := sev.SLATargets{MTTDTargetSeconds: int64p(600)}
	now := slaStart.Add(2 * time.Minute) // 120s elapsed < 600s target

	eval := sev.EvaluateSLA(s, targets, now)
	if eval.MTTD != sev.SLAOnTrack {
		t.Fatalf("MTTD = %v, want ok (elapsed 120s < target 600s)", eval.MTTD)
	}
}

func TestEvaluateSLA_BreachedOnceFinalized(t *testing.T) {
	detected := slaStart.Add(10 * time.Minute) // 600s, over a 300s target
	s := &store.SEV{StartedAt: &slaStart, DetectedAt: &detected, MTTDSeconds: int64p(600)}
	targets := sev.SLATargets{MTTDTargetSeconds: int64p(300)}

	// now is irrelevant once the final value is recorded — no more live
	// elapsed-time comparison for this metric.
	eval := sev.EvaluateSLA(s, targets, detected.Add(time.Hour))
	if eval.MTTD != sev.SLABreached {
		t.Fatalf("MTTD = %v, want breached (final 600s > target 300s)", eval.MTTD)
	}
	if eval.Overall != sev.SLABreached {
		t.Fatalf("Overall = %v, want breached", eval.Overall)
	}
}

func TestEvaluateSLA_OnTrackOnceFinalizedUnderTarget(t *testing.T) {
	detected := slaStart.Add(2 * time.Minute) // 120s, under a 300s target
	s := &store.SEV{StartedAt: &slaStart, DetectedAt: &detected, MTTDSeconds: int64p(120)}
	targets := sev.SLATargets{MTTDTargetSeconds: int64p(300)}

	eval := sev.EvaluateSLA(s, targets, detected.Add(time.Hour))
	if eval.MTTD != sev.SLAOnTrack {
		t.Fatalf("MTTD = %v, want ok (final 120s <= target 300s)", eval.MTTD)
	}
}

func TestEvaluateSLA_OverallIsWorstOfThree(t *testing.T) {
	s := &store.SEV{StartedAt: &slaStart}
	targets := sev.SLATargets{
		MTTDTargetSeconds: int64p(600), // on track: 300s elapsed < 600s
		MTTMTargetSeconds: int64p(60),  // at risk: 300s elapsed > 60s
		MTTRTargetSeconds: nil,         // not applicable: no target
	}
	now := slaStart.Add(5 * time.Minute) // 300s elapsed

	eval := sev.EvaluateSLA(s, targets, now)
	if eval.MTTD != sev.SLAOnTrack {
		t.Fatalf("MTTD = %v, want ok", eval.MTTD)
	}
	if eval.MTTM != sev.SLAAtRisk {
		t.Fatalf("MTTM = %v, want at_risk", eval.MTTM)
	}
	if eval.MTTR != sev.SLANotApplicable {
		t.Fatalf("MTTR = %v, want not_applicable", eval.MTTR)
	}
	if eval.Overall != sev.SLAAtRisk {
		t.Fatalf("Overall = %v, want at_risk (worst of ok, at_risk, not_applicable)", eval.Overall)
	}
}

func TestEvaluateSLA_RTPCMeasuredFromResolvedAtNotStartedAt(t *testing.T) {
	// StartedAt is 2 hours before ResolvedAt — if RTPC were (incorrectly)
	// measured from StartedAt like MTTD/MTTM/MTTR, this would already be
	// breached against a 1-hour target. It isn't: RTPC's baseline is
	// ResolvedAt, and only 10 minutes have elapsed since then.
	resolved := slaStart.Add(2 * time.Hour)
	s := &store.SEV{StartedAt: &slaStart, ResolvedAt: &resolved}
	targets := sev.SLATargets{RTPCTargetSeconds: int64p(3600)} // 1 hour
	now := resolved.Add(10 * time.Minute)

	eval := sev.EvaluateSLA(s, targets, now)
	if eval.RTPC != sev.SLAOnTrack {
		t.Fatalf("RTPC = %v, want ok (10m elapsed since ResolvedAt < 1h target)", eval.RTPC)
	}
}

func TestEvaluateSLA_RTPCNotApplicableBeforeResolution(t *testing.T) {
	s := &store.SEV{StartedAt: &slaStart} // not yet resolved
	targets := sev.SLATargets{RTPCTargetSeconds: int64p(3600)}

	eval := sev.EvaluateSLA(s, targets, slaStart.Add(2*time.Hour))
	if eval.RTPC != sev.SLANotApplicable {
		t.Fatalf("RTPC = %v, want not_applicable (no ResolvedAt baseline yet)", eval.RTPC)
	}
}

func TestEvaluateSLA_RTPCBreachedOncePostmortemCompletedLate(t *testing.T) {
	resolved := slaStart.Add(time.Hour)
	postmortemComplete := resolved.Add(48 * time.Hour) // over a 24h target
	s := &store.SEV{
		StartedAt: &slaStart, ResolvedAt: &resolved,
		PostmortemCompletedAt: &postmortemComplete, RTPCSeconds: int64p(48 * 3600),
	}
	targets := sev.SLATargets{RTPCTargetSeconds: int64p(24 * 3600)}

	eval := sev.EvaluateSLA(s, targets, postmortemComplete.Add(time.Hour))
	if eval.RTPC != sev.SLABreached {
		t.Fatalf("RTPC = %v, want breached (final 48h > 24h target)", eval.RTPC)
	}
	if eval.Overall != sev.SLABreached {
		t.Fatalf("Overall = %v, want breached", eval.Overall)
	}
}
