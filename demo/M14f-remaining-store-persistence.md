# M14f Demo — Persisting the Remaining In-Memory Stores

## What was built

The last six store interfaces that had no PostgreSQL implementation now have
one, closing out the recurring gap first found in M14's postmortem fix and
the five admin-tab stores fixed in M14d:

| Store | Backs | Table |
|---|---|---|
| `AnnouncementStore` | `AnnouncementService` (M06) | `sev_announcements` |
| `ChatStore` | `ChatService` (M06) | `sev_chat_log` |
| `SEVLinkStore` | `SEVLinkService` (M06) | `sev_links` |
| `TaskStore` | `TaskService` (M07) | `sev_linked_tasks` |
| `AIOutputStore` | AI plugin outputs (M12) | `ai_outputs` |
| `ShareStore` | `ShareService` (M13/M14e) | `shareable_links` |

A seventh, `SLIStore` (`sev_slis`), also gained a PostgreSQL implementation
for consistency, even though nothing in the API currently calls it — it's
unwired scaffolding from M01 that predates any SLI-tracking feature, exactly
like the store it backs.

Before this fix, every SEV survived an API restart, but its announcements,
chat log, linked tasks, linked SEVs, AI-drafted content, and public share
links did not — reviewing a SEV after a restart looked like it had none of
that activity, even though it clearly did before the restart.

## Root cause

Identical in shape to the two persistence bugs found earlier in this branch
(`PostmortemStore` in M14, then five admin-tab stores in M14d): the SQL
migrations and sqlc-generated query code for all six tables have existed
unused since M01, but `cmd/server/main.go`'s `buildStores` never had a
`store.XStore` PostgreSQL wrapper to call — it fell back to
`memory.NewXStore()` even when `DATABASE_URL` was set, silently, with only a
single combined startup warning log line to notice it by.

Two smaller pre-existing gaps were also fixed as part of this work:

- **`sev_linked_tasks` had no uniqueness constraint** on
  `(sev_id, external_system, task_id)`, even though the in-memory
  `TaskStore` had always rejected a duplicate triple with `ErrConflict`. The
  PostgreSQL-backed store would have silently allowed the same external task
  to be linked to the same SEV twice. Added via migration
  `000008_task_unique_constraint`.
- `AnnouncementStore.SearchSEVIDs` (used by `SearchService` — see M08) and
  `TaskStore.SetDueDateIfUnset` / `TaskStore.CountOverdue` had no
  corresponding sqlc queries yet; added to `internal/store/sql/announcements.sql`
  and `tasks.sql` and regenerated with `sqlc generate`. The announcement
  search reuses the same `search_vector`/`plainto_tsquery` full-text search
  M06 already wired up via a PostgreSQL trigger — the in-memory store's
  plain substring match was always just a local-dev approximation of it.

## Prerequisites

- `DATABASE_URL` set (the whole point of this fix only applies to the
  PostgreSQL-backed deployment)
- `make migrate` run (or `make up`, which runs migrations automatically) to
  pick up the new `000008_task_unique_constraint` migration

## Start the stack

```bash
make up
```

## Walkthrough

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

SEV_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Persistence check","severity_level":2,"description":"test"}' | jq -r .id)

# One write to each of the six stores
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/announcements" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"message":"Investigating","audience":"internal"}' | jq -r .id

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/chat" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"source":"slack","author":"alice","content":"on it","occurred_at":"2026-08-24T19:00:00Z"}' | jq -r .id

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/tasks" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"external_system":"github","task_id":"42","url":"https://github.com/org/repo/issues/42","title":"Fix it","relationship_type":"action-item","priority":"critical"}' | jq -r .id

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/share" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}' | jq -r .token
```

Restart the API only (the database keeps running):

```bash
docker restart sevitout-api-1
```

Confirm everything is still there:

```bash
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/announcements" -H "Authorization: Bearer $TOKEN" | jq '.announcements | length'
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/chat" -H "Authorization: Bearer $TOKEN" | jq '.entries | length'
curl -s "http://localhost:8080/v1/sevs/$SEV_ID/tasks" -H "Authorization: Bearer $TOKEN" | jq '.tasks | length'
```

Expected: `1` for each — before this fix, all three would have been `0`
(or the announcements/tasks endpoints would 404 the SEV_ID entirely if the
container had cycled between calls in a real deployment, since each restart
of the old in-memory store wiped it back to empty).

Duplicate-task conflict (new constraint):

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/tasks" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"external_system":"github","task_id":"42","url":"...","title":"dup","relationship_type":"action-item","priority":"critical"}'
```

Expected: `{"code":6,"message":"this task is already linked to the SEV"}`
(`AlreadyExists`), both before and after a restart.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

`internal/store/postgres` has no dedicated unit test file — like the rest of
that package, it's verified via the live Docker walkthrough above rather
than a mocked/fake-DB unit suite (there's no PostgreSQL test-container setup
in this repo yet).

## Known limitations

- `SLIStore`/`sev_slis` has a working PostgreSQL implementation but is still
  entirely unwired — no RPC, no handler, no UI calls it. It's dead code
  carried forward from M01, unrelated to this fix beyond receiving the same
  treatment for consistency.
- Every store interface in `internal/store/store.go` is now backed by
  PostgreSQL when `DATABASE_URL` is set. There is no remaining in-memory
  fallback gap — the in-memory implementations remain only as the
  zero-dependency local-dev/test default when `DATABASE_URL` is unset.
