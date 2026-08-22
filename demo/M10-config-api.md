# M10 Demo — Configuration API

## What was built

`ConfigService`, the admin configuration surface described in `docs/requirements.md`
§18:

- **Service registry** — CRUD for the lightweight internal service registry
  (`/v1/config/services`). Deactivating a service (`active=false` via `UpdateService`)
  removes it from active-only listings while preserving the record for historical SEV
  references; `DeleteService` is a true hard delete.
- **User management** — list/search the user directory, change a user's organization
  role, and deactivate/reactivate a user without losing historical attribution
  (`/v1/config/users`).
- **On-call configuration** — CRUD for on-call rotations, including manual overrides
  with a time window (`/v1/config/oncall`).
- **Integration configuration** — per-integration settings and credentials
  (`/v1/config/integrations`). Credentials are sealed with **AES-256-GCM**
  (`internal/store/crypto`) before they ever reach the store; no RPC in this service
  returns decrypted credentials, only whether they're configured.
- **Data retention** — per-severity-level retention policy (`/v1/config/retention`);
  `retention_days == 0` means retain forever (the default for every level).
- **Integration health check** — `GET /admin/integrations/health`, a plain HTTP
  endpoint (JWT-authenticated, Admin-only) that reports `connected` / `error` /
  `not_configured` / `unknown` for each configured integration. Built-in checkers ship
  for `pagerduty` and `github`; other integration types report `unknown` until a
  checker is registered for them.

Every `ConfigService` RPC requires at least the Viewer role to read the service
registry and on-call rotations (referenced elsewhere in the UI); everything else —
user management, integration credentials, retention policy, and all writes — is
Admin-only.

---

## Prerequisites

- M03 complete (auth: you need an Admin JWT)
- `ENCRYPTION_KEY` set to a base64-encoded 32-byte value — required only for
  `UpsertIntegrationConfig` calls that include `credentials`; everything else works
  without it
- `curl` and `jq` installed

Generate a key:

```bash
openssl rand -base64 32
```

---

## Start the stack

```bash
cp .env.example .env
# Fill in JWT_SECRET and ENCRYPTION_KEY (see above)
make up
```

Or for local development without Docker:

```bash
JWT_SECRET=changeme ENCRYPTION_KEY=$(openssl rand -base64 32) go run ./cmd/server
```

---

## Walkthrough

All commands below assume the server is running on `localhost:8080`.

### 0. Log in as Admin

`POST /auth/register` only grants `admin` to the very first user ever created in the
database.

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' | jq .

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

### 1. Service registry CRUD

```bash
# Create
curl -s -X POST http://localhost:8080/v1/config/services \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"id":"checkout","name":"Checkout","owning_team":"commerce","tags":{"tier":"1"}}' | jq .

# List (all)
curl -s http://localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" | jq .

# Deactivate — preserves the record for historical SEV references
curl -s -X PATCH http://localhost:8080/v1/config/services/checkout \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"active":false}' | jq '{id, active}'

# active_only listing now excludes it
curl -s "http://localhost:8080/v1/config/services?active_only=true" \
  -H "Authorization: Bearer $TOKEN" | jq '.services | length'
# → 0
```

### 2. User management

```bash
# Register a second user — gets the default Viewer role
curl -s -X POST http://localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"changeme123","name":"Alice"}' | jq -r .user.org_role
# → viewer

# List / search the directory
curl -s "http://localhost:8080/v1/config/users?query=alice" \
  -H "Authorization: Bearer $TOKEN" | jq .

ALICE_ID=$(curl -s "http://localhost:8080/v1/config/users?query=alice" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.users[0].id')

# Promote to Responder
curl -s -X PATCH "http://localhost:8080/v1/config/users/$ALICE_ID/role" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"org_role":"responder"}' | jq '{id, org_role}'

# Deactivate — revokes access, keeps historical attribution
curl -s -X POST "http://localhost:8080/v1/config/users/$ALICE_ID/deactivate" \
  -H "Authorization: Bearer $TOKEN" | jq '{id, active}'
```

### 3. On-call configuration

```bash
# Normal rotation, PagerDuty-backed
curl -s -X POST http://localhost:8080/v1/config/oncall \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Checkout primary","service_id":"checkout","pagerduty_schedule_id":"PSCHED1"}' | jq .

# Manual override with a time window (planned change), takes precedence
# over the normal rotation while the window is active
curl -s -X POST http://localhost:8080/v1/config/oncall \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "name": "Holiday override",
    "service_id": "checkout",
    "manual_user_id": "user-1",
    "manual_display_name": "Alice",
    "override_start": "2026-12-24T00:00:00Z",
    "override_end": "2026-12-26T00:00:00Z"
  }' | jq .

curl -s http://localhost:8080/v1/config/oncall -H "Authorization: Bearer $TOKEN" | jq .
```

### 4. Integration configuration (encrypted credentials)

```bash
# Store a PagerDuty API key — encrypted with AES-256-GCM before it touches the DB
curl -s -X PUT http://localhost:8080/v1/config/integrations/pagerduty \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"credentials":{"api_key":"YOUR_PAGERDUTY_KEY"},"settings":{"default_escalation_policy":"P123"}}' \
  | jq .
# → credentials_configured: true — the response never contains the raw key

curl -s http://localhost:8080/v1/config/integrations/pagerduty \
  -H "Authorization: Bearer $TOKEN" | jq .

# Update settings only — the stored credential is left untouched
curl -s -X PUT http://localhost:8080/v1/config/integrations/pagerduty \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"settings":{"default_escalation_policy":"P456"}}' | jq .
```

If you inspect the database directly, `integration_config.encrypted_credentials` is
opaque bytes, never the plaintext API key:

```bash
make psql
# \x
# SELECT integration_type, encrypted_credentials FROM integration_config;
```

### 5. Integration health check

```bash
curl -s http://localhost:8080/admin/integrations/health \
  -H "Authorization: Bearer $TOKEN" | jq .
```

With a real PagerDuty key configured in step 4, this reports `"status":"connected"`
(or `"status":"error"` with PagerDuty's rejection message if the key is bad). An
integration type with no built-in checker (e.g. `datadog`) reports `"status":"unknown"`
— its config exists but connectivity isn't tested yet.

### 6. Data retention policy

```bash
curl -s http://localhost:8080/v1/config/retention -H "Authorization: Bearer $TOKEN" | jq .
# → all four levels default to retention_days: 0 (retain forever)

# SEV-4s: archive (soft-delete) after 1 year
curl -s -X PUT http://localhost:8080/v1/config/retention/4 \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"retention_days":365,"hard_delete":false}' | jq .
```

---

## Verify tests pass

```bash
go test ./internal/api/grpc/... ./internal/store/... ./internal/integrations/... ./internal/auth/... -v
golangci-lint run
```

Key coverage:

- `internal/store/crypto`: AES-256-GCM round trip, tampered ciphertext rejected,
  wrong key rejected, invalid key size rejected.
- `internal/store/memory`: `RetentionConfigStore` pre-seeded defaults, upsert,
  not-found — plus the pre-existing `ServiceStore`, `OnCallStore` (manual override /
  time-window precedence), `IntegrationConfigStore`, and `UserStore` coverage from M01.
- `internal/api/grpc`: full CRUD for services, users, on-call rotations, integration
  config (including the encrypt-on-write/decrypt-on-read round trip and the
  credentials-omitted-preserves-existing case), and retention config; the
  `IntegrationsHealthHandler`'s own JWT + RBAC enforcement and status reporting.

---

## Known limitations

- `UpsertIntegrationConfig` and `GetRetentionConfig`/`UpdateRetentionConfig` are
  in-memory only even when `DATABASE_URL` is set — a PostgreSQL-backed
  `IntegrationConfigStore`/`RetentionConfigStore` is deferred, matching the same
  "in-memory for now" treatment M01–M09 already gave `ServiceStore`, `OnCallStore`,
  `PostmortemStore`, `AnnouncementStore`, `ChatStore`, `SEVLinkStore`, and `TaskStore`.
- The integration health check only ships built-in checkers for `pagerduty` and
  `github`; other integration types (`slack`, `datadog`, `prometheus`, `cloudwatch`)
  report `"status":"unknown"` until M11/M12 register checkers for them.
- `notification_config` (§18.5 — escalation thresholds and notification routing) has
  its table from M01 but no Config API surface yet; it's deferred to whichever
  milestone wires up actual notification delivery.
- No guard prevents demoting or deactivating the last remaining Admin — take care not
  to lock yourself out in a fresh environment.
