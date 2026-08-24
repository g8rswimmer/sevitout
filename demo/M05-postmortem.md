# M05 — Postmortem Demo Runbook

## What was built

M05 adds the postmortem workflow to Sevitout. Every SEV now automatically receives an empty Draft postmortem when it is created. The `PostmortemService` exposes four RPCs: retrieve the postmortem document, update its Markdown content, transition it through the `Draft → In Review → Approved` state machine, and unlock a completed (locked) SEV to allow further edits. When a SEV is transitioned to `Postmortem Complete` status its record becomes read-only; subsequent mutations require a short-lived unlock token obtained via `UnlockSEV`.

## Prerequisites

- M04 complete (auth, roles, PagerDuty integration all running)
- Docker Compose stack is up (`make up`)
- `.env` file present with at least `JWT_SECRET` set
- `jq` installed for pretty-printing JSON
- A registered user and JWT token (see M03 demo for registration steps)

Store the JWT in a shell variable for convenience:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

## Start the stack

```bash
make up
```

Wait for the `api` container to print `sevitout api starting`.

## Walkthrough

### 1. Create a SEV (postmortem auto-created in Draft)

```bash
SEV_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Checkout service outage","severity_level":2}' | jq -r .id)
echo "Created SEV: $SEV_ID"
```

Expected: a `SEV-YYYY-XXXX` identifier.

### 2. Fetch the auto-created postmortem

```bash
curl -s http://localhost:8080/v1/sevs/$SEV_ID/postmortem \
  -H "Authorization: Bearer $TOKEN" | jq .
```

Expected: `"status": "draft"`, `"content": ""`.

### 3. Write postmortem content (Markdown)

```bash
curl -s -X PATCH http://localhost:8080/v1/sevs/$SEV_ID/postmortem \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "content": "## Summary\n\nThe checkout service was unavailable for 22 minutes.\n\n## Root cause\n\nA bad deployment rolled out a nil pointer in the payment handler."
  }' | jq .
```

Expected: updated content returned with `"status": "draft"`.

### 4. Transition postmortem: Draft → In Review

```bash
curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/postmortem/transition \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"to_status":"in-review"}' | jq .
```

Expected: `"status": "in-review"`.

### 5. Transition postmortem: In Review → Approved

```bash
curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/postmortem/transition \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"to_status":"approved"}' | jq .
```

Expected: `"status": "approved"`.

### 6. Walk the SEV to Postmortem Complete (locks the record)

Move the SEV through its remaining lifecycle steps:

```bash
# Investigating → Mitigated → Resolved → Postmortem In Progress → Postmortem Complete
for STATUS in investigating mitigated resolved postmortem_in_progress postmortem_complete; do
  curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/transition \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"to_status\":\"$STATUS\"}" | jq -r .status
done
```

Expected output (one status per line):
```
investigating
mitigated
resolved
postmortem_in_progress
postmortem_complete
```

Verify the SEV is now locked:

```bash
curl -s http://localhost:8080/v1/sevs/$SEV_ID \
  -H "Authorization: Bearer $TOKEN" | jq .locked
```

Expected: `true`.

### 7. Attempt a write without an unlock token (must be rejected)

```bash
curl -s -X PATCH http://localhost:8080/v1/sevs/$SEV_ID/postmortem \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"content":"unauthorized edit"}' | jq .
```

Expected: HTTP 403 with `"SEV is locked; provide an unlock_token"`.

### 8. Unlock the SEV with a written reason

```bash
UNLOCK_TOKEN=$(curl -s -X POST http://localhost:8080/v1/sevs/$SEV_ID/unlock \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"reason":"Correcting a factual error in the root cause section"}' \
  | jq -r .unlock_token)
echo "Unlock token: $UNLOCK_TOKEN"
```

Expected: a JWT string.

### 9. Edit the postmortem with the unlock token

```bash
curl -s -X PATCH http://localhost:8080/v1/sevs/$SEV_ID/postmortem \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"content\": \"## Summary\n\nCorrected: the checkout service was unavailable for 22 minutes.\",
    \"unlock_token\": \"$UNLOCK_TOKEN\"
  }" | jq .
```

Expected: updated content returned successfully.

### 10. Verify the SEV is still locked after the edit

```bash
curl -s http://localhost:8080/v1/sevs/$SEV_ID \
  -H "Authorization: Bearer $TOKEN" | jq .locked
```

Expected: `true` — the record auto-relocks after each write.

### 11. Verify the unlock reason is in the audit log

```bash
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/audit" \
  -H "Authorization: Bearer $TOKEN" | jq '[.entries[] | select(.action == "sev.unlock_requested")]'
```

Expected: one entry with `"new_value"` containing the reason string.

## Verify tests pass

```bash
go test ./internal/postmortem/... ./internal/api/grpc/... -v
golangci-lint run
```

All tests should pass with no lint errors.

## Known limitations

- The unlock token is a short-lived JWT (15-minute TTL). Within that window the same token can be used for multiple writes; a nonce/revocation store is deferred to a future milestone.
- ~~The `PostmortemStore` backed by PostgreSQL is not yet wired up; the in-memory store is used at runtime.~~ **Fixed during M14c**: a postmortem created while `DATABASE_URL` is set now survives an API server restart — `internal/store/postgres/postmortem.go` was added and wired into `cmd/server/main.go`'s `buildStores`, using SQL/generated code that had existed unused since M01. Diagnosed from a real bug report: a postmortem edited through the M14c UI reverted to "not found" after the API process restarted, because only the SEV itself (Postgres-backed) survived — its postmortem (in-memory-only) didn't.
- Only `UpdatePostmortem` and `UpdateSEV`/`TransitionStatus` enforce lock checks. Role assignment, announcements, and chat entries do not yet check the lock; this is deferred to M06+.
