# M04 — SEV Roles, People & PagerDuty On-Call

## What was built

Role assignment API for SEVs — assign named roles (Incident Commander, on-call, responders, etc.) to people on any SEV. On SEV creation, if the primary affected service is registered and has a PagerDuty service ID configured, the current on-call engineer is automatically assigned the on-call role. Derived metrics (MTTD, MTTM, MTTR, DTTM) are already computed by the state machine on every relevant status transition (implemented in M02 and unchanged here).

New RPCs: `RoleService.AssignRole`, `RoleService.RemoveRole`, `RoleService.ListRoles`  
New REST: `POST /v1/sevs/{id}/roles`, `DELETE /v1/sevs/{id}/roles/{roleId}`, `GET /v1/sevs/{id}/roles`

## Prerequisites

- M03 complete (auth and RBAC in place)
- `go test ./...` passing
- `.env` populated (see `.env.example`)
- To exercise PagerDuty auto-population: a valid `PAGERDUTY_API_KEY` and a service in the registry with a matching PagerDuty service ID

## Start the stack

```bash
make up
```

The API listens on `http://localhost:8080`.

## Walkthrough

### 1 — Register a user and log in

```bash
# Register (first user becomes Admin)
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"ic@example.com","password":"secret123","name":"Alice"}' | jq .

# Log in and capture the token
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"ic@example.com","password":"secret123"}' | jq -r .token)

echo "TOKEN=$TOKEN"
```

### 2 — Create a SEV

```bash
SEV_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Database latency spike",
    "severity_level": 2,
    "description": "P99 DB latency exceeded 5s"
  }' | jq -r .id)

echo "SEV_ID=$SEV_ID"
```

### 3 — Assign roles

```bash
# Assign Incident Commander
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "role_type": "incident-commander",
    "display_name": "Alice",
    "user_id": "usr-alice"
  }' | jq .

# Assign a responder
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "role_type": "responder",
    "display_name": "Bob"
  }' | jq .

# Assign comms lead
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "role_type": "comms-lead",
    "display_name": "Carol"
  }' | jq .
```

### 4 — List roles on the SEV

```bash
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

Expected output: three role objects with their assigned role types and display names.

### 5 — Remove a role

```bash
# Grab the ID of the first role from the list
ROLE_ID=$(curl -s "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.roles[0].id')

curl -s -X DELETE "http://localhost:8080/v1/sevs/$SEV_ID/roles/$ROLE_ID" \
  -H "Authorization: Bearer $TOKEN"

# Verify it's gone
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $TOKEN" | jq '.roles | length'
```

Expected: `2` (one fewer than before).

### 6 — PagerDuty on-call auto-population (requires PAGERDUTY_API_KEY)

When `PAGERDUTY_API_KEY` is set and a service in the registry has a matching
PagerDuty service ID, creating a SEV with that service in `affected_services`
automatically assigns the current on-call engineer.

```bash
# Add PAGERDUTY_API_KEY to .env, then restart with: make down && make up

# Create a SEV referencing a service with a PagerDuty service ID
SEV2_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Payment service degraded",
    "severity_level": 1,
    "affected_services": ["svc-payments"]
  }' | jq -r .id)

# Roles should include a pre-populated on-call entry
curl -s "http://localhost:8080/v1/sevs/$SEV2_ID/roles" \
  -H "Authorization: Bearer $TOKEN" | jq '.roles[] | select(.role_type == "on-call")'
```

### 7 — RBAC: Viewer cannot assign roles

```bash
# Register a second user (gets Viewer role by default after the first Admin)
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"viewer@example.com","password":"viewer123","name":"Viewer"}' | jq .

VIEWER_TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"viewer@example.com","password":"viewer123"}' | jq -r .token)

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/roles" \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role_type":"responder","display_name":"Unauthorized"}' | jq .
```

Expected: HTTP 403 with `code: 7` (PermissionDenied).

## Verify tests pass

```bash
go test ./...
golangci-lint run
```

## Known limitations

- ~~The service registry (for PagerDuty on-call lookup) uses in-memory storage in
  this milestone. Services are not persisted across restarts.~~ **Fixed during
  M14d**: `internal/store/postgres/service.go` now backs `ServiceStore` when
  `DATABASE_URL` is set — see `demo/M14d-admin-pages.md`'s "Bug fix" section.
- `RoleStore` is backed by postgres when `DATABASE_URL` is set (as is `ServiceStore`,
  as of the fix above).
