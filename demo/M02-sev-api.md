# M02 — SEV Core API (Unauthenticated)

## What was built

A running gRPC + REST gateway server that creates, reads, updates, and transitions SEVs. The server multiplexes gRPC (h2c) and HTTP/1.1 on the same port (`:8080`) using `cmux`. Every mutation writes an immutable audit log entry. The state machine enforces valid lifecycle transitions. Derived metrics (MTTD, MTTM, MTTR, DTTM) are computed automatically from lifecycle timestamps. OpenAPI documentation is served at `GET /openapi.json`.

## Prerequisites

- M01 complete (migrations applied)
- Docker and Docker Compose available
- `.env` populated (`cp .env.example .env` then fill `POSTGRES_*`)
- `curl` and `jq` installed for the walkthrough

## Start the stack

```bash
make up          # starts postgres + runs migrations + starts api on :8080
```

Wait for the `api` service to log `"msg":"sevitout api starting","addr":":8080"` before proceeding.

## Walkthrough

### 1 — Confirm the server is up

```bash
curl -s http://localhost:8080/openapi.json | jq .info
# Expected: {"title":"...","version":"..."}
```

### 2 — Create a SEV

```bash
SEV=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "API latency spike",
    "description": "P99 exceeded 5 s for 15 minutes",
    "severity_level": 2,
    "created_by": "demo-user",
    "affected_services": ["api-gateway"],
    "detection_method": "alert",
    "alert_name": "p99_latency_high"
  }')
echo "$SEV" | jq .
SEV_ID=$(echo "$SEV" | jq -r .id)
echo "Created: $SEV_ID"
# Expected: id like "SEV-2026-0001", status "open"
```

### 3 — Get the SEV

```bash
curl -s http://localhost:8080/v1/sevs/$SEV_ID | jq '{id,status,title,severity_level}'
```

### 4 — Update the SEV

```bash
curl -s -X PATCH http://localhost:8080/v1/sevs/$SEV_ID \
  -H 'Content-Type: application/json' \
  -d '{
    "root_cause_category": "deployment",
    "business_impact": "~5% of checkout requests failed"
  }' | jq '{id,root_cause_category,business_impact}'
```

### 5 — Transition through the lifecycle

```bash
# Open → Investigating
curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/transition \
  -H 'Content-Type: application/json' \
  -d '{"to_status":"investigating","user_id":"demo-user"}' | jq '{id,status}'

# Investigating → Mitigated (set mitigated_at timestamp)
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/transition \
  -H 'Content-Type: application/json' \
  -d "{\"to_status\":\"mitigated\",\"user_id\":\"demo-user\",\"mitigated_at\":\"$NOW\"}" | jq '{id,status,mttm_seconds}'

# Mitigated → Resolved (set resolved_at)
curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/transition \
  -H 'Content-Type: application/json' \
  -d "{\"to_status\":\"resolved\",\"user_id\":\"demo-user\",\"resolved_at\":\"$NOW\"}" | jq '{id,status,mttr_seconds}'
```

### 6 — Verify an invalid transition is rejected

```bash
curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/transition \
  -H 'Content-Type: application/json' \
  -d '{"to_status":"open","user_id":"demo-user"}' | jq .
# Expected: HTTP 400, message "sev: invalid status transition"
# (resolved → open is not a permitted transition; only resolved → postmortem_in_progress or re-open via postmortem_complete)
```

### 7 — List SEVs

```bash
curl -s 'http://localhost:8080/v1/sevs' | jq '{total:.total, ids:[.sevs[].id]}'
```

### 8 — Read the audit log

```bash
curl -s http://localhost:8080/v1/sevs/$SEV_ID/audit | jq '[.entries[] | {action,user_id,created_at}]'
# Expected: entries for sev.created, sev.status_transitioned (×3)
```

## Verify tests pass

```bash
# Unit tests (state machine, metrics, gRPC handlers)
go test ./internal/sev/... ./internal/api/grpc/...

# All project tests
go test ./...

# Linter
golangci-lint run

# Build binary
go build ./cmd/server/
```

Expected output:
```
ok  github.com/g8rswimmer/sevitout/internal/sev
ok  github.com/g8rswimmer/sevitout/internal/api/grpc
ok  github.com/g8rswimmer/sevitout/internal/store/memory
```

## Known limitations

- No authentication — all endpoints are open (added in M03)
- SEV list filtering (by severity/status/search) is passed to the store interface but the current PostgreSQL query returns all rows; filtering is applied in-memory for the postgres store until M08 adds proper SQL WHERE clauses
- Role assignments (IC, on-call, etc.) not yet exposed — added in M04
- Postmortem not yet created on SEV create — added in M05
- WebSocket push events not yet emitted — added in M09
