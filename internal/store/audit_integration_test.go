//go:build integration

package store_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestAuditWriterRole verifies that the audit_writer PostgreSQL role has only
// INSERT privileges on audit_log — no UPDATE or DELETE.
//
// Run with: go test -tags integration -run TestAuditWriterRole ./internal/store/
// Requires: DATABASE_URL set and the M01 migration applied (make migrate).
func TestAuditWriterRole(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()

	// Connect as the superuser to set up the test fixture.
	superConn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect superuser: %v", err)
	}
	// Register the connection close first so it runs last (t.Cleanup is LIFO),
	// keeping superConn alive for all other cleanup callbacks below.
	t.Cleanup(func() { superConn.Close(context.Background()) })

	// Grant LOGIN to audit_writer so we can connect as it.
	// We set a temporary password and revoke LOGIN afterward in cleanup.
	const testPass = "audit_writer_test_pass_m01"
	_, err = superConn.Exec(ctx, fmt.Sprintf(
		"ALTER ROLE audit_writer LOGIN PASSWORD '%s'", testPass,
	))
	if err != nil {
		t.Fatalf("alter role audit_writer login: %v", err)
	}
	t.Cleanup(func() {
		if _, err := superConn.Exec(context.Background(), "ALTER ROLE audit_writer NOLOGIN"); err != nil {
			t.Logf("cleanup: alter role audit_writer nologin: %v", err)
		}
	})

	// Insert a test SEV so we have a valid sev_id for audit_log FK.
	sevID := fmt.Sprintf("SEV-%d-TEST", time.Now().Year())
	_, err = superConn.Exec(ctx,
		`INSERT INTO sevs (id, title, severity_level, status, created_by)
		 VALUES ($1, 'audit_writer integration test', 1, 'open', 'test')
		 ON CONFLICT (id) DO NOTHING`,
		sevID,
	)
	if err != nil {
		t.Fatalf("insert test sev: %v", err)
	}
	t.Cleanup(func() {
		if _, err := superConn.Exec(context.Background(), "DELETE FROM audit_log WHERE user_id = 'audit_writer_test'"); err != nil {
			t.Logf("cleanup: delete audit_log rows: %v", err)
		}
		if _, err := superConn.Exec(context.Background(), "DELETE FROM sevs WHERE id = $1", sevID); err != nil {
			t.Logf("cleanup: delete test sev: %v", err)
		}
	})

	// Build an audit_writer connection URL from the superuser URL.
	auditURL := replaceCredentials(dbURL, "audit_writer", testPass)
	auditConn, err := pgx.Connect(ctx, auditURL)
	if err != nil {
		t.Fatalf("connect as audit_writer: %v", err)
	}
	defer auditConn.Close(ctx)

	// INSERT should succeed. Deliberately not using RETURNING here: it
	// requires SELECT privilege on the table in addition to INSERT, which
	// audit_writer must not have (INSERT-only is the whole point of this
	// role) — granting it just to make RETURNING work would defeat the
	// test. Read the new row back over the superuser connection instead.
	_, err = auditConn.Exec(ctx,
		`INSERT INTO audit_log (sev_id, user_id, action) VALUES ($1, 'audit_writer_test', 'test')`,
		sevID,
	)
	if err != nil {
		t.Fatalf("audit_writer INSERT failed (want success): %v", err)
	}

	var auditID int64
	err = superConn.QueryRow(ctx,
		`SELECT id FROM audit_log WHERE sev_id = $1 AND user_id = 'audit_writer_test' ORDER BY id DESC LIMIT 1`,
		sevID,
	).Scan(&auditID)
	if err != nil {
		t.Fatalf("fetch inserted audit_log id: %v", err)
	}

	// UPDATE must be denied.
	_, err = auditConn.Exec(ctx,
		`UPDATE audit_log SET action = 'tampered' WHERE id = $1`, auditID,
	)
	if err == nil {
		t.Fatal("audit_writer UPDATE succeeded; expected permission denied")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error on UPDATE: %v", err)
	}

	// DELETE must be denied.
	_, err = auditConn.Exec(ctx,
		`DELETE FROM audit_log WHERE id = $1`, auditID,
	)
	if err == nil {
		t.Fatal("audit_writer DELETE succeeded; expected permission denied")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error on DELETE: %v", err)
	}
}

// replaceCredentials swaps user:password in a postgres:// URL.
func replaceCredentials(dsn, user, password string) string {
	// postgres://olduser:oldpass@host/db  →  postgres://newuser:newpass@host/db
	const prefix = "postgres://"
	if !strings.HasPrefix(dsn, prefix) {
		return dsn
	}
	rest := dsn[len(prefix):]
	at := strings.Index(rest, "@")
	if at < 0 {
		return dsn
	}
	return prefix + user + ":" + password + "@" + rest[at+1:]
}
