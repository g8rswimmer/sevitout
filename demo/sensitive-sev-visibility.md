# Demo — Sensitive SEV Visibility (§14)

## What was built

`docs/requirements.md` §14 promises: *"Sensitive SEVs (e.g., security
incidents) may have restricted visibility — only explicitly added users can
view."* Before this change, that was entirely unimplemented — `GetSEV` and
`ListSEVs` did zero visibility checks, so any authenticated Viewer could
read a sensitive SEV by ID, and `UpdateSEV`/`TransitionStatus` had the same
gap, meaning a non-allowed user who merely knew a sensitive SEV's ID could
silently edit or transition it too.

This adds an explicit per-user access list, enforced across all four of
those endpoints:

- **`SEVAccessService`** (`GrantAccess`/`RevokeAccess`/`ListAccess`,
  `/v1/sevs/{sev_id}/access`) — a new `sev_access` table records who's been
  explicitly granted visibility into a specific sensitive SEV.
- **`sensitiveSEVVisible`** (`internal/api/grpc/visibility.go`) — the shared
  check. A non-sensitive SEV is always visible. A sensitive SEV is visible
  to an Admin or Incident Commander unconditionally (the same trust
  boundary already used for `UnlockSEV`/`ShareService`), to anyone
  explicitly granted, and to no one else. `GetSEV`, `UpdateSEV`, and
  `TransitionStatus` all return `NotFound` — never `PermissionDenied` — when
  denied, so a non-allowed caller can't tell "doesn't exist" from "exists
  but you can't see it" (same masking `ShareViewHandler` already used for
  public share links).
- **`ListSEVs`** — an Admin or Incident Commander keeps today's fully
  SQL-pushed-down fast path; everyone else gets sensitive SEVs they can't
  see filtered out in Go, with `Total` and pagination both reflecting the
  post-filter set.
- **Auto-grant** — creating a sensitive SEV grants the creator immediately
  (so they don't lose access to what they just filed); flipping an existing
  SEV to `sensitive=true` grants the original reporter (`created_by`), not
  the person who flipped it (who already bypasses the check as Admin/IC).
- **Web UI**: a new "Allowed Viewers" panel on the SEV detail page,
  rendered only when the SEV is sensitive, gated to Incident Commander/
  Admin — plus a friendlier, still-deliberately-vague 404 message
  ("This SEV doesn't exist or you don't have access to view it.").

## Prerequisites

- `go build ./... && go test ./...` passing
- `make up` started (or `go run ./cmd/server` locally)

## Walkthrough

All commands below assume the server is running on `localhost:8080`.

### 0. Set up three users: Admin, Responder, Viewer

```bash
export API=http://localhost:8080

# First registrant is Admin (§14/§18).
curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' | jq .
ADMIN_TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"responder@example.com","password":"changeme123","name":"Responder"}' | jq .
RESPONDER_ID=$(curl -s "$API/v1/config/users" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | jq -r '.users[] | select(.email=="responder@example.com") | .id')
curl -s -X PATCH "$API/v1/config/users/$RESPONDER_ID/role" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"org_role":"responder"}' | jq .
RESP_TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"responder@example.com","password":"changeme123"}' | jq -r .token)

# Registers as Viewer by default — stays Viewer for now.
curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"viewer@example.com","password":"changeme123","name":"Viewer"}' | jq .
VIEWER_ID=$(curl -s "$API/v1/config/users" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | jq -r '.users[] | select(.email=="viewer@example.com") | .id')
VIEWER_TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"viewer@example.com","password":"changeme123"}' | jq -r .token)
```

### 1. Responder creates a sensitive SEV — the creator is auto-granted

```bash
SEV_ID=$(curl -s -X POST "$API/v1/sevs" -H "Authorization: Bearer $RESP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Security incident","severity_level":1,"sensitive":true}' | jq -r .id)
echo "SEV_ID=$SEV_ID"

# The creator can still see their own sensitive SEV — no manual grant needed.
curl -s -w '\nstatus=%{http_code}\n' "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $RESP_TOKEN" \
  | tail -1
# → status=200
```

### 2. A different Viewer can't see it at all — masked as 404, not 403

```bash
curl -s "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $VIEWER_TOKEN"
# → {"code":5,"message":"SEV not found"}  (code 5 = NotFound)

curl -s "$API/v1/sevs" -H "Authorization: Bearer $VIEWER_TOKEN" | jq .
# → {}  (Total and Sevs are both their zero value, omitted by protojson —
#        the sensitive SEV is excluded from the Viewer's list entirely)
```

### 3. Admin sees it unconditionally, via both GetSEV and ListSEVs

```bash
curl -s -o /dev/null -w 'status=%{http_code}\n' "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $ADMIN_TOKEN"
# → status=200

curl -s "$API/v1/sevs" -H "Authorization: Bearer $ADMIN_TOKEN" | jq '.sevs[].id'
# → "SEV-2026-0001" (or whatever ID was assigned)
```

### 4. Promote the Viewer to Incident Commander — same unconditional bypass

```bash
curl -s -X PATCH "$API/v1/config/users/$VIEWER_ID/role" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"org_role":"incident-commander"}' | jq .
IC_TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"viewer@example.com","password":"changeme123"}' | jq -r .token)

curl -s -o /dev/null -w 'status=%{http_code}\n' "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $IC_TOKEN"
# → status=200 (Incident Commander, like Admin, doesn't need an explicit grant)
```

### 5. A plain Responder can't grant access — RBAC floor is IC-or-Admin

```bash
curl -s -w '\nstatus=%{http_code}\n' -X POST "$API/v1/sevs/$SEV_ID/access" \
  -H "Authorization: Bearer $RESP_TOKEN" -H 'Content-Type: application/json' -d '{"user_id":"someone"}'
# → 403 {"code":7,"message":"insufficient permissions for /sevitout.v1.SEVAccessService/GrantAccess"}
```

### 6. The Incident Commander grants a fresh Viewer access

```bash
curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"secondviewer@example.com","password":"changeme123","name":"Second Viewer"}' | jq .
V2_ID=$(curl -s "$API/v1/config/users" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | jq -r '.users[] | select(.email=="secondviewer@example.com") | .id')
V2_TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"secondviewer@example.com","password":"changeme123"}' | jq -r .token)

# Before the grant: 404, same as step 2.
curl -s -o /dev/null -w 'before grant: status=%{http_code}\n' "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $V2_TOKEN"

curl -s -X POST "$API/v1/sevs/$SEV_ID/access" -H "Authorization: Bearer $IC_TOKEN" \
  -H 'Content-Type: application/json' -d "{\"user_id\":\"$V2_ID\"}" | jq .
# → {"id":"...","sev_id":"...","user_id":"...","created_at":"...","created_by":"..."}

curl -s -o /dev/null -w 'after grant: status=%{http_code}\n' "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $V2_TOKEN"
# → status=200

curl -s "$API/v1/sevs" -H "Authorization: Bearer $V2_TOKEN" | jq '.total, .sevs[].id'
# → 1, "SEV-2026-0001" — now included in this user's list
```

### 7. List and revoke access

```bash
curl -s "$API/v1/sevs/$SEV_ID/access" -H "Authorization: Bearer $ADMIN_TOKEN" | jq .
# → {"access":[{...creator's auto-grant...}, {...the grant from step 6...}]}

GRANT_ID=$(curl -s "$API/v1/sevs/$SEV_ID/access" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | jq -r '.access[] | select(.user_id=="'"$V2_ID"'") | .id')

curl -s -o /dev/null -w 'status=%{http_code}\n' -X DELETE "$API/v1/sevs/$SEV_ID/access/$GRANT_ID" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
# → status=200

curl -s -o /dev/null -w 'status=%{http_code}\n' "$API/v1/sevs/$SEV_ID" -H "Authorization: Bearer $V2_TOKEN"
# → status=404 — access revoked, back to "not found"
```

### 8. The same masking applies to UpdateSEV

A non-allowed caller can't edit a sensitive SEV they can't see either — not
just read it (this was the more severe part of the original gap). This
needs a caller who actually clears `UpdateSEV`'s RBAC floor (Responder) but
still has no explicit grant — the Viewer/second-Viewer tokens above get
blocked by RBAC itself (`403`) before ever reaching the visibility check, so
create one more user for this:

```bash
curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"responder2@example.com","password":"changeme123","name":"Responder Two"}' | jq .
R2_ID=$(curl -s "$API/v1/config/users" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | jq -r '.users[] | select(.email=="responder2@example.com") | .id')
curl -s -X PATCH "$API/v1/config/users/$R2_ID/role" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"org_role":"responder"}' | jq .
R2_TOKEN=$(curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"responder2@example.com","password":"changeme123"}' | jq -r .token)

curl -s -o /dev/null -w 'status=%{http_code}\n' -X PATCH "$API/v1/sevs/$SEV_ID" \
  -H "Authorization: Bearer $R2_TOKEN" -H 'Content-Type: application/json' -d '{"title":"hacked"}'
# → status=404 — masked exactly like GetSEV, not 403
```

**`TransitionStatus` has no equivalent live demo**: its own RBAC floor is
already Incident Commander (`internal/auth/rbac.go`), and Incident Commander
unconditionally bypasses `sensitiveSEVVisible` by design (see "What was
built" above — an IC needs to be able to find a sensitive SEV to grant
others access to it). So *any* caller who clears `TransitionStatus`'s RBAC
floor at all is already exempt from the visibility check — there's no
legitimately-permitted caller today who can reach the check and be denied by
it. Trying it with `$R2_TOKEN` above returns `403` (blocked by RBAC before
the handler even runs), not `404`:

```bash
curl -s -o /dev/null -w 'status=%{http_code}\n' -X POST "$API/v1/sevs/$SEV_ID/transition" \
  -H "Authorization: Bearer $R2_TOKEN" -H 'Content-Type: application/json' -d '{"to_status":"investigating"}'
# → status=403 (RBAC floor, not the visibility check)
```

The check in `TransitionStatus` is still worth having as defense-in-depth —
it costs nothing and immediately starts doing real work if that RBAC floor
is ever loosened below Incident Commander — it's just not exercisable via
the API as configured today.

### 9. Flipping an existing SEV to sensitive auto-grants the original reporter

```bash
# Responder opens an ordinary (non-sensitive) SEV.
ORDINARY_ID=$(curl -s -X POST "$API/v1/sevs" -H "Authorization: Bearer $RESP_TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"Ordinary incident","severity_level":3}' | jq -r .id)

# Admin later flags it sensitive.
curl -s -X PATCH "$API/v1/sevs/$ORDINARY_ID" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"sensitive":true}' | jq '{id,sensitive}'

# The Responder (the reporter) still sees it — auto-granted on the flip.
curl -s -o /dev/null -w 'status=%{http_code}\n' "$API/v1/sevs/$ORDINARY_ID" -H "Authorization: Bearer $RESP_TOKEN"
# → status=200
```

## Verify tests pass

```bash
go test ./internal/api/grpc/... ./internal/store/... -v
go test -race ./...
golangci-lint run
```

Key coverage: `internal/api/grpc/sev_access_test.go` (Grant/Revoke/ListAccess
CRUD, `AlreadyExists`/`NotFound` mapping, `ListAccess` masking for a
non-granted Viewer); `sev_test.go` (`GetSEV`/`UpdateSEV`/`TransitionStatus`
hidden-vs-visible cases for Admin/Incident Commander/granted/non-granted
callers, `ListSEVs` exclusion + inclusion + pagination-after-filtering, both
auto-grant paths and their idempotency on repeated flips);
`internal/store/memory/memory_test.go`'s `TestSEVAccessStore` (store-level
grant/revoke/list/exists behavior, including the duplicate-grant conflict).

Frontend: `cd web && npm run build && npx oxlint && npm test` — covers
`AllowedViewersPanel.test.tsx` (read-only vs. managing rendering, grant,
revoke, server-error surfacing).

## Known limitations

- **WebSocket broadcasts aren't filtered per viewer.** The pre-existing
  `if !record.Sensitive { publish }` pattern still suppresses every
  sensitive-SEV update for *all* subscribers, including ones now explicitly
  granted access — it was already all-or-nothing before this change, and
  making it per-viewer would need visibility checks inside the WS hub
  itself, a separate follow-up.
- **Sub-resource list endpoints aren't gated.** `GetPostmortem`,
  `ListRoles`, `ListAnnouncements`, `ListChatEntries`, `ListTasks`, and
  `ListLinkedSEVs` all accept a `sev_id` directly and don't check
  `Sensitive` — someone who already knows a sensitive SEV's ID can still
  pull its postmortem/chat/roles/tasks/links by calling those services
  directly, bypassing `GetSEV`'s gate entirely. A fast-follow using the same
  `sensitiveSEVVisible` helper at each call site would close this.
- `ListSEVs`'s non-privileged path fetches up to `sevListFanoutLimit`
  (10,000) matching SEVs into Go memory to filter and paginate, mirroring
  the same trade-off `SearchService`/`ReportService` already made —
  acceptable at this system's single-org v1 scale, not how a larger
  deployment would want this computed.
