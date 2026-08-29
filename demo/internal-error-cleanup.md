# Demo — `codes.Internal` Root-Cause Cleanup (Roadmap Phase 3)

## What was built

Before this change, `internal/api/grpc/*.go` had 113 `status.Error(codes.Internal,
"...")` call sites that discarded the underlying error entirely — e.g.
`return nil, status.Error(codes.Internal, "failed to get SEV")` never logged or
wrapped the real `err`. `LoggingUnaryInterceptor`'s own `"rpc failed"` log line only
ever saw the already-generic status message (see `demo/metrics.md`'s
`sevitout_rpc_requests_total` — the metric was equally blind to the root cause),
so a real failure — a DB outage, a driver error — was nearly invisible in the logs
beyond `code=Internal`.

- **`internalError(ctx, msg, err)`** (new file, `internal/api/grpc/errors.go`) —
  logs `err` via `telemetry.LoggerFromContext(ctx)` at Error level (so it carries
  the same `request_id`/`user_id` every other log line for that call does — see
  `demo/request-scoped-logging.md`) and returns `status.Error(codes.Internal, msg)`.
  `msg` is the only thing that still crosses the wire to the caller; `err`'s detail
  now reaches the log instead of vanishing.
- **111 of the 113 sites converted**, one mechanical substitution repeated across
  every file in `internal/api/grpc/`:
  ```go
  // before
  return nil, status.Error(codes.Internal, "failed to get SEV")
  // after
  return nil, internalError(ctx, "failed to get SEV", err)
  ```
  Landed as six separate commits on this branch, one per file group, matching
  `docs/roadmap.md`'s sequencing by call-site density / operational criticality:
  `sev.go`+`visibility.go` → `task.go`+`search.go` → `postmortem.go`+`sev_access.go`
  → the `config_*.go` family (six files) → `share.go`+`sev_link.go`+`role.go`+
  `report.go` → `chat.go`+`announcement.go`+`ai.go`+`audit.go`+`auth.go`.
- **`config_ai.go`'s `encryptAPIKey` helper** gained a `ctx` parameter (both of its
  call sites updated) specifically so its own internal-error site could convert too
  — a small, single-file-local helper, so threading `ctx` through was low-risk.

## Design notes

**Two sites deliberately left unconverted — not a swallowed error, so not this
phase's target:**
- `sev.go`'s `validateUnlock`: `if u == nil { return status.Error(codes.Internal,
  "lock enforcement not configured") }` has no underlying `err` at all — it's a
  nil-dependency configuration check, not a discarded error. Converting it would
  also mean threading `ctx` into a helper shared with `postmortem.go`, for no real
  benefit (the message is already fully descriptive on its own).
- `ai.go`'s `aiErrorToStatus` default case: `status.Error(codes.Internal, "AI
  action failed: "+err.Error())` already embeds `err`'s detail in the
  client-facing message on purpose — the opposite of the swallowed-error pattern.

**Two more sites were never part of the 113 — a related but distinct pattern,
also deliberately left alone:** `task.go`'s `CreateGitHubIssue` has two
`status.Errorf(codes.Internal, "...: %v", err)` sites (a GitHub issue was created
but couldn't be linked to the SEV, or a repo/label call failed outright) that
likewise already expose `err`'s detail to the caller by design — the caller needs
that detail to act on it (e.g. link the issue manually).

**Verification strategy**: this package has no precedent for white-box
(`package grpc`) unit tests — every existing unexported helper (`bindLogger`,
`sensitiveSEVVisible`, `buildSearchFilter`, ...) is exercised indirectly through
the exported handler surface, and `internalError` follows that same convention.
Since the mechanical substitution changes zero observable behavior (same code,
same message, at every site), the primary verification is that the **full existing
test suite passes unmodified** — confirmed after every one of the six commits.
One representative test, `TestGetSEV_StoreError_LogsUnderlyingError`
(`sev_test.go`), demonstrates the new logging behavior end-to-end via a
fault-injecting store wrapper (`erroringSEVStore`, already established in
`share_view_test.go` during Phase 1) — deliberately not duplicated per call site,
since that would mean 111 near-identical tests for one mechanical pattern.

## Prerequisites

- `go build ./... && go test ./...` passing

## Walkthrough

```bash
make up

TOKEN=$(curl -s -X POST http://localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' >/dev/null
curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

curl -s -o /dev/null -H "Authorization: Bearer $TOKEN" http://localhost:8080/v1/sevs/does-not-exist

docker logs sevitout-api-1 --tail 3
```

Expected: a `WARN` `"rpc failed"` line with `"code":"NotFound"` — the client-error
path is untouched by this phase (only `codes.Internal` sites were converted).
Triggering an actual `codes.Internal` path requires a real backing-store failure
(a DB outage, a dropped connection) — see
`TestGetSEV_StoreError_LogsUnderlyingError` for the equivalent exercised via a
fault-injecting store, since the demo stack's own stores don't fail on demand.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

New coverage: `TestGetSEV_StoreError_LogsUnderlyingError` (`sev_test.go`) — the
one representative test described above. No other new tests were added; the
existing suite (unmodified) is the primary regression guard for this phase, since
every converted site's observable behavior (status code, message) is unchanged.

## Known limitations

- Two sites (`ai.go`, `task.go` x2) and one site with no underlying error
  (`sev.go`'s `validateUnlock`) remain on the older pattern, by design — see
  Design notes above for why each doesn't fit this phase's target.
- `internalError` isn't unit-tested directly (no white-box test file) — it's
  exercised indirectly via handler-level tests, following this package's existing
  convention rather than introducing a new one for a single helper.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 3) and
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) (§5) for
the fuller design rationale.
