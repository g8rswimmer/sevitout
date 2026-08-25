//go:build integration

// Package postgres_test holds integration tests for internal/store/postgres
// that run against a real PostgreSQL instance. They're gated behind the
// "integration" build tag and skip individually if DATABASE_URL is unset,
// mirroring internal/store/audit_integration_test.go's gating so
// `go test ./...` (no tag) stays clean with no DB present.
//
// Run with: go test -tags integration -v ./internal/store/postgres/...
// Requires: DATABASE_URL set and all migrations applied (make migrate, or
// `migrate -path=./migrations -database "$DATABASE_URL" up`).
package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool returns a connection pool to the PostgreSQL instance
// configured by DATABASE_URL, or skips the calling test if it's unset. The
// pool is closed via t.Cleanup.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// truncateAll clears every application table (all tests in this package
// share one database, and there's no per-test transaction available — the
// store constructors take a *pgxpool.Pool, not a pgx.Tx). Call it via
// t.Cleanup at the start of each test so tests can run in any order without
// interfering with each other. CASCADE handles FK-dependent tables (e.g.
// sev_access → sevs) regardless of listed order; schema_migrations is
// deliberately excluded.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `TRUNCATE TABLE
		ai_outputs, ai_plugins, audit_log, integration_config,
		notification_config, oncall_rotations, postmortems, retention_config,
		services, sev_access, sev_announcements, sev_chat_log,
		sev_linked_tasks, sev_links, sev_roles, sev_slis, sev_status_history,
		sevs, shareable_links, users
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
