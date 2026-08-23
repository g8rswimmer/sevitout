# M13 Demo — Reporting, Analytics & Public Shareable Links

## What was built

Dashboard metrics, recurring-incident detection, CSV export, and public shareable
links described in `docs/requirements.md` §17 and §14.1:

- **`ReportService`** (`proto/sevitout/v1/report.proto`):
  - `GetDashboardMetrics` — active SEV count by severity level, MTTR trend over
    7/30/90-day windows, SEV frequency by affected service and severity level,
    postmortem completion rate, and overdue task count. All computed in Go over the
    full SEV/postmortem/task set (single-org v1 scale — see `docs/requirements.md`
    §19), the same fan-out-then-aggregate approach `SearchService` already uses for
    its announcement-matching path.
  - `GetSEVTrends` — groups SEVs by (affected service, root cause category) and
    returns every group with 2+ members, the "recurring incident flag."
  - `ExportSEVs` — `GET /v1/sevs/export.csv`, a filtered SEV list as `text/csv`. This
    is the one RPC in the whole API whose response isn't JSON: it returns
    `google.api.HttpBody`, and `cmd/server/main.go` wraps the gateway's marshaler in
    `runtime.HTTPBodyMarshaler` so grpc-gateway writes the raw bytes instead of
    JSON-encoding them — everything else on the gateway is completely unaffected,
    since that marshaler falls back to the normal JSONPb path for every other
    response type.
- **Recurring incident auto-link** (§17) — `SEVServer.autoLinkRecurrence`, called
  whenever a SEV's root cause category is set (`CreateSEV`, and `UpdateSEV` when the
  category is newly set or changed): looks for the most recent other SEV sharing both
  the category and an affected service, and links the new one to it as
  `recurrence-of` via the existing `SEVLinkService` machinery (M06). Only the single
  most recent match is linked, not every prior occurrence.
- **`ShareService`** (`proto/sevitout/v1/share.proto`): `CreateShareLink` (blocked for
  Sensitive SEVs — §14.1) and `RevokeShareLink`, both Incident-Commander-or-Admin like
  unlocking a completed SEV. A link is a signed token (`internal/share.Signer`, HMAC-
  SHA256 over the SEV ID and expiry, reusing `JWT_SECRET` — the same pattern
  `postmortem.UnlockSigner` already uses for unlock tokens) stored in
  `shareable_links` (an M01 table that had gone unused until now).
- **The public view itself, `GET /s/{token}`, is deliberately *not* a gRPC method.**
  Every gRPC call — and every REST route grpc-gateway generates from one — passes
  through the JWT auth interceptor unconditionally, so a route that must work with no
  login can't be implemented as one. It's a plain `net/http` handler
  (`internal/api/grpc.ShareViewHandler`) registered directly on the HTTP mux ahead of
  the gateway's catch-all — the same pattern already used for `/ws` and
  `/admin/integrations/health`. It checks `ShareStore` for revocation/expiry (the real
  authority — a signed token can't be un-signed), re-validates the token's own
  signature and embedded expiry, and re-checks `Sensitive` against a freshly-fetched
  SEV (in case it was flagged sensitive after the link was minted). The response is a
  curated JSON object — title, severity, status, lifecycle timestamps, `external`-
  audience announcements only, and business impact — nothing else from the SEV record
  is present.

---

## Prerequisites

- M04 (roles/on-call), M05 (postmortem), M06 (announcements/links), M07 (linked
  tasks) complete
- `JWT_SECRET` set (reused for share-link signing — no new env var)
- `curl` and `jq` installed

---

## Start the stack

```bash
cp .env.example .env
# Fill in JWT_SECRET
make up
```

Or for local development without Docker:

```bash
JWT_SECRET=changeme go run ./cmd/server
```

---

## Walkthrough

All commands below assume the server is running on `localhost:8080`.

### 0. Log in

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' | jq .

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

The first registered user is Admin (§14), which is also the minimum role
`CreateShareLink`/`RevokeShareLink` need.

### 1. Recurring incident auto-link

Two SEVs on the same service, same root cause category:

```bash
SEV1=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout 500s (first)","severity_level":2,"affected_services":["checkout"]}' \
  | jq -r .id)

curl -s -X PATCH "http://localhost:8080/v1/sevs/$SEV1" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"root_cause_category":"deployment"}' | jq '{id,root_cause_category}'

SEV2=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout 500s (again)","severity_level":2,"affected_services":["checkout"]}' \
  | jq -r .id)

curl -s -X PATCH "http://localhost:8080/v1/sevs/$SEV2" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"root_cause_category":"deployment"}' | jq '{id,root_cause_category}'

# SEV2 was auto-linked to SEV1 as recurrence-of when its root cause was set:
curl -s "http://localhost:8080/v1/sevs/$SEV2/links" \
  -H "Authorization: Bearer $TOKEN" | jq .
# → relationship_type: "recurrence-of", target_sev_id: $SEV1
```

### 2. Dashboard metrics and trends

```bash
curl -s http://localhost:8080/v1/reports/dashboard \
  -H "Authorization: Bearer $TOKEN" | jq .
# → active_by_level, mttr_trends (7/30/90-day), frequency_by_service_and_level,
#   postmortem_completion_rate, overdue_task_count

curl -s http://localhost:8080/v1/reports/trends \
  -H "Authorization: Bearer $TOKEN" | jq .
# → recurring_patterns: [{service_id: "checkout", root_cause_category: "deployment",
#     count: 2, sev_ids: [<SEV2>, <SEV1>]}]
```

### 3. CSV export

```bash
curl -s "http://localhost:8080/v1/sevs/export.csv?severity_levels=2" \
  -H "Authorization: Bearer $TOKEN" -o /tmp/sevs.csv
head -3 /tmp/sevs.csv
# id,title,severity_level,status,root_cause_category,affected_services,started_at,...
```

### 4. Public shareable link

```bash
LINK=$(curl -s -X POST "http://localhost:8080/v1/sevs/$SEV1/share" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"expires_in_days":7}')
echo "$LINK" | jq .
TOKEN_STR=$(echo "$LINK" | jq -r .token)

# No Authorization header — this is the whole point:
curl -s "http://localhost:8080/s/$TOKEN_STR" | jq .
# → {id, title, severity_level, status, started_at, ..., business_impact,
#     announcements: [...external only...]}
# root_cause_category, tags, created_by, audit/chat log are all absent, not just empty.
```

### 5. Revoke it

```bash
curl -s -w '\nhttp_status=%{http_code}\n' -X DELETE \
  "http://localhost:8080/v1/sevs/$SEV1/share/$TOKEN_STR" \
  -H "Authorization: Bearer $TOKEN"

curl -s -w '\nhttp_status=%{http_code}\n' "http://localhost:8080/s/$TOKEN_STR"
# → 410 Gone
```

### 6. Sensitive SEVs can't get a link

```bash
SENSITIVE=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Security incident","severity_level":1,"sensitive":true}' | jq -r .id)

curl -s -w '\nhttp_status=%{http_code}\n' -X POST "http://localhost:8080/v1/sevs/$SENSITIVE/share" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}'
# → 400 FAILED_PRECONDITION: sensitive SEVs cannot have shareable links generated
```

---

## Verify tests pass

```bash
go test ./internal/share/... ./internal/api/grpc/... ./internal/store/... -v
go test -race ./...
golangci-lint run
```

Key coverage:

- `internal/share`: signer sign/verify round trip, expired token, tampered token,
  wrong-secret token, garbage input.
- `internal/api/grpc`: `ReportServer` — active-by-level, MTTR trend per window,
  frequency by service+level, postmortem completion rate (including the no-data
  case), overdue task count, CSV header/row/quoting, severity filter; `ShareServer` —
  create (default and custom expiry, sensitive-SEV block, audit entry), revoke
  (sev_id/token mismatch rejected, audit entry); `ShareViewHandler` — valid token,
  unknown token, revoked (410), expired (410), sensitive SEV re-checked at read time
  (404), internal fields absent from the response, method-not-allowed; `SEVServer` —
  recurrence auto-link on matching service+category, no link for a different
  service/category, no re-link on an unrelated follow-up update.

---

## Known limitations

- `ShareStore` is in-memory only even when `DATABASE_URL` is set — same "postgres
  implementation deferred" treatment M10/M12 gave several other stores (the
  `shareable_links` table itself has existed since M01's migration).
- `GetDashboardMetrics`/`GetSEVTrends`/`ExportSEVs` fetch the full SEV set into Go
  memory to aggregate (bounded at 10,000 records, failing loudly rather than silently
  truncating past that) instead of pushing the aggregation into SQL — acceptable at
  the single-org v1 scale this system targets (`docs/requirements.md` §19), but not
  how a multi-tenant or high-volume deployment would want this computed.
- The recurring-incident auto-link only fires when a root cause category is set or
  changed, and only links to the single most recent match — it doesn't re-scan or
  re-link older SEVs retroactively, and it never creates more than one new link per
  update.
- `ExportSEVsRequest` supports the same filter shape as `SearchSEVsRequest` minus
  full-text search and pagination (an export is meant to return everything matching
  the filter, not one page of it).
- The public share view exposes announcements with audience `external` only, per
  `docs/requirements.md` §14.1's literal field list — `status-page`-audience
  announcements are not included even though they're also public-facing.
