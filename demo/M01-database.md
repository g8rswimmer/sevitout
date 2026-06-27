# M01 — Database Schema & Store Interfaces

## What was built

All PostgreSQL tables for the Sevitout V1 data model are created via migration `000002_schema`. `sqlc` generates type-safe Go query code in `internal/store/queries/`. Repository interfaces are defined in `internal/store/` and backed by thread-safe in-memory implementations in `internal/store/memory/` for use in unit tests throughout the project.

## Prerequisites

- M00 complete (Docker Compose stack, `make up`/`migrate` targets working)
- Docker and Docker Compose available
- `.env` populated from `.env.example` (`cp .env.example .env` then fill in `POSTGRES_*` values)
- `sqlc` installed: `brew install sqlc`

## Start the stack

```bash
make up          # starts postgres in the background; press Ctrl-C or run make down to stop
```

In a second terminal:

```bash
make migrate     # applies migrations 000001 (baseline) and 000002 (full schema)
```

## Walkthrough

### 1 — Verify the schema was applied

```bash
make psql
```

Inside psql:

```sql
-- List all application tables
\dt

-- Expected output includes:
--  ai_outputs, ai_plugins, audit_log, integration_config, notification_config,
--  oncall_rotations, postmortems, retention_config, sev_announcements,
--  sev_chat_log, sev_linked_tasks, sev_links, sev_roles, sev_slis,
--  sev_status_history, sevs, shareable_links, services, users
```

### 2 — Inspect the search_vector trigger

```sql
-- Insert a test SEV and confirm the search_vector is populated automatically
INSERT INTO sevs (id, title, description, severity_level, status, created_by)
VALUES ('SEV-TEST-0001', 'API latency spike', 'P99 exceeded 5 s', 1, 'open', 'demo');

SELECT id, search_vector FROM sevs WHERE id = 'SEV-TEST-0001';

-- Cleanup
DELETE FROM sevs WHERE id = 'SEV-TEST-0001';
```

### 3 — Confirm the audit_writer role (INSERT-only restriction)

```sql
-- Confirm role exists and has only INSERT on audit_log
SELECT grantee, privilege_type
FROM information_schema.role_table_grants
WHERE table_name = 'audit_log' AND grantee = 'audit_writer';

-- Expected: one row with privilege_type = 'INSERT'
```

### 4 — Check the SEV number sequence

```sql
SELECT nextval('sev_number_seq');
-- Returns the next value to be used when generating SEV-YYYY-NNNN IDs
SELECT setval('sev_number_seq', 1, false); -- reset for demo purposes (do not do this in production)
```

### 5 — Confirm default retention config

```sql
SELECT severity_level, retention_days, hard_delete FROM retention_config ORDER BY severity_level;

-- Expected:
--  severity_level | retention_days | hard_delete
-- ----------------+----------------+-------------
--               1 |              0 | f
--               2 |              0 | f
--               3 |              0 | f
--               4 |              0 | f
```

Exit psql with `\q`.

### 6 — Roll back and re-apply (verify idempotency)

```bash
make migrate-down   # rolls back migration 000002
make migrate        # re-applies 000002
```

## Verify tests pass

```bash
# Unit tests (in-memory store compliance)
go test ./internal/store/...

# All project tests
go test ./...

# Linter
golangci-lint run

# Integration test against a live DB (requires make up && make migrate first)
go test -tags integration -v -run TestAuditWriterRole ./internal/store/
```

Expected output for unit tests:
```
ok      github.com/g8rswimmer/sevitout/internal/store/memory
```

## Known limitations

- The `users` table exists but is empty until M03 (OAuth login) populates it.
- The PostgreSQL store implementations (`internal/store/postgres/`) are not yet written; the interfaces are backed only by the in-memory fakes at this milestone.
- Full-text search filtering via `search_vector` is available in the DB but not yet exposed through the store interfaces; it will be wired in M08.
- The `notification_config` and `retention_config` tables have no store interfaces yet; they will be added in M10.
