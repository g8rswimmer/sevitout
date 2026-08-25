package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildStores_EmptyDSN_ReturnsMemoryStores(t *testing.T) {
	stores, err := buildStores(context.Background(), discardLogger(), "")
	if err != nil {
		t.Fatalf("buildStores: %v", err)
	}
	if stores == nil {
		t.Fatal("stores is nil")
	}
	if _, ok := stores.SEV.(*memory.SEVStore); !ok {
		t.Errorf("SEV = %T, want *memory.SEVStore", stores.SEV)
	}
	if _, ok := stores.SEVAccess.(*memory.SEVAccessStore); !ok {
		t.Errorf("SEVAccess = %T, want *memory.SEVAccessStore", stores.SEVAccess)
	}
}

func TestBuildStores_InvalidDSN_ReturnsError(t *testing.T) {
	// A syntactically invalid DSN fails at pgxpool.New's parse step, before
	// any network I/O — this exercises buildStores' error-return path
	// (rather than the os.Exit(1) main used to call directly, which isn't
	// something a test can safely trigger in-process) without needing a
	// real reachable Postgres.
	_, err := buildStores(context.Background(), discardLogger(), "not-a-valid-dsn")
	if err == nil {
		t.Fatal("want an error for an invalid DSN, got nil")
	}
}
