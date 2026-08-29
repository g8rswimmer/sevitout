package telemetry_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// Every test below uses a method/outcome/severity label value unique to
// that test, since these are process-wide Prometheus metrics — other tests
// in this same package's test binary incrementing the same label
// combination would make an absolute-value assertion flaky depending on
// test order. internal/api/grpc, internal/ai, internal/api/ws, and
// cmd/server each run in their own separate test binary, so this isolation
// concern is local to this file.

func TestRPCRequestsTotal_IncrementsForLabelCombination(t *testing.T) {
	before := testutil.ToFloat64(telemetry.RPCRequestsTotal.WithLabelValues("/test.Metrics/RPCRequestsTotal", "OK"))
	telemetry.RPCRequestsTotal.WithLabelValues("/test.Metrics/RPCRequestsTotal", "OK").Inc()
	after := testutil.ToFloat64(telemetry.RPCRequestsTotal.WithLabelValues("/test.Metrics/RPCRequestsTotal", "OK"))

	if after != before+1 {
		t.Errorf("RPCRequestsTotal after Inc() = %v, want %v", after, before+1)
	}
}

func TestRPCDurationSeconds_RecordsObservation(t *testing.T) {
	// A histogram's exposed "value" via CollectAndCount is its sample count,
	// not the observed value itself — Observe() should still increase it by
	// exactly one per call.
	before := testutil.CollectAndCount(telemetry.RPCDurationSeconds)
	telemetry.RPCDurationSeconds.WithLabelValues("/test.Metrics/RPCDurationSeconds", "OK").Observe(0.05)
	after := testutil.CollectAndCount(telemetry.RPCDurationSeconds)

	if after != before+1 {
		t.Errorf("RPCDurationSeconds sample count after Observe() = %d, want %d", after, before+1)
	}
}

func TestWSConnections_IncDec(t *testing.T) {
	before := testutil.ToFloat64(telemetry.WSConnections)
	telemetry.WSConnections.Inc()
	if got := testutil.ToFloat64(telemetry.WSConnections); got != before+1 {
		t.Errorf("WSConnections after Inc() = %v, want %v", got, before+1)
	}
	telemetry.WSConnections.Dec()
	if got := testutil.ToFloat64(telemetry.WSConnections); got != before {
		t.Errorf("WSConnections after Inc()+Dec() = %v, want %v (back to baseline)", got, before)
	}
}

func TestAIActionsTotal_IncrementsPerOutcome(t *testing.T) {
	for _, outcome := range []string{"success", "error", "skipped"} {
		before := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues(outcome))
		telemetry.AIActionsTotal.WithLabelValues(outcome).Inc()
		after := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues(outcome))
		if after != before+1 {
			t.Errorf("AIActionsTotal[%s] after Inc() = %v, want %v", outcome, after, before+1)
		}
	}
}

func TestOpenSEVs_SetPerSeverity(t *testing.T) {
	telemetry.OpenSEVs.WithLabelValues("99").Set(3)
	if got := testutil.ToFloat64(telemetry.OpenSEVs.WithLabelValues("99")); got != 3 {
		t.Errorf("OpenSEVs[99] = %v, want 3", got)
	}
	telemetry.OpenSEVs.WithLabelValues("99").Set(0)
	if got := testutil.ToFloat64(telemetry.OpenSEVs.WithLabelValues("99")); got != 0 {
		t.Errorf("OpenSEVs[99] after resetting to 0 = %v, want 0", got)
	}
}

func TestDBPoolGauges_Set(t *testing.T) {
	telemetry.DBPoolIdleConns.Set(2)
	telemetry.DBPoolUsedConns.Set(1)
	telemetry.DBPoolMaxConns.Set(10)

	if got := testutil.ToFloat64(telemetry.DBPoolIdleConns); got != 2 {
		t.Errorf("DBPoolIdleConns = %v, want 2", got)
	}
	if got := testutil.ToFloat64(telemetry.DBPoolUsedConns); got != 1 {
		t.Errorf("DBPoolUsedConns = %v, want 1", got)
	}
	if got := testutil.ToFloat64(telemetry.DBPoolMaxConns); got != 10 {
		t.Errorf("DBPoolMaxConns = %v, want 10", got)
	}
}
