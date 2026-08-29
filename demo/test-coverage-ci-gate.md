# Demo — Test Coverage + CI Gate (Roadmap Phase 5)

## What was built

Both CI workflows (`backend-ci.yml`, `frontend-ci.yml`) already ran coverage
and uploaded the report as a build artifact, but enforced no minimum — a
coverage regression would pass CI silently. This phase closed the two real
coverage gaps the roadmap called out, then added an enforced gate to each
workflow measured against the resulting baseline.

**Coverage gaps closed:**

- **`internal/store/memory`** — was already at 94.1% (memory_test.go covered
  far more than the roadmap's original "18 source files, only 2 test files"
  framing suggested — most stores already had happy-path + `ErrNotFound`
  coverage). Closed the remaining gap to **100%**:
  - `StatusHistoryStore` (`internal/store/memory/sev.go`) had *zero* test
    coverage at all — now covers `Create`, the nil-`FromStatus`
    first-transition case, and `ListBySEVID`.
  - Missing `NotFound`/conflict branches across `AIPluginStore`,
    `OnCallStore`, `PostmortemStore`, `ServiceStore` (including the
    name-conflict-on-a-different-ID case, distinct from the same-ID case
    already covered), `TaskStore`, `UserStore` (including the update-time
    email-conflict-with-a-different-user branch).
  - `UserStore.Count` was entirely untested.
  - `ShareStore.Revoke`'s already-revoked no-op branch.
  - `OnCallStore.GetCurrentOnCall`'s override-precedence logic (active
    override wins; an expired one is skipped, falling back to a normal
    rotation; a rotation for a different service is ignored) — this needed
    its own dedicated, multi-rotation test, since the existing
    single-rotation `TestOnCallStore` walk-through wasn't set up to
    exercise it.
  - `SEVStore.List`'s `Limit`-truncation branch and `matchesSEVFilter`'s
    `SeverityLevels` found-a-match branch (the existing filter tests only
    exercised the zero-result path for both).
- **`internal/store` (root)** — `sort.go`'s `SortSEVs`/`sortKeyMissing`/
  `sortLess` were exercised only *indirectly*, via `internal/store/memory`'s
  filter/sort tests — Go's per-package coverage counts only a package's own
  test files, not cross-package callers, so that indirect exercise never
  registered. A new `internal/store/sort_test.go` (pure comparison logic, no
  DB/integration tag needed) tests every `SEVSortField`, nil-value ordering,
  tie-breaking, and the empty-field/unknown-field fallback branches directly
  — **0% → 100%** for the package.
- **`internal/sev/id.go`** — the one remaining untested file in that package
  (`statemachine.go`/`metrics.go` already had test siblings). `FormatID` is
  a thin `fmt.Sprintf` wrapper; a short table test locks in the exact
  zero-padded `SEV-YYYY-NNNN` format cheaply.

**CI gates added, at the post-cleanup baseline minus a buffer** (per the
roadmap: measure first, set at-or-slightly-below — never above, which would
fail `main` on day one):

- **Backend** (`backend-ci.yml`): the coverage step's package list now
  excludes `internal/api/pb` (generated gRPC/protobuf code) and
  `internal/store/postgres` + `internal/store/queries` (sqlc-generated glue,
  already covered separately by the existing DB-backed integration-test
  job) — those three sit at a permanent 0% under a unit-test-only run and
  would dilute the aggregate into a number that doesn't track real
  regressions. A new **Coverage gate** step parses `go tool cover`'s total
  and fails the job below **78.0%** (measured at commit time: ~79.5%).
- **Frontend** (`web/vite.config.ts`): vitest's built-in
  `coverage.thresholds` (global, not per-file — one weak file doesn't block
  the gate on its own) now fails `npm test -- --coverage` below
  **statements 75% / branches 70% / functions 65% / lines 77%** (measured:
  77.86% / 72.58% / 67.63% / 79.82%), replacing the placeholder comment that
  had explicitly deferred this until a baseline existed.

## Design notes

**Why exclude `internal/api/pb`, `internal/store/postgres`,
`internal/store/queries` from the backend gate's denominator, rather than
writing tests for them:** `internal/api/pb` is `protoc`-generated
request/response/service-stub code with no logic of its own to test.
`internal/store/postgres` and `internal/store/queries` (sqlc-generated) are
already covered — by `internal/store/postgres/*_test.go`, gated behind the
`integration` build tag and a real Postgres instance (`make
test-integration`, and the `integration-test` job in `backend-ci.yml`) —
just not by this coverage run, which only ever sees `go test ./...` without
that tag. Including them in the gate's denominator would either fail the
gate permanently on code that's tested elsewhere, or (worse) invite someone
to lower the threshold to compensate, hiding a real regression in code that
*is* in scope.

**Why a global threshold, not per-package/per-file:** matches the roadmap's
own framing ("set the threshold at or slightly below [the aggregate]") and
keeps the gate simple — a per-package minimum would need its own baseline
and rationale per package, which is more machinery than a first gate
warrants. Revisit if one package's coverage starts silently eroding while
others compensate for the aggregate.

**Why buffer below the measured baseline instead of the exact number:** the
aggregate moves a little run to run as unrelated code changes shift the
denominator (e.g. adding an uncovered line elsewhere), even with no coverage
regression in the change that triggered the run. A threshold set at exactly
today's number would intermittently fail unrelated PRs; the buffer absorbs
that without hiding an actual regression, which would still be several
points, not a fraction of one.

## Prerequisites

- `go build ./... && go test ./...` passing (backend)
- `cd web && npm ci` (frontend)

## Walkthrough

```bash
# Backend: reproduce the CI coverage run + gate locally
PKGS=$(go list ./... | grep -v -E '/internal/api/pb$|/internal/store/postgres$|/internal/store/queries$')
go test -race -coverprofile=coverage.out -covermode=atomic $PKGS
go tool cover -func=coverage.out | tail -1
# total:  (statements)  79.5%

# Frontend: reproduce the vitest gate locally
cd web && npm test -- --coverage
# Exits non-zero if any of statements/branches/functions/lines falls below
# its configured threshold in vite.config.ts.
```

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
cd web && npm run lint && npm run build && npm test -- --coverage
```

New coverage: `internal/store/memory/memory_test.go` and `task_test.go`
(gap-closing subtests, see above), `internal/store/sort_test.go` (new),
`internal/sev/id_test.go` (new). `internal/store/memory` and
`internal/store` are now both at 100%; `internal/sev` is at 100%.

## Known limitations

- The backend gate's denominator (post-exclusion) is still not every line
  in the repo that could conceivably be tested — `cmd/server` sits at 23.4%
  (most of `main()` is startup wiring exercised only by running the real
  binary, not unit tests) and pulls the aggregate down; it's included
  rather than excluded because, unlike the three excluded packages, it
  isn't covered anywhere else. Revisit `cmd/server`'s shape (more of its
  logic factored into testable helpers, as `buildStores`/`gatewayMetadata`/
  `loggingMiddleware` already are) as a separate, future improvement — not
  bundled into this phase.
- Both thresholds are deliberately conservative (baseline minus a buffer,
  not the baseline itself) — see Design notes. They will not catch a small
  coverage regression that stays within that buffer; only ratcheting them
  upward over time (once there's real headroom) tightens that.
- The frontend gate is global across the whole `web/` tree, not scoped to
  `src/`; this matches what `npm test -- --coverage` already reports and
  requires no change to reproduce locally.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 5) for the fuller
sequencing rationale.
