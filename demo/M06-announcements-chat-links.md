# M06 Demo — Announcements, Chat Log & Linked SEVs

## What was built

Three new gRPC/REST services are now live on the API server:

- **AnnouncementService** — time-ordered status updates on a SEV with enforced audience types (`internal`, `external`, `status-page`). Supports optional audience filtering on list.
- **ChatService** — chat log entries captured from incident channels, ordered by insertion.
- **SEVLinkService** — typed, bidirectional SEV-to-SEV relationships (`related`, `caused-by`, `duplicate`, `recurrence-of`). Linking A→B automatically inserts the reverse B→A row so both SEVs reflect the relationship.

All three services are protected by the existing JWT auth interceptors and use the same in-memory stores for local development (swap to PostgreSQL by setting `DATABASE_URL`).

---

## Prerequisites

- M05 complete (auth, SEV lifecycle, postmortem all working)
- Required env vars: `JWT_SECRET` (optional — default used if absent)
- `curl` and `jq` installed

---

## Start the stack

```bash
make up
```

Or for local development without Docker:

```bash
go run ./cmd/server
```

---

## Walkthrough

All commands below assume the server is running on `localhost:8080`.

### 1. Register and log in

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","password":"s3cr3t","name":"Alice"}' | jq .

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","password":"s3cr3t"}' | jq -r .token)

echo "token: $TOKEN"
```

### 2. Create a SEV

```bash
SEV_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"API latency spike","severity_level":2}' | jq -r .id)

echo "sev_id: $SEV_ID"
```

### 3. Post announcements with different audiences

```bash
# Internal update
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/announcements" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"We are investigating elevated P99 latency on the API.\",\"audience\":\"internal\"}" | jq .

# External customer-facing update
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/announcements" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"We are aware of degraded API performance and are working on a fix.\",\"audience\":\"external\"}" | jq .

# Status page update (milestone)
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/announcements" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"Service degradation identified. Mitigation in progress.\",\"audience\":\"status-page\",\"is_milestone\":true}" | jq .
```

### 4. List announcements (all and filtered)

```bash
# All announcements
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/announcements" \
  -H "Authorization: Bearer $TOKEN" | jq '.announcements | length'
# Expected: 3

# External only
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/announcements?audience=external" \
  -H "Authorization: Bearer $TOKEN" | jq '.announcements | length'
# Expected: 1
```

### 5. Add chat log entries

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/chat" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"source":"slack","author":"alice","content":"Checked Datadog — P99 spiked to 8s at 14:02 UTC."}' | jq .

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/chat" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"source":"slack","author":"bob","content":"Deploy went out at 13:58 — rolling back now."}' | jq .
```

### 6. List chat entries

```bash
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/chat" \
  -H "Authorization: Bearer $TOKEN" | jq '.entries | length'
# Expected: 2
```

### 7. Create a second SEV and link it

```bash
SEV_ID2=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Database connection pool exhaustion","severity_level":3}' | jq -r .id)

echo "sev_id2: $SEV_ID2"

# Link: SEV_ID is caused by SEV_ID2
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/links" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"target_sev_id\":\"$SEV_ID2\",\"relationship_type\":\"caused-by\"}" | jq .
```

### 8. Verify bidirectional links

```bash
# SEV_ID should show a link
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/links" \
  -H "Authorization: Bearer $TOKEN" | jq '.links | length'
# Expected: at least 1 (A→B)

# SEV_ID2 should also show the reverse link
curl -s "http://localhost:8080/v1/sevs/$SEV_ID2/links" \
  -H "Authorization: Bearer $TOKEN" | jq '.links | length'
# Expected: at least 1 (B→A inserted automatically)
```

### 9. Unlink SEVs

```bash
curl -s -X DELETE "http://localhost:8080/v1/sevs/$SEV_ID/links/$SEV_ID2?relationship_type=caused-by" \
  -H "Authorization: Bearer $TOKEN"

# Verify both sides are gone
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/links" \
  -H "Authorization: Bearer $TOKEN" | jq '.links | length'
# Expected: 0
```

---

## Verify tests pass

```bash
go test ./internal/api/grpc/... -run "TestCreateAnnouncement|TestListAnnouncements|TestAddChatEntry|TestListChatEntries|TestLinkSEVs|TestUnlinkSEVs|TestListLinkedSEVs" -v
go test ./...
golangci-lint run
```

---

## Known limitations

- Announcement `search_vector` (`tsvector`) is populated by a PostgreSQL trigger (defined in M01 migrations). The in-memory store used for local dev does not replicate this — full-text search across announcements is only available when `DATABASE_URL` is set.
- PostgreSQL store implementations for `AnnouncementStore`, `ChatStore`, and `SEVLinkStore` are deferred to a later milestone; the server currently falls back to in-memory for these stores even when `DATABASE_URL` is set.
- No pagination yet on list endpoints — all entries are returned in a single response.
