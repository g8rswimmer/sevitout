package sev_test

import (
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// Fixed timestamps used by all metrics tests.
//
//	metricsStart              → T+0
//	metricsDetected           → T+5m  (MTTD = 300s)
//	metricsMitigated          → T+30m (MTTM = 1800s; DTTM = 1500s from detected)
//	metricsResolved           → T+60m (MTTR = 3600s)
//	metricsPostmortemComplete → T+90m (MTTPC = 3600s from mitigated)
var (
	metricsStart              = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	metricsDetected           = metricsStart.Add(5 * time.Minute)
	metricsMitigated          = metricsStart.Add(30 * time.Minute)
	metricsResolved           = metricsStart.Add(60 * time.Minute)
	metricsPostmortemComplete = metricsStart.Add(90 * time.Minute)
)

func TestComputeMetrics_AllTimestamps(t *testing.T) {
	s := &store.SEV{
		StartedAt:             &metricsStart,
		DetectedAt:            &metricsDetected,
		MitigatedAt:           &metricsMitigated,
		ResolvedAt:            &metricsResolved,
		PostmortemCompletedAt: &metricsPostmortemComplete,
	}
	sev.ComputeMetrics(s)

	checkMetric(t, "MTTDSeconds", s.MTTDSeconds, 300)
	checkMetric(t, "MTTMSeconds", s.MTTMSeconds, 1800)
	checkMetric(t, "MTTRSeconds", s.MTTRSeconds, 3600)
	checkMetric(t, "DTTMSeconds", s.DTTMSeconds, 1500)
	checkMetric(t, "MTTPCSeconds", s.MTTPCSeconds, 3600)
}

func TestComputeMetrics_MTTDOnly(t *testing.T) {
	s := &store.SEV{
		StartedAt:  &metricsStart,
		DetectedAt: &metricsDetected,
		// MitigatedAt and ResolvedAt intentionally nil
	}
	sev.ComputeMetrics(s)

	checkMetric(t, "MTTDSeconds", s.MTTDSeconds, 300)
	checkNilMetric(t, "MTTMSeconds", s.MTTMSeconds)
	checkNilMetric(t, "MTTRSeconds", s.MTTRSeconds)
	checkNilMetric(t, "DTTMSeconds", s.DTTMSeconds)
}

func TestComputeMetrics_MTTMOnly(t *testing.T) {
	s := &store.SEV{
		StartedAt:   &metricsStart,
		MitigatedAt: &metricsMitigated,
		// DetectedAt and ResolvedAt intentionally nil
	}
	sev.ComputeMetrics(s)

	checkNilMetric(t, "MTTDSeconds", s.MTTDSeconds)
	checkMetric(t, "MTTMSeconds", s.MTTMSeconds, 1800)
	checkNilMetric(t, "MTTRSeconds", s.MTTRSeconds)
	checkNilMetric(t, "DTTMSeconds", s.DTTMSeconds)
}

func TestComputeMetrics_MTTROnly(t *testing.T) {
	s := &store.SEV{
		StartedAt:  &metricsStart,
		ResolvedAt: &metricsResolved,
		// DetectedAt and MitigatedAt intentionally nil
	}
	sev.ComputeMetrics(s)

	checkNilMetric(t, "MTTDSeconds", s.MTTDSeconds)
	checkNilMetric(t, "MTTMSeconds", s.MTTMSeconds)
	checkMetric(t, "MTTRSeconds", s.MTTRSeconds, 3600)
	checkNilMetric(t, "DTTMSeconds", s.DTTMSeconds)
}

func TestComputeMetrics_DTTMOnly(t *testing.T) {
	s := &store.SEV{
		DetectedAt:  &metricsDetected,
		MitigatedAt: &metricsMitigated,
		// StartedAt and ResolvedAt intentionally nil
	}
	sev.ComputeMetrics(s)

	checkNilMetric(t, "MTTDSeconds", s.MTTDSeconds)
	checkNilMetric(t, "MTTMSeconds", s.MTTMSeconds)
	checkNilMetric(t, "MTTRSeconds", s.MTTRSeconds)
	checkMetric(t, "DTTMSeconds", s.DTTMSeconds, 1500)
}

func TestComputeMetrics_MTTPCOnly(t *testing.T) {
	s := &store.SEV{
		MitigatedAt:           &metricsMitigated,
		PostmortemCompletedAt: &metricsPostmortemComplete,
		// StartedAt, DetectedAt, and ResolvedAt intentionally nil
	}
	sev.ComputeMetrics(s)

	checkNilMetric(t, "MTTDSeconds", s.MTTDSeconds)
	checkNilMetric(t, "MTTMSeconds", s.MTTMSeconds)
	checkNilMetric(t, "MTTRSeconds", s.MTTRSeconds)
	checkNilMetric(t, "DTTMSeconds", s.DTTMSeconds)
	checkMetric(t, "MTTPCSeconds", s.MTTPCSeconds, 3600)
}

func TestComputeMetrics_NoTimestamps(t *testing.T) {
	s := &store.SEV{}
	sev.ComputeMetrics(s)

	checkNilMetric(t, "MTTDSeconds", s.MTTDSeconds)
	checkNilMetric(t, "MTTMSeconds", s.MTTMSeconds)
	checkNilMetric(t, "MTTRSeconds", s.MTTRSeconds)
	checkNilMetric(t, "DTTMSeconds", s.DTTMSeconds)
	checkNilMetric(t, "MTTPCSeconds", s.MTTPCSeconds)
}

// TestComputeMetrics_PreexistingNotOverwrittenWhenInputNil verifies that a
// metric whose timestamp inputs are absent is left untouched by ComputeMetrics.
func TestComputeMetrics_PreexistingNotOverwrittenWhenInputNil(t *testing.T) {
	existing := int64(999)
	s := &store.SEV{
		MTTDSeconds: &existing,
		// No timestamps → the MTTD block will not execute
	}
	sev.ComputeMetrics(s)

	if s.MTTDSeconds == nil || *s.MTTDSeconds != 999 {
		t.Errorf("MTTDSeconds = %v, want 999 (must not be overwritten when inputs are nil)", s.MTTDSeconds)
	}
}

// checkMetric asserts that got is non-nil and equals want.
func checkMetric(t *testing.T, name string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %d", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}

// checkNilMetric asserts that got is nil.
func checkNilMetric(t *testing.T, name string, got *int64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %d, want nil", name, *got)
	}
}
