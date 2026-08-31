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

**Status**: ✅ shipped, see [`demo/healthz.md`](../demo/healthz.md)

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

**Status**: ✅ shipped, see [`demo/test-coverage-ci-gate.md`](../demo/test-coverage-ci-gate.md)

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

## Phase 6a — Jira integration

**Status**: ✅ shipped, see [`demo/jira-integration.md`](../demo/jira-integration.md)

Highest value, lowest risk of the new-feature candidates — the closest fit to an
existing pattern already in the codebase. Mirrors
`internal/integrations/tasktracker/github/client.go`'s shape almost exactly —
`Client{baseURL, http}`, `NewClient`/`NewClientWithBaseURL`, an `APIError` type —
against Jira's REST API v3, using basic-auth-via-API-token instead of a bearer
token.

- `internal/api/grpc/task.go`'s `IssueClient` interface likely needs generalizing
  (or a second, Jira-specific interface alongside it — `internal/integrations/`
  clients are declared behind interfaces owned by their consumer, per
  `CLAUDE.md`'s Design principles, so this is a design choice made when the
  concrete shape of Jira's create-issue response is in hand, not before).
- A `taskTrackerFactory` mirroring `internal/ai/factory.go`'s provider-switch
  pattern, so `ConfigService` can pick GitHub vs. Jira per service.
- Closes `docs/requirements.md` §13.3's "v2 fast-follow" (Linear can follow the
  same shape later, reusing whatever `IssueClient` generalization this phase
  lands on).

**Estimate**: ~2-3 days. **Depends on**: nothing — independent of every other
phase.

---

## Phase 6b — Structured monitoring-tool metadata

**Status**: ✅ shipped, see [`demo/monitoring-metadata.md`](../demo/monitoring-metadata.md)

The base of `docs/requirements.md` §13.4, not the "Future" chart-embed part.
Today a SEV's detection metadata is free-text alert name + tool name + link. Add
a `monitoring_tool` enum (`datadog`/`prometheus`/`cloudwatch`/`other`) plus
structured `dashboard_url`/`query` fields — a schema + proto + frontend form
change, no new integration client, no live health-check or chart embedding (that
stays "Future" per requirements).

**Also considered and explicitly deferred**, noted here rather than given their
own phase since neither is recommended yet:

- *(Optional, lower priority)* Recurring/scheduled CSV export (§17 already has
  one-off export from M13). Not recommended for this round — the codebase has no
  existing scheduler/cron precedent to extend, disproportionate effort for a
  hardening phase.
- Live GitHub Issue status polling (§8) and AI semantic search (§12) — both need
  a new surface (a webhook receiver, or non-trivial `SearchService` integration
  work) with no existing pattern to build on, out of proportion with "hardening
  phase" scope. Revisit once the observability core (Phases 0-4) is in place and
  there's real usage data to justify them.

**Estimate**: ~1-2 days. **Depends on**: nothing — independent of every other
phase (including Phase 6a; the two can land in either order).

---

## Phase 7 — Linked Issues frontend

**Status**: ✅ shipped, see [`demo/linked-issues-frontend.md`](../demo/linked-issues-frontend.md)

Phase 6a shipped `CreateJiraIssue` on the backend (`POST
/v1/sevs/{sev_id}/jira-issues`) with no frontend caller — flagged as an explicit
Known limitation there. Reviewing the running `TasksPanel.tsx` ("Linked tasks"
panel) surfaced a second, independent gap: `TaskResponse.external_system` is
fetched but never rendered, so a GitHub issue, a Jira issue, and a plain
manually-linked URL are visually indistinguishable in the list — same generic
hyperlink, same generic `ExternalLink` icon, no per-tracker badge or color.

**7a. Create Jira issue from the UI**

- `web/src/types/api.ts`: add `CreateJiraIssueRequest` (`project_key`,
  `issue_type`, `summary`, `description?`, `relationship_type`, `priority`),
  mirroring `CreateGitHubIssueRequest`'s shape and the backend proto exactly.
- `web/src/lib/api.ts`: add `tasks.createJiraIssue(sevId, req)` calling `POST
  /v1/sevs/{sevId}/jira-issues`, mirroring `tasks.createGitHubIssue`.
- `web/src/components/sev/TasksPanel.tsx`: extend `Mode` to `'link' | 'github' |
  'jira'`, add a "Create Jira issue" button alongside "Create GitHub issue", and a
  third form (`project_key`, `issue_type`, `summary`, `description`,
  relationship-type/priority selects reused as-is from the existing forms).
  There's no SEV-level field to pre-fill a default project key the way
  `github_repo`/`parseRepo()` does for GitHub — the field starts empty. Adding a
  `jira_project_key`-equivalent SEV field is a schema/proto change, explicitly
  **out of scope for this phase**; note it as a follow-up rather than scope-creep
  this one.

**7b. Distinguish github / jira / generic in the list**

- `web/src/types/api.ts`: tighten `TaskResponse.external_system` from a bare
  `string` to a union with a fallback (`'github' | 'jira' | 'generic' | string`)
  so display logic has something to switch on while still tolerating any value
  the backend accepts today (it's unvalidated free text server-side).
- `web/src/components/sev/badges.tsx` already holds this exact pattern for other
  fields — `SeverityBadge`/`StatusBadge`, each a thin `<Badge>` wrapper backed by
  a variant/label lookup (`severityVariant()`, `SEV_STATUS_LABELS`/
  `SEV_STATUS_BADGE_CLASS` in `web/src/types/api.ts`). Add an
  `ExternalSystemBadge({ system })` there the same way, backed by a new
  `external_system → { label, badgeClass }` map, and use it in `TasksPanel.tsx`'s
  list render next to each entry's title. No new icon dependency required,
  consistent with how relationship-type/priority/overdue are already communicated
  purely through `Badge` color+text, not icons, everywhere else in this panel. A
  branded-logo treatment (e.g. via `react-icons/si`) is a nicer-to-have,
  explicitly deferred rather than bundled in — the badge approach alone fully
  resolves the reported "hard to tell apart" problem without a new dependency.

**Estimate**: ~1.5-2.5 days (7a the larger half — a third inline form plus request
plumbing; 7b is a typing tighten-up plus a lookup table and one render change).
**Depends on**: Phase 6a (the backend RPC this calls already shipped).

---

## Phase 8 — Datastore-driven Slack bot credentials

**Status**: 🔲 not started. The gap is identified and an interim mitigation is
already shipped — `web/src/pages/admin/AdminIntegrationsPage.tsx`'s Slack entry
carries a `note` explaining the limitation described below, added during the
same investigation that fixed `/ws` returning 500 (a `statusWriter` missing
`http.Hijacker`) and `/sev open`'s invalid `detection_method`.

**The gap**: PagerDuty/GitHub/Jira all prefer datastore-configured credentials
over their static `*_API_KEY`/`*_TOKEN` env vars (env var as fallback), and
pick up an admin's change with no restart — see `cmd/server`'s `*Resolver`
types and `internal/api/grpc/config.go`'s `IntegrationCredentialsRefresher`.
Slack does not: `cmd/slackbot/main.go` builds its real Slack client
(`socketmode.New`, `sevitoutslack.NewClient`) directly from `SLACK_BOT_TOKEN`/
`SLACK_APP_TOKEN` env vars once, at startup. The datastore-configured "slack"
`IntegrationConfig` today only drives the admin health-check widget
(`slackHealthChecker`, which builds its own separate, throwaway client) and
two settings (`default_channel`, `channel_naming_convention`, via
`loadSlackSettings`/`runSettingsRefresher`) — saving a new bot token via the
admin UI has no effect on the running bot process.

**Why this isn't the same fix as the other three**: those resolvers run
*in-process* inside `cmd/server`, with direct access to
`store.IntegrationConfigStore` and the encryption key, so they decrypt
locally and plaintext never leaves that process. `cmd/slackbot` is a separate
binary that only talks to the API over gRPC, and `IntegrationConfigResponse`
deliberately never returns decrypted credentials over that wire (only
`credentials_configured: bool`) — a real security boundary, not an oversight.
Closing the gap means accepting one of two trade-offs: a new gRPC path that
puts plaintext secrets on the wire for the first time in this system, or
giving `cmd/slackbot` direct DB + `ENCRYPTION_KEY` access (making it a second
process with reach into every encrypted secret, not just Slack's).

**Recommended design** — a narrowly-scoped gRPC path, not direct DB access:

- One new RPC, e.g. `ConfigService.GetSlackBotCredential` — **not** a
  generalized "return any integration's decrypted credentials" method.
  Scoping it to exactly this one caller/use case keeps the blast radius of
  "a new way to get plaintext over the wire" as small as possible. It
  decrypts server-side (reusing the existing `DecryptIntegrationCredentials`)
  and returns the credential pair, gated to callers authenticated as the
  specific `SLACKBOT_SERVICE_EMAIL` account — not just any Admin — so an
  unrelated admin session can't casually pull it.
- Extend the "slack" `IntegrationConfig`'s credential map to also carry
  `app_token` alongside `bot_token`, so the whole pair (not just the bot
  token) can be datastore-sourced — symmetric with how Jira's `cloud_id`/
  `site_url` already live together in one config.
- `cmd/slackbot`: fold this call into the existing
  `runSettingsRefresher`/`loadSlackSettings` polling loop (already hits
  `ConfigService` on an interval) instead of adding a second poller. Cache
  the pair behind a mutex, the same pattern `bot.defaultChannel`/
  `channelNamingConvention` already use.
- **The genuinely hard part**: `bot.slack` (the REST client behind
  `CreateChannel`/`PostMessage`/`InviteUsers`) is already an interface, so
  live-swapping it on refresh is small and mechanical — the same shape as
  `cmd/server`'s `*Resolver.apply`. Rebuilding the **Socket Mode** connection
  when the token pair changes is not: `socketmode.New` + `client.RunContext`
  establish one long-lived session at startup, used for slash commands and
  mention events, and there's no existing "tear down and re-establish this
  session" path to reuse. Scope a first version to the REST-client swap alone
  (a real improvement on its own — `CreateChannel`/`PostMessage` stop needing
  a container restart to pick up a rotated token) and treat live Socket Mode
  reconnection as a separate follow-up with its own retry/backoff design.
- Env vars remain the fallback, exactly like the other three integrations: if
  the datastore has nothing usable (not configured, or the RPC fails), fall
  back to `SLACK_BOT_TOKEN`/`SLACK_APP_TOKEN` — preserves today's behavior for
  anyone not using the admin UI at all.

**Estimate**: ~2-3 days for the REST-client swap (new RPC + RBAC gate +
refresher integration + tests). Socket Mode live-reconnection is a separate,
not-yet-estimated follow-up — likely comparable effort on its own, given it
needs a genuinely new retry/backoff design. **Depends on**: nothing
technically; revisit priority once there's a real operational need (e.g.
Slack token rotation becoming a recurring pain point) — today's
restart-to-rotate is a known, tolerable limitation for a self-hosted,
single-org tool.

---

## Phase 9 — Schema-driven integration settings

**Status**: 🔲 not started

Today's `AdminIntegrationsPage.tsx` / `IntegrationConfig` blob has no schema: credentials
and settings are both generic `map[string]string` rows edited through the same
`TagRowsEditor`, so raw storage keys (`bot_token`, `cloud_id`) leak into the UI as field
labels, credential inputs are plain text instead of password-masked, and an "Other…"
option lets an admin create an `integration_type` that no client in the codebase will
ever read. This phase adds a backend-owned field catalog — the single source of truth for
field names, labels, types, and valid values — exposed via a new endpoint and enforced
server-side on upsert, and rebuilds the admin page around a sidebar of the fixed
integration set with a schema-driven detail form per integration. Monitoring (tool type +
base URL, no credentials) is added as a 5th configurable integration, closing a
requirements §18.4 gap that had no UI at all before.

Step 0, before any code: a Claude Design canvas mockup of the sidebar + detail-form layout
(credential fields password-masked, select fields shown as dropdowns), reviewed and
iterated on before backend/frontend work starts.

**9a. Backend: field catalog + upsert validation**

New `internal/integrations/catalog` package — a dependency-free static registry, not a
client for any one integration, so it sits outside `internal/integrations/{slack,pagerduty,...}`
and is importable by `internal/api/grpc` without a cycle:

- `catalog.Field{Key, Label, Kind (text|secret|select), Required, Help, Options}` and
  `catalog.Integration{Type, Label, CredentialFields, SettingsFields}`.
- `catalog.All` — the fixed, ordered set: PagerDuty (`api_key`), GitHub (`token`), Slack
  (`bot_token`; settings `default_channel`, `channel_naming_convention` — carry forward
  today's UI note that these reach the running `cmd/slackbot` process, but the credential
  saved here only powers the connectivity check, since the bot reads its own token from
  its environment at startup), Jira (`api_token`; settings `cloud_id` required, `site_url`
  optional), Monitoring (settings only: `tool` as a select — datadog/prometheus/cloudwatch,
  deliberately *without* an "other" option since there's no base-URL shape to assume for an
  unnamed tool — and `base_url` as text). All storage keys reuse today's convention exactly,
  so no data migration is needed.
- New RPC `GetIntegrationCatalog` → `GET /v1/config/integrations/catalog` (Admin-only, for
  consistency with the rest of `ConfigService`), a pure translation of `catalog.All` with no
  store access.
- `UpsertIntegrationConfig` (`internal/api/grpc/config_integration.go`) validates the
  incoming `integration_type`/credential keys/settings keys/select values against the
  catalog before touching the store or crypto — unknown `integration_type` or unknown key
  rejects the whole request (`codes.InvalidArgument`), matching the file's existing
  all-or-nothing semantics (it already rolls back on refresher rejection). `Required` is
  intentionally *not* enforced at upsert time — since a request can supply just credentials
  or just settings and the other side is left untouched, "required" would have to reason
  about the merged existing+incoming state; the existing fallback-to-static-client
  behavior in `cmd/server/*_resolver.go` already covers "this won't activate without X",
  so `required` stays a UI-only affordance in this phase.

**9b. Frontend: sidebar + schema-driven detail form**

`AdminIntegrationsPage.tsx` is rebuilt around the new catalog endpoint instead of the
hardcoded `KNOWN_INTEGRATIONS` array:

- Left-hand list of the 5 catalog entries (fetched via a new
  `api.config.integrations.catalog()`), each row showing its label, a configured/not-set
  indicator, and its health badge — replacing today's separate "Configured integrations"
  table; selecting a row is now the page's primary navigation, not just a table action.
- Right-hand detail form rendered from the selected integration's schema: credential
  fields as `<Input type="password">` (placeholder communicates "leave blank to keep the
  current value" once something is already configured), settings fields as `<Input>` or,
  for `select`-kind fields (Monitoring's `tool`), the existing `<Select>` component —
  non-secret settings are always shown with their current value, satisfying "the user can
  see current settings except creds."
- `TagRowsEditor` stays for SEV tags elsewhere in the app; only this page's
  credential/settings editing moves off it. The "Other…" branch, `customType` state, and
  the generic key/value rows for known integrations are deleted.
- `types/api.ts` / `lib/api.ts` gain the catalog response types and client method,
  mirroring every other endpoint's existing pattern.

**9c. Tests + demo doc**

- `internal/integrations/catalog/catalog_test.go`: structural sanity checks over
  `catalog.All` (unique types/keys, `select` fields have options, non-select fields don't).
- Extend integration-config handler tests: `GetIntegrationCatalog` shape, and
  `UpsertIntegrationConfig` rejecting an unknown `integration_type`, an unknown
  credential/settings key, and an invalid `select` value, plus a valid Monitoring config
  round-tripping through `List`/`Get`.
- Rewrite `AdminIntegrationsPage.test.tsx` for the new layout: sidebar shows exactly 5
  entries with no "Other…" anywhere; labels render as "Bot Token" not `bot_token`;
  credential inputs are `type="password"`; Monitoring's `tool` renders as a 3-option
  select; leaving a credential blank omits it from the save payload; a server validation
  error surfaces through the existing error-alert path.
- `demo/admin-integrations-settings.md` (What was built / Prerequisites / Walkthrough /
  Known limitations, matching the existing per-phase template). Known limitations must
  restate: GitHub/Jira default-project and PagerDuty default-escalation-policy settings
  are still unsupported (no consumer exists yet); the catalog is static Go code, not
  itself admin-editable, since every entry already has a real client in the codebase and a
  6th integration is a one-file catalog change when it's actually needed; Monitoring has
  no live health check by design (nothing to poll).

**Estimate**: ~3-4 days (≈1.25 days backend catalog/API/validation/tests, ≈1.5-2 days
frontend redesign, ≈0.5-0.75 day frontend tests + demo doc — comparable to Phase 6a and
Phase 7's scopes combined, since this is genuinely both a backend schema/API addition and
a full page redesign). **Depends on**: nothing — Monitoring needs no new backend client,
and PagerDuty/GitHub/Slack/Jira's existing resolvers and health checkers are reused
unchanged.

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
| 6a | Jira integration | — | 2-3 days |
| 6b | Structured monitoring-tool metadata | — | 1-2 days |
| 7 | Linked Issues frontend (create-Jira UI + tracker badges) | 6a | 1.5-2.5 days |
| 8 | Datastore-driven Slack bot credentials (REST client; Socket Mode reconnect deferred) | — | 2-3 days |
| 9 | Schema-driven integration settings (catalog + sidebar admin UI + Monitoring) | — | 3-4 days |

Phases 0→1→2→3→4 are the observability core and genuinely depend on each other in
that order. Phases 5, 6a, and 6b are independent of the observability core and of
each other — run them as a parallel workstream, or sequence after, depending on
team size. Phase 7 is likewise independent of the observability core, but — unlike
6a/6b — it specifically depends on Phase 6a's backend RPC. Phase 8 is independent
of everything above it and, unlike the others, isn't scheduled — it's a scoped
design for a known, currently-tolerated gap, to pick up if/when it becomes an
operational pain point rather than on a fixed timeline. Phase 9 is also independent
of everything above it — it reuses PagerDuty/GitHub/Slack/Jira's existing resolvers
and health checkers unchanged, and Monitoring needs no new backend client — so it
can be sequenced whenever the sidebar-based UI redesign becomes a priority.

Each phase, once implemented, gets its own `demo/<topic>.md` runbook following the
existing template (What was built / Prerequisites / Walkthrough / Known
limitations — see `demo/logging-observability.md`,
`demo/sensitive-sev-visibility.md`). Update this document's status line per phase
as it ships (e.g. "✅ shipped, see `demo/request-scoped-logging.md`") rather than
rewriting the whole document.

See [`docs/architecture-evolution.md`](architecture-evolution.md) for the resulting
architecture shape.
