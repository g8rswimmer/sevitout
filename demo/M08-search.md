# M08 — Search & Filtering

## What was built

`SearchService.SearchSEVs` — a single gRPC/REST endpoint providing full-text search,
structured filtering, sorting, and quick-view presets across SEV records.

**Full-text search** (`query`) matches a SEV's title, description, root cause
description, and business impact, plus the message text of any announcement posted on
the SEV. Against PostgreSQL this uses the existing `search_vector tsvector` columns on
`sevs` and `sev_announcements` (`plainto_tsquery('english', ...)`, chosen over
`to_tsquery` so free-form user input can't produce a malformed query); the in-memory
store does a case-insensitive substring match over the same fields.

**Structured filters** — severity levels, statuses, service IDs (matches if a SEV is
affected by any of the given services), tags (all given key/value pairs must match),
root cause category, on-call user, detected-by, and a started-at date range. `on_call_user`
and `detected_by` are resolved against `RoleService` role assignments rather than stored
directly on the SEV.

**Sort** — `started_at`, `severity`, `mttr`, or `updated_at`, ascending or descending
(`sort_desc`). SEVs missing the sorted-on value (e.g. `mttr` on a SEV that hasn't
resolved) always sort last, regardless of direction. The default (no `sort` given)
preserves the pre-M08 behavior: most recently created first.

**Quick views** (`quick_view`) —

| Value | Meaning |
|---|---|
| `open` | status is `open`, `investigating`, or `mitigated` |
| `awaiting_postmortem` | status is `resolved` or `postmortem_in_progress` (not yet `postmortem_complete`) |
| `my_sevs` | the authenticated caller holds any role assignment on the SEV |

`my_sevs` requires an authenticated caller (`401` otherwise); an unrecognized
`quick_view` or `sort` value returns `400`.

## Prerequisites

- M03 (auth) complete — you need a JWT to call authenticated endpoints.
- M04 (roles) — `on_call_user`, `detected_by`, and `my_sevs` need role assignments to
  match against.
- M06 (announcements) — optional, only needed to see announcement text contribute to
  `query` matches.
- `make up` started (or in-memory server via `go run ./cmd/server`).

## Start the stack

```bash
cp .env.example .env          # fill in JWT_SECRET
make up
```

Or run the server locally without Docker:

```bash
JWT_SECRET=changeme go run ./cmd/server
```

## Walkthrough

### 0. Log in

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

(See `demo/M07-linked-tasks.md` §0 if you need to bootstrap `admin@example.com` first.)

### 1. Seed a few SEVs with varied attributes

```bash
SEV1=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout latency spike","description":"P99 latency exceeded 5s","severity_level":1,"affected_services":["checkout"]}' \
  | jq -r .id)

SEV2=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Billing 500s","description":"Payment webhook failures","severity_level":3,"affected_services":["billing"]}' \
  | jq -r .id)

echo "SEV1=$SEV1 SEV2=$SEV2"
```

### 2. Filter by severity, status, and service

```bash
curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'severity_levels=1' \
  --data-urlencode 'statuses=open' \
  --data-urlencode 'service_ids=checkout' | jq '.sevs[].id, .total'
# → only SEV1
```

### 3. Full-text search across SEV fields and announcements

```bash
curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'query=latency' | jq '.sevs[].id'
# → SEV1 (matches on description)

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV2/announcements" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"message":"Mitigated by rolling back the latency-inducing deploy","audience":"internal"}' | jq .id

curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'query=latency' | jq '.sevs[].id'
# → both SEV1 (field match) and SEV2 (announcement match)
```

### 4. Quick views

```bash
curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'quick_view=open' | jq '.total'

curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'quick_view=my_sevs' | jq '.sevs[].id'
```

### 5. Sort by severity, ascending

```bash
curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'sort=severity' | jq '.sevs[] | {id, severity_level}'
# → SEV1 (severity 1) before SEV2 (severity 3)
```

### 6. On-call / detected-by filters

Requires a role assignment first (see `demo/M04-roles-oncall.md`):

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV1/roles" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"role_type":"on-call","display_name":"alice@example.com"}' | jq .id

curl -s -G http://localhost:8080/v1/search/sevs \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'on_call_user=alice@example.com' | jq '.sevs[].id'
# → SEV1
```

## Verify tests pass

```bash
go test ./internal/api/grpc/... ./internal/store/... -v
golangci-lint run
```

## Known limitations

- Combining `query` with more results than fit in a single fetch: when the query also
  matches announcement text, results are merged and paginated in-process rather than as
  a single paginated SQL query (the two data sources can't otherwise be combined
  correctly), bounded by an internal 10,000-row fanout cap per source. Fine for a
  single-org tool at this scale; a query without announcement matches is unaffected and
  stays fully DB-paginated.
- `tags` filtering requires an exact value match per key (no partial/wildcard match).
- Quick views are fixed presets (not user-configurable) and can be combined with
  explicit filters, but an explicit `statuses` filter takes precedence over the quick
  view's own status set rather than intersecting with it.
- AI-assisted semantic search (mentioned as a stretch goal in requirements §12) is not
  implemented; `query` is lexical full-text search only.
