package main

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// erroringSEVStore wraps *memory.SEVStore, returning err from List instead
// of delegating — used to exercise refreshMetrics' error-logging path,
// which the in-memory store's own List method can't otherwise trigger (it
// never fails).
type erroringSEVStore struct {
	*memory.SEVStore
	err error
}

func (e *erroringSEVStore) List(_ context.Context, _ store.SEVFilter) ([]*store.SEV, error) {
	return nil, e.err
}

func mustCreateTestSEV(t *testing.T, sevs *memory.SEVStore, status store.SEVStatus, severity int16, sensitive bool) {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title: "test sev", Status: status, SeverityLevel: severity, Sensitive: sensitive,
		CreatedBy: "user-1", CreatedAt: now, UpdatedAt: now,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
}

func TestRefreshMetrics_OpenSEVsCountedBySeverityExcludingSensitiveAndClosed(t *testing.T) {
	sevs := memory.NewSEVStore()
	mustCreateTestSEV(t, sevs, store.SEVStatusOpen, 1, false)
	mustCreateTestSEV(t, sevs, store.SEVStatusInvestigating, 1, false)
	mustCreateTestSEV(t, sevs, store.SEVStatusMitigated, 2, false)
	mustCreateTestSEV(t, sevs, store.SEVStatusOpen, 1, true)                // sensitive: excluded
	mustCreateTestSEV(t, sevs, store.SEVStatusResolved, 3, false)           // resolved: not "open"
	mustCreateTestSEV(t, sevs, store.SEVStatusPostmortemComplete, 4, false) // closed: not "open"

	refreshMetrics(context.Background(), discardLogger(), sevs, nil)

	cases := map[int16]float64{1: 2, 2: 1, 3: 0, 4: 0}
	for severity, want := range cases {
		got := testutil.ToFloat64(telemetry.OpenSEVs.WithLabelValues(strconv.Itoa(int(severity))))
		if got != want {
			t.Errorf("OpenSEVs[severity=%d] = %v, want %v", severity, got, want)
		}
	}
}

func TestRefreshMetrics_NilPool_SkipsDBGaugesWithoutPanic(t *testing.T) {
	sevs := memory.NewSEVStore()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("refreshMetrics panicked with a nil pool: %v", r)
		}
	}()
	refreshMetrics(context.Background(), discardLogger(), sevs, nil)
}

func TestRefreshMetrics_ListError_LogsAndDoesNotPanic(t *testing.T) {
	boom := errors.New("db exploded")
	sevs := &erroringSEVStore{SEVStore: memory.NewSEVStore(), err: boom}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("refreshMetrics panicked on a List error: %v", r)
		}
	}()
	refreshMetrics(context.Background(), discardLogger(), sevs, nil)
}

func TestStartMetricsRefresher_StopsWhenContextCanceled(t *testing.T) {
	sevs := memory.NewSEVStore()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		startMetricsRefresher(ctx, discardLogger(), sevs, nil)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startMetricsRefresher did not return after its context was canceled")
	}
}
