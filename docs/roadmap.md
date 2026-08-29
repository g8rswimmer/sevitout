# Sevitout — Roadmap

**Version**: 0.1 (living document — update each phase's status line as it ships)
**Status of underlying doc**: unlike `docs/project-plan.md` (a historical record of
the M00–M14 build, now complete — see `README.md`'s Documentation table), this is a
*current* plan. It gets edited in place as work lands, not superseded by a new file.

---

## Where the project is

All 15 planned milestones (M00 through M14, with M14 split into six sub-milestones
M14a–M14f) are complete — every one has a runbook under `demo/`. The architecture
described in `docs/architecture.md` was implemented as designed; there's no
divergence between what was planned and what was built.

The project is now in a post-completion **hardening phase**. Recent work has been
test-coverage additions, pure refactors (extracting shared helpers, splitting large
files by domain), a real security fix (sensitive-SEV visibility — see
`demo/sensitive-sev-visibility.md`), and CI coverage *reporting* with no enforcement
gate yet. Two non-milestone runbooks already exist documenting this phase:
`demo/logging-observability.md` and `demo/sensitive-sev-visibility.md`.

This document lays out what's next: closing observability gaps, tightening
engineering practices, and a short list of concretely-scoped new features. See
[`docs/architecture-evolution.md`](architecture-evolution.md) for the architecture
shape these phases produce.

---

## Phase 0 — `internal/config` package

**Status**: ✅ shipped, see [`demo/config-package.md`](../demo/config-package.md)

Replace the scattered `os.Getenv` calls throughout `cmd/server/main.go` (roughly ten
env vars: `DATABASE_URL`, `JWT_SECRET`, `ALLOW_INSECURE_JWT_SECRET`,
`JWT_TTL_HOURS`, `LOG_LEVEL`, `PAGERDUTY_API_KEY`, `GITHUB_TOKEN`,
`ENCRYPTION_KEY`) with a typed `Config` struct loaded once via `config.Load()` in
`internal/config/config.go` (today just a `.gitkeep` stub).

- `Load()` does `os.Getenv`/`os.LookupEnv` only, returns validation errors (e.g. a
  malformed `JWT_TTL_HOURS`) as a plain `error` — it never calls `os.Exit`, so it
  stays unit-testable and `main()` remains the single place that decides to exit.
- The fail-closed `JWT_SECRET` decision (refuse to start without
  `ALLOW_INSECURE_JWT_SECRET=true` when unset) stays in `main()` as a visible
  business decision — `Load()` just returns the raw values.
- `parseLogLevel` (currently in `cmd/server/main.go`, tested in `main_test.go`)
  moves into this package as `config.ParseLogLevel`.

**Why first**: every later phase adds new env-driven flags (e.g. request-ID header
name, whether `/metrics` is enabled). Doing this after those phases land would mean
touching the same `os.Getenv` scatter pattern twice.

**Estimate**: ~0.5 day. **Depends on**: nothing.

---

## Phase 1 — Request-scoped logging

**Status**: ✅ shipped, see [`demo/request-scoped-logging.md`](../demo/request-scoped-logging.md)

Today, structured logging (`log/slog`, JSON, one `LoggingUnaryInterceptor` in
`internal/api/grpc/logging.go` covering every gRPC call *and* the entire REST
surface via grpc-gateway) is solid, but two things are missing:

1. **No request/correlation ID anywhere** — there's no way to follow one request's
   log lines other than grepping by `method` + a timestamp window, or by `user_id`
   when one's attached.
2. **No context-bound logger** — `internal/auth/context.go` carries
   `*auth.UserContext` (user_id/email/org_role) in `context.Context`, but not a
   logger. Every log call site that wants `user_id` re-derives it by hand via
   `auth.UserFromContext(ctx)` (see `internal/api/grpc/logging.go`'s `logRPC`).

**Design**:

- New package **`internal/telemetry`** (`internal/telemetry/context.go`), parallel
  in shape to `internal/auth/context.go`:
  - `WithRequestID`/`RequestIDFromContext` — same `context.WithValue` pattern.
  - `WithLogger`/`LoggerFromContext` — stash and retrieve a `*slog.Logger` already
    bound with `request_id` and `user_id` via `log.With(...)`. `LoggerFromContext`
    falls back to `slog.Default()` when nothing's attached (e.g. inside
    `internal/ai/dispatcher.go`'s background worker pool, which runs against the
    process-lifetime context, not a single request's).
  - This is a *new* package, not an extension of `internal/auth/context.go`:
    request ID and a bound logger are cross-cutting infra with no relationship to
    authentication, and the plain-`net/http` handlers in `cmd/server/main.go`
    shouldn't need to import `internal/auth` to use them.
- **`RequestIDUnaryInterceptor`** (new file `internal/api/grpc/request_id.go`,
  sibling to `logging.go`) generates a UUID per call (reusing
  `github.com/google/uuid`, already a direct dependency — no new dep needed) or
  reuses one supplied via gRPC metadata / an `X-Request-Id` header forwarded
  through grpc-gateway. It must sit **outside** `auth.UnaryInterceptor` in the
  chain (`cmd/server/main.go`) — this extends the ordering rule already documented
  in `internal/api/grpc/logging.go` (`context.WithValue` only propagates inward, so
  each layer must wrap the one before it): request-ID outermost, then auth, then
  logging. That way even an auth *rejection* — logged directly by
  `auth.authenticate` since it never reaches `LoggingUnaryInterceptor` — carries a
  request ID.
- `LoggingUnaryInterceptor` binds `request_id` + `user_id` into one logger via
  `telemetry.WithLogger` **before** calling the handler (a restructuring from
  today's post-hoc-only `logRPC` call), so every handler can do
  `log := telemetry.LoggerFromContext(ctx)` once and get a logger that already
  carries both fields, instead of re-deriving `user_id` at each call site.
- Extend `loggingMiddleware` (bottom of `cmd/server/main.go`) the same way for the
  three standalone `net/http` handlers (`/ws`, `/admin/integrations/health`,
  `/s/{token}`) — generate/reuse a request ID, bind a logger into the request
  context, echo `X-Request-Id` back in the response header. This also gives
  `share_view.go` and `integrations_health.go` their first `slog` calls ever: today
  both call `http.Error` on failure with zero accompanying log line.
- Bridge `X-Request-Id` through the grpc-gateway `gwMux` (`cmd/server/main.go`) so a
  REST caller's header becomes gRPC metadata `x-request-id`, and
  `RequestIDUnaryInterceptor` honors it instead of minting a fresh one — one
  correlation ID survives the REST→loopback-gRPC hop.

**Demo doc** (once shipped): `demo/request-scoped-logging.md`, following the
existing template. Call out explicitly that background work (the AI dispatcher's
worker pool) only carries whatever request ID was live when the triggering RPC
dispatched it, if threaded through at all — a known limitation, not silently left
ambiguous.

**Estimate**: ~2-3 days. **Depends on**: Phase 0.

---

## Phase 2 — Metrics

**Status**: ✅ shipped, see [`demo/metrics.md`](../demo/metrics.md)

Zero metrics exist today: no `prometheus/client_golang` in `go.mod`, no
OpenTelemetry code (OTel packages are present only transitively in `go.sum`, unused
by any code), no `expvar`.

**Library choice**: `prometheus/client_golang`, not OpenTelemetry. OTel being
transitively present doesn't make it free to adopt — it's still new integration
work, for a heavier framework (SDK + exporter + typically a collector) whose main
value, distributed tracing, doesn't match the near-term need: a scrape target an
ops person points Prometheus/Grafana or `curl` at. Full tracing is an explicit
non-goal for this phase — see `docs/architecture-evolution.md` §7 and
`docs/requirements.md` §19's existing "no full monitoring platform" scope.

**Minimum metric set** — folded into the existing `logRPC`
(`internal/api/grpc/logging.go`) rather than a second interceptor, since it already
resolves `method`/`code`/`dur` once per call:

- `sevitout_rpc_requests_total{method,code}` — counter
- `sevitout_rpc_duration_seconds{method,code}` — histogram
- `sevitout_ws_connections` — gauge, incremented/decremented at the two existing
  connect/disconnect log sites in `internal/api/ws/hub.go`
- `sevitout_ai_action_total{outcome}` — counter (`success`/`error`/`skipped`), at
  the three existing log sites in `internal/ai/dispatcher.go`'s shared `run` core
- `sevitout_db_pool_*` gauges from `pgxpool.Pool.Stat()` (idle/used/max
  connections) — preferred over per-query histograms for now; wrapping every
  sqlc-generated query call individually is a large surface for a speculative win.
  Revisit per-query latency only when a real slow-query investigation needs it.
- `sevitout_open_sevs{severity}` — gauge, refreshed every ~30s by a background
  goroutine reading `stores.SEV.List` filtered to open statuses. The one genuinely
  new "measure something not already computed" item on this list — most cuttable if
  scope needs to shrink.

**Exposure**: `GET /metrics` (`promhttp.Handler()`) added to the existing `httpMux`
in `cmd/server/main.go`, alongside `/admin/integrations/health`. Deliberately
unauthenticated, matching Prometheus scrape convention and the already-open
`/openapi.json` — worth a one-line code comment since most of the surface is
auth-gated.

**Demo doc** (once shipped): `demo/metrics.md`, with a real `curl
localhost:8080/metrics` excerpt, and the `sevitout_open_sevs` staleness window and
deferred per-query histograms noted under Known limitations.

**Estimate**: ~2-3 days. **Depends on**: Phase 1 (shares `logRPC`).

---

## Phase 3 — `codes.Internal` root-cause cleanup

**Status**: ✅ shipped, see [`demo/internal-error-cleanup.md`](../demo/internal-error-cleanup.md)

`internal/api/grpc/*.go` has 119 `status.Error(codes.Internal, "...")` call sites
that discard the underlying error — e.g. `return nil, status.Error(codes.Internal,
"failed to get SEV")` never logs or wraps the real `err`. `LoggingUnaryInterceptor`
only ever sees the already-generic status message, so a DB outage today is nearly
invisible in the logs beyond `code=Internal`.

**Design**: a small helper, `internalError(ctx, msg, err)` (new
`internal/api/grpc/errors.go`, or co-located with `logRPC` in `logging.go`), that
logs `err` via `telemetry.LoggerFromContext(ctx)` at Error level and returns
`status.Error(codes.Internal, msg)` — the same generic `msg` still crosses the
wire; `err`'s detail now reaches the log instead of vanishing. Call sites become a
mechanical substitution:

```go
// before
return nil, status.Error(codes.Internal, "failed to get SEV")
// after
return nil, internalError(ctx, "failed to get SEV", err)
```

**Sequencing** (by call-site density / operational criticality, one PR per group,
each ≤30 line-level changes, independently revertable):

1. `sev.go` + `visibility.go` (18 sites) — highest-traffic path
2. `task.go` + `search.go` (18)
3. `postmortem.go` + `sev_access.go` (15) — sensitive-visibility-adjacent, good to
   harden while that code is fresh from the recent ACL fix
4. `config_*.go` family — `config_ai.go`, `config_service.go`, `config_oncall.go`,
   `config_integration.go`, `config_user.go`, `config_retention.go` (26, one PR
   since already a recognized sibling-file group)
5. `share.go` + `sev_link.go` + `role.go` + `report.go` (20)
6. `chat.go` + `announcement.go` + `ai.go` + `audit.go` + `auth.go` (11, long tail)

**Estimate**: ~1 day per group, spread over following weeks. **Depends on**: Phase
1 (`telemetry.LoggerFromContext` must exist); doesn't need to wait for Phase 2.

---

## Phase 4 — `GET /healthz`

**Status**: not started

An unauthenticated liveness/readiness endpoint, checking DB reachability only
(`Stores.Ping`, a new thin method — no-ops for the in-memory-store dev fallback).
Explicitly distinct from the existing `GET /admin/integrations/health`
(`internal/api/grpc/integrations_health.go`), which is authenticated, admin-only,
and checks *third-party* integration connectivity (PagerDuty/GitHub/Slack) — not
process liveness. `/healthz` is what a container orchestrator's liveness/readiness
probe should point at.

**Estimate**: ~0.5 day. **Depends on**: nothing — natural to batch into the same PR
as Phase 2's `/metrics` endpoint since both are new unauthenticated ops-facing
routes on the same `httpMux`.

---

## Phase 5 — Test coverage + CI gate

**Status**: not started

Test coverage is generally strong (`internal/auth`, `internal/api/grpc`,
`internal/store/postgres`, `internal/ai`, `cmd/slackbot` are all well-tested), with
two real gaps:

- **`internal/store/memory`**: 18 source files, only 2 test files
  (`memory_test.go`, `task_test.go`). Add `_test.go` per untested store,
  prioritized: `sev.go` (core), `sev_access.go` (security-relevant, currently only
  indirectly covered), `share.go`, `oncall.go`, `sli.go`.
- **`internal/sev`**: `id.go` is the only untested file (`statemachine.go` and
  `metrics.go` both have test siblings already) — audit whether it needs a table
  test or is genuinely trivial.
- **`internal/store` root**: `models.go`/`store.go`/`sort.go` against a single test
  file — `sort.go`'s comparison/ordering functions are the likeliest place for
  untested logic that could silently break.

CI (`.github/workflows/backend-ci.yml`, `.github/workflows/frontend-ci.yml`)
already runs coverage and uploads `coverage.out`/`web/coverage/` as artifacts, but
enforces no minimum and no regression gate. Add a gate **after** the above tests
land, not before — measure the actual current aggregate first and set the
threshold at or slightly below it (a gate set above current coverage fails `main`
on day one), then ratchet upward in a follow-up once headroom exists.

**Estimate**: ~2-4 days. **Depends on**: nothing — independent of Phases 0-4, can
run as a parallel workstream.

---

## Phase 6 — New features

**Status**: not started

Ranked by value and how closely an existing pattern in the codebase can be
followed:

1. **Jira integration** (highest value, lowest risk). Mirrors
   `internal/integrations/tasktracker/github/client.go`'s shape almost exactly —
   `Client{baseURL, http}`, `NewClient`/`NewClientWithBaseURL`, an `APIError` type
   — against Jira's REST API v3, using basic-auth-via-API-token instead of a
   bearer token. `internal/api/grpc/task.go`'s `IssueClient` interface likely needs
   generalizing (or a second interface), plus a `taskTrackerFactory` mirroring
   `internal/ai/factory.go`'s provider-switch pattern so `ConfigService` can pick
   GitHub vs. Jira per service. Closes `docs/requirements.md` §13.3's "v2
   fast-follow" (Linear can follow the same shape later). Estimate: ~2-3 days.
2. **Structured monitoring-tool metadata** (the base of §13.4, not the "Future"
   chart-embed part). Today a SEV's detection metadata is free-text alert name +
   tool name + link. Add a `monitoring_tool` enum (`datadog`/`prometheus`/
   `cloudwatch`/`other`) plus structured `dashboard_url`/`query` fields — a schema
   + proto + frontend form change, no new integration client, no live health-check
   or chart embedding (that stays "Future" per requirements). Estimate: ~1-2 days.
3. *(Optional, lower priority)* Recurring/scheduled CSV export (§17 already has
   one-off export from M13). Not recommended for this round — the codebase has no
   existing scheduler/cron precedent to extend, disproportionate effort for a
   hardening phase.

**Explicitly not recommended for this phase**: live GitHub Issue status polling
(§8) and AI semantic search (§12) — both need a new surface (a webhook receiver, or
non-trivial `SearchService` integration work) with no existing pattern to build on,
out of proportion with "hardening phase" scope. Revisit once the observability core
(Phases 0-4) is in place and there's real usage data to justify them.

**Estimate**: Jira ~2-3 days, monitoring metadata ~1-2 days. **Depends on**:
nothing — independent of every other phase.

---

## Sequencing summary

| Phase | Work | Depends on | Estimate |
|---|---|---|---|
| 0 | `internal/config` package | — | 0.5 day |
| 1 | Request ID + `internal/telemetry` context logger | 0 | 2-3 days |
| 2 | Metrics (`prometheus/client_golang`) | 1 | 2-3 days |
| 3 | `codes.Internal` cleanup (6 sub-PRs) | 1 | ~1 day per sub-PR |
| 4 | `GET /healthz` | — (batch with 2) | 0.5 day |
| 5 | Test coverage + CI gate | — | 2-4 days |
| 6 | Jira integration + monitoring metadata | — | 3-5 days |

Phases 0→1→2→3→4 are the observability core and genuinely depend on each other in
that order. Phases 5 and 6 are independent of the observability core and of each
other — run them as a parallel workstream, or sequence after, depending on team
size.

Each phase, once implemented, gets its own `demo/<topic>.md` runbook following the
existing template (What was built / Prerequisites / Walkthrough / Known
limitations — see `demo/logging-observability.md`,
`demo/sensitive-sev-visibility.md`). Update this document's status line per phase
as it ships (e.g. "✅ shipped, see `demo/request-scoped-logging.md`") rather than
rewriting the whole document.

See [`docs/architecture-evolution.md`](architecture-evolution.md) for the resulting
architecture shape.
