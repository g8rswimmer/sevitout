# M07 — Linked Tasks & GitHub Issues

## What was built

`TaskService` gRPC/REST endpoints that link external task references to a SEV, enforce
SLA-based due dates, and surface overdue tasks.  A GitHub REST v3 client creates and
fetches issues; when `GITHUB_TOKEN` is set the `CreateGitHubIssue` endpoint creates a
real issue in the named repository and links it in one call.  When the token is absent
the GitHub features return `503 Unavailable`; all other task operations continue to work.

**SLA due-date logic** — due dates are set at link time from the SEV's `resolved_at`:

| Priority     | SLA       |
|--------------|-----------|
| `critical`   | +30 days  |
| `non-critical` | +90 days |

If the SEV is not yet resolved the due date is deferred; the first `ListTasks` call after
the SEV resolves back-fills and persists all outstanding due dates.

A task is **overdue** when `due_date < now`.  The flag is refreshed on every `ListTasks`
call.

## Prerequisites

- M03 (auth) complete — you need a JWT to call authenticated endpoints.
- `make up` started (or in-memory server via `go run ./cmd/server`).
- Optional: `GITHUB_TOKEN` env var containing a GitHub PAT with `repo` scope.

## Start the stack

```bash
cp .env.example .env          # fill in JWT_SECRET (and optionally GITHUB_TOKEN)
make up
```

Or run the server locally without Docker:

```bash
GITHUB_TOKEN=ghp_... JWT_SECRET=changeme go run ./cmd/server
```

## Walkthrough

### 0. Log in

Creating a SEV and linking tasks requires at least the `responder` role. `POST
/auth/register` only grants `admin` to the very first user ever created in the
database — every user registered after that gets `viewer`, which is too low for
these endpoints. If you've already run the M03 (or later) demo against this
database, `admin@example.com` was that first user, so log in with those
credentials instead of registering a new one:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

Starting from a fresh database instead? Register `admin@example.com` first so it
becomes the bootstrap admin:

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' \
  | jq .

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

### 1. Create a SEV

```bash
SEV=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Database connection pool exhausted","severity_level":2}' \
  | jq -r .id)
echo "SEV: $SEV"
```

### 2. Link an existing task (no GitHub token needed)

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV/tasks" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "external_system": "github",
    "task_id": "acme/infra#101",
    "url": "https://github.com/acme/infra/issues/101",
    "title": "Increase connection pool size",
    "relationship_type": "action-item",
    "priority": "critical"
  }' | jq .
# → due_date is null (SEV not yet resolved), overdue: false
```

### 3. Resolve the SEV and re-list tasks

```bash
# Resolve the SEV
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV/transition" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"to_status":"investigating"}' | jq .id

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV/transition" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"to_status":"mitigated"}' | jq .id


# NOTE: resolved_at must be passed explicitly — TransitionStatus does not
# auto-stamp it, and ListTasks's SLA back-fill only runs once resolved_at is set.
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV/transition" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"to_status\":\"resolved\",\"resolved_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}" | jq .id

# List tasks — due_date is now back-filled to resolved_at + 30 days
curl -s "http://localhost:8080/v1/sevs/$SEV/tasks" \
  -H "Authorization: Bearer $TOKEN" | jq '.tasks[0] | {due_date, overdue}'
```

### 4. Override the due date manually

```bash
TASK_ID=$(curl -s "http://localhost:8080/v1/sevs/$SEV/tasks" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.tasks[0].id')

curl -s -X PATCH "http://localhost:8080/v1/sevs/$SEV/tasks/$TASK_ID/due-date" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"due_date":"2020-01-01T00:00:00Z"}' | jq '{due_date, overdue}'
# → overdue: true (date is in the past)
```

### 5. Create a GitHub Issue and link it in one call (requires GITHUB_TOKEN)

The created issue is automatically labeled with the SEV id (`$SEV`) and its
criticality (`critical`/`non-critical`).

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV/github-issues" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "owner": "your-org",
    "repo": "your-repo",
    "title": "[SEV] Database connection pool exhausted — action item",
    "body": "SEV ID: '"$SEV"'\n\nPlease increase the max connection pool size.",
    "relationship_type": "action-item",
    "priority": "critical"
  }' | jq '{task_id, url, due_date, overdue}'
```

### 6. Unlink a task

```bash
curl -s -X DELETE "http://localhost:8080/v1/sevs/$SEV/tasks/$TASK_ID" \
  -H "Authorization: Bearer $TOKEN"
# → 200 {}
```

## Verify tests pass

```bash
go test ./internal/api/grpc/... ./internal/integrations/github/... -v
golangci-lint run
```

## Known limitations

- The overdue flag does not cross-check GitHub issue state ("open" vs "closed").
  Per-requirements, an overdue task should only surface when the issue is still open;
  live status polling is deferred to a future milestone.
- Only GitHub Issues is supported in M07.  Jira and Linear support is deferred (v2).
- The `resolved_at` back-fill in `ListTasks` is best-effort: if the store `Update`
  fails the due date is still returned in the response for that request but will not
  be persisted.
