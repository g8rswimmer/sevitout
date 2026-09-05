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

**Status**: ✅ shipped (REST-client swap only; Socket Mode live-reconnection
remains a follow-up, as scoped below), see
[`demo/datastore-slack-bot-credentials.md`](../demo/datastore-slack-bot-credentials.md).
The gap is identified and an interim mitigation is
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

**Status**: ✅ shipped, see [`demo/admin-integrations-settings.md`](../demo/admin-integrations-settings.md)
(`GetIntegrationCatalog` ended up at `/v1/config/integration-catalog`, not the
`.../integrations/catalog` path below — it collided with
`GetIntegrationConfig`'s path template; see the demo doc's Design notes)

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

## Phase 10 — Per-user integration profiles (Slack / GitHub / Jira identity)

**Status**: ✅ shipped, see [`demo/integration-user-profiles.md`](../demo/integration-user-profiles.md)

Today nothing links a Sevitout user to their Slack account, GitHub username, or Jira
account beyond an email-address coincidence: `inviteOnCall` (`cmd/slackbot/channel.go`)
only invites the **on-call** role, and only resolves an invitee by regex-scraping an
email out of `SEVRole.DisplayName` — a format that happens to hold only because
PagerDuty's auto-assign writes it that way, and silently fails for a manually-typed name
or any other role type. GitHub/Jira issues created from a SEV never get an assignee at
all. This phase adds a self-service "integration identity" (Slack user ID, GitHub
username, Jira account ID) that each user manages for themselves, and uses it to widen
Slack auto-invite to every assigned role, add a manual "add to chat" action, and default
new tracker issues to the creating user.

**10a. Backend: integration-identity data model + self-service API**

- `internal/store/models.go`: add `SlackUserID`, `GitHubUsername`, `JiraAccountID
  *string` to `User`. Nullable, no uniqueness constraint (a stale/duplicate ID just
  resolves to the wrong/no invite — no integrity risk worth enforcing).
- New migration `migrations/000012_integration_identities.up/down.sql`:
  `ALTER TABLE users ADD COLUMN slack_user_id TEXT, ADD COLUMN github_username TEXT, ADD COLUMN jira_account_id TEXT;`
  (down drops all three), following 000002/000003's naming convention.
- `UserStore` (`internal/store/store.go`) gains a **dedicated** method —
  `UpdateIntegrationIdentities(ctx, userID string, slackUserID, githubUsername, jiraAccountID *string) (*User, error)`
  — rather than widening the existing `Update` (which already deliberately excludes
  `Email`/`PasswordHash`). Mirrors how `ConfigService` already prefers narrow methods
  (`UpdateUserRole`, `Deactivate/ReactivateUser`) over one generic mutator. Implement in
  `internal/store/postgres/user.go` and the in-memory test fake.
- Extend `AuthService` (not `ConfigService` — that's the Admin-only "manage other users"
  surface, and this is a write-side sibling of the existing self-identity RPC `WhoAmI`,
  which already acts purely on the caller via `auth.UserFromContext(ctx)`):
  - `rpc UpdateMyIntegrationIdentities(UpdateMyIntegrationIdentitiesRequest) returns (WhoAmIResponse)`
    → `PATCH /v1/auth/me`. **Full-replace semantics** (all 3 fields sent every call,
    empty string clears that field) — a deliberate deviation from `UpdateSEV`'s
    sparse-patch convention, since this endpoint only ever touches these 3 fields and
    "empty = leave alone" would make clearing one impossible.
  - `WhoAmIResponse` gains `slack_user_id`, `github_username`, `jira_account_id` so the
    profile page pre-fills off the same call it already makes today. (`oauth_provider` on
    this message is a stale leftover from removed OAuth — pre-existing, not touched here.)
  - RBAC: `"/sevitout.v1.AuthService/UpdateMyIntegrationIdentities": store.OrgRoleViewer`,
    same floor as `WhoAmI`.
- New minimal directory RPC, needed by both 10c and 10d (neither can use the Admin-gated
  `ConfigService.ListUsers`): `AuthService.ListUserDirectory(query, ids) →
  {users: [{id, name, email, slack_user_id}]}`, `store.OrgRoleViewer`. Deliberately
  narrower than `ConfigService.ListUsers` — an org-wide "who is this person" lookup, not
  a user-management surface.

**10b. Frontend: My Profile page**

- New `web/src/pages/ProfilePage.tsx`, route `/profile` in `App.tsx`'s authenticated
  route tree (sibling of `/reports`/`/sevs` — not under `/admin/*`, Viewer-floor
  self-service).
- Nav entry point: `AppLayout.tsx` has no user-menu today, just a name/role label and a
  Logout icon button — add a second icon button (`UserCog` from `lucide-react`) linking
  to `/profile`, matching that existing minimal pattern.
- Page: read-only Name + Email (from `WhoAmIResponse`), 3 editable inputs (Slack User ID,
  GitHub Username, Jira Account ID) with short help text, Save calling
  `UpdateMyIntegrationIdentities`, inline success/error feedback.
- **Explicitly out of scope this phase**: name/avatar/password editing — flagged as a
  known limitation/follow-up, not silently expanded into.

**10c. `RolesPanel.tsx`: user picker (hard dependency for 10d/10e)**

Without this, `SEVRole.UserID` is never set by the current UI, and "every assigned role"
auto-invite has nothing to resolve beyond today's DisplayName-regex path for anything but
on-call — this sub-step is required, not optional polish, and is sized into the estimate.

- Assign form gains an optional user combobox (built on `ListUserDirectory`'s `query`
  search) alongside the existing free-text `display_name` input — picking a real user
  sets `user_id` on the `AssignRole` call (the field already exists end-to-end). The
  free-text-only path stays fully supported; nothing is removed.
- `web/src/lib/api.ts` / `types/api.ts` gain the directory response types/method.

**10d. Slack: widen SEV-creation auto-invite to every assigned role**

- `cmd/slackbot/channel.go`: generalize `inviteOnCall` → `inviteRoleHolders`, dropping
  the `RoleType == store.SEVRoleOnCall` filter so every role type is covered.
- Resolution order per role, replacing today's regex-only path:
  1. `SEVRole.UserID` set → batch-resolve via `ListUserDirectory(ids: [...])` (one call
     for all roles, not one per role) → if that user has a stored `SlackUserID`, use it.
  2. Else (UserID set, no stored `SlackUserID`) → `LookupUserIDByEmail(user.Email)`, as today.
  3. Else (no `UserID`, e.g. an older or free-text-only assignment) → fall back to
     today's `emailInAngleBrackets` regex scrape of `DisplayName` — kept, not deleted.
  4. Else → skip, as today.
- `cmd/slackbot/apiclient.go`: extend `roleAPI` (or add a narrow `directoryAPI`
  interface, per the file's consumer-owned-interface convention) with
  `ListUserDirectory`.

**10e. Slack: manual "add to chat" action**

Persist the SEV→channel mapping (currently in-memory only in `cmd/slackbot`, an accepted
v1 limitation per `demo/M11-slack-bot.md`) so `cmd/server` can act on it directly:

- Add `SlackChannelID *string` to `store.SEV` + a nullable `sevs.slack_channel_id`
  column (same or a sibling migration to 10a's).
- `cmd/slackbot/channel.go`'s `createIncidentChannel`, right after recording the channel
  locally, also calls `UpdateSEV{id: sevID, slack_channel_id: channelID}` (safe due to
  sparse-patch semantics) — a bonus side effect that finally closes the in-memory-only
  limitation, for SEVs created after this ships (older SEVs have no retroactive value;
  the button must handle that as disabled, not an error).
- New RPC `RoleService.InviteRoleToSlack(sev_id, role_id) → google.protobuf.Empty`, RBAC
  `store.OrgRoleResponder` (auxiliary invite action, not role *management* — lower than
  `AssignRole`/`RemoveRole`'s IC floor). Implemented in `internal/api/grpc/role.go`,
  reusing 10d's identity-resolution order server-side: reads `SEV.SlackChannelID`
  (`codes.FailedPrecondition` if nil, so the UI can grey the button out), builds a
  `slack.Client` from the config-store's decrypted `bot_token` (same pattern as
  `slackHealthChecker`), calls `InviteUsers` directly — no round trip through the live
  bot process. A small (~20-line) resolver duplicated between `cmd/slackbot` and
  `internal/api/grpc/role.go` is an accepted trade-off for two call sites in two
  different binaries, not worth a shared package until a third consumer appears.
- `SEVResponse` gains `slack_channel_id` (not sensitive — raw IDs are already exposed
  elsewhere) so the frontend knows whether to enable the button.
- `RolesPanel.tsx` gets a per-role-row "Add to chat" icon button next to Remove,
  disabled when `SEV.slack_channel_id` is unset.

**10f. GitHub/Jira: default assignee on issue creation**

- Proto: `CreateGitHubIssueRequest` gains `string assignee = 8;` (wrapped into GitHub's
  `assignees: []string` at the client layer). `CreateJiraIssueRequest` gains `string
  assignee_account_id = 8;` (sent as Jira's `assignee: {accountId: "..."}`). `TaskResponse`
  gains `string assignee = 11;` so the Tasks list can show it without a live re-fetch.
- `internal/integrations/tasktracker/github/client.go`'s `CreateIssueRequest` gains
  `Assignees []string`; `.../jira/client.go`'s gains `AssigneeAccountID *string`, sent
  only when non-nil.
- `internal/api/grpc/task.go` handlers pass the field straight through — no server-side
  default-injection; all defaulting happens client-side.
- `TasksPanel.tsx`: both create-issue forms gain an editable "Assignee" input, pre-filled
  from the already-fetched `WhoAmIResponse` (via `useAuth()`) when present,
  clearable/editable before submit, omitted from the payload when empty.
- **UX risk, surfaced not solved here**: Jira Cloud account IDs are opaque strings not
  visible in Jira's UI without hitting `/rest/api/3/user/search?query=email` — and the
  Jira client has no user-search method today (only `Ping/GetIssue/CreateIssue`). Most
  users won't know their own account ID. The demo doc documents a manual workaround; an
  email→accountId lookup is a named follow-up, not built in this phase.

**10g. Tests + demo doc**

- Go: store tests for `UpdateIntegrationIdentities`; `AuthService` handler tests for
  `UpdateMyIntegrationIdentities` (full-replace/clear, RBAC floor) and
  `ListUserDirectory` (query/ids filtering); `RoleService` tests for
  `InviteRoleToSlack` (resolution precedence, `FailedPrecondition` on no channel,
  Responder floor); `cmd/slackbot` tests for generalized `inviteRoleHolders` (every role
  type, UserID-first order, regex fallback preserved, skip-on-no-match) and the
  `UpdateSEV` write-back; tracker-client tests for the new request fields
  serializing/omitting correctly; `task.go` tests extended for assignee passthrough.
- Frontend: new `ProfilePage.test.tsx`; `RolesPanel.test.tsx` extended for the picker and
  "Add to chat" enabled/disabled states; `TasksPanel.test.tsx` extended for assignee
  pre-fill/clear/omit; `AdminUsersPage.test.tsx` extended for a new read-only
  "Integrations" column (dot/badge per identity set, no edit control).
- `demo/integration-user-profiles.md` (existing template): walkthrough sets a user's
  identities, assigns several role types (via picker and free-text), shows auto-invite
  covering all of them, demonstrates "Add to chat" for a role added after channel
  creation, creates a GitHub/Jira issue showing assignee pre-fill. Known limitations
  restate: name/avatar/password out of scope; no Jira account-ID lookup helper; "Add to
  chat" only works for SEVs whose channel was created after this ships; the role picker
  is optional, not mandatory.

**Estimate**: ~6-8 days (10a ~1 day, 10b ~0.75-1 day, 10c ~1-1.25 days, 10d ~0.75-1 day,
10e ~1.25-1.5 days, 10f ~1 day, 10g distributed + ~0.5 day demo doc) — larger than Phase
9 because this phase touches six independent layers (DB, two extended/new gRPC surfaces,
two tracker clients, the `cmd/slackbot` binary, two frontend surfaces) rather than one
backend/frontend pair. **Depends on**: nothing already in the roadmap (Phase 9 is
unrelated). 10c is an internal hard dependency for 10d/10e.

**Possible split if too large for one PR/phase**: pull **10f** into its own follow-up —
it only needs 10a's `User` fields and `WhoAmIResponse` extension, no dependency on
10c/10d/10e, and is the most self-contained piece. A more aggressive trim would also defer
**10e** — the secondary use case in the stated goal, and the riskiest sub-piece (new
persisted `SEV` field + a new direct-Slack-API path in `cmd/server`) — while 10d alone
already delivers the higher-value "auto-invite every role" outcome.

---

## Phase 11 — Integration-aware SEV UI

**Status**: ✅ shipped, see [`demo/integration-aware-sev-ui.md`](../demo/integration-aware-sev-ui.md)
(11d shipped early, folded into Phase 10 — see
[`demo/integration-user-profiles.md`](../demo/integration-user-profiles.md)'s
"10h" section)

The SEV detail page shows every integration-tied action unconditionally today —
`TasksPanel.tsx` always offers "Create GitHub issue" and "Create Jira issue" even
when only one (or neither) tracker is configured, and there's no way for a
non-Admin to even ask "is X configured" (every existing status surface is
Admin-gated). Separately, Phase 10's Slack-invite work covers assigned roles but
never the person who actually opened the SEV, and there's no self-service way for
whoever's viewing a SEV to join its Slack channel on their own. This phase closes
all three gaps: a viewer-safe "enabled integrations" signal that hides
unconfigured actions, a self-service "Join Slack channel" button, and
auto-inviting the SEV creator when the incident channel is created.

**11a. Backend: viewer-safe "enabled integrations" signal**

- New RPC `ConfigService.ListEnabledIntegrations() → ListEnabledIntegrationsResponse{repeated string enabled_types}`,
  RBAC `store.OrgRoleViewer` — a distinct, narrowly-scoped RPC on the existing
  service (matching how `ChatService.ListChatEntries` sits at Viewer floor
  alongside `ChatService`'s higher-gated `AddChatEntry`; RBAC is per-RPC, not
  per-service). Returns **only** a list of type strings — no settings values, no
  `credentials_configured` per type, nothing an Admin-only endpoint wouldn't
  already consider safe to hand to any Viewer.
- Backed by `IntegrationConfigStore.List()`, filtered to rows with meaningful
  configuration (`len(EncryptedCredentials) > 0`, or for settings-only types like
  Monitoring, `len(Settings) > 0`) — the same "configured" concept Phase 9's admin
  UI already uses.
- **Known limitation, stated explicitly rather than solved here**: this reflects
  store-configured integrations only. PagerDuty/GitHub/Jira can also activate via
  static env-var fallback with zero store rows (see `cmd/server`'s `*Resolver`
  pattern) — such a setup would show as "not enabled" here and hide its SEV-page
  action even though the backend would actually serve the request. Acceptable
  given Phase 9 already steers configuration toward the admin UI as the primary
  path; a fully accurate signal would need each resolver to expose its own
  `Active()` state, a bigger change not justified by a UI-hiding feature alone.

**11b. Frontend: gate SEV-page integration actions by enabled status**

- New shared query (e.g. `useEnabledIntegrations()`, React Query, cached) calling
  the new RPC, used by `TasksPanel.tsx` and wherever Slack actions live.
- `TasksPanel.tsx`: "Create GitHub issue" renders only when `'github'` is enabled,
  "Create Jira issue" only when `'jira'` is enabled — if only Jira is configured,
  GitHub's option simply isn't offered (the concrete case named in this phase's
  request). If neither tracker is enabled, only "Link existing" (which needs no
  integration) remains.
- Slack-tied actions — Phase 10e's per-role "Add to chat" button and this phase's
  11c "Join Slack channel" button — render only when `'slack'` is enabled **and**
  `SEV.slack_channel_id` is set (Phase 10e's field).

**11c. Backend + frontend: self-service "Join Slack channel"**

- New RPC `RoleService.JoinSlackChannel(sev_id) → google.protobuf.Empty`, reusing
  `RoleServer`'s Phase-10e-added Slack dependencies (`IntegrationConfigStore`,
  `Encryptor`, Slack client construction) and 10d/10e's identity-resolution order,
  scoped to the caller (`auth.UserFromContext(ctx)`) instead of a role holder:
  stored `SlackUserID` → `LookupUserIDByEmail(caller's own email)` →
  `codes.FailedPrecondition` ("no Slack identity on file — set one in your
  profile") if neither resolves. `codes.FailedPrecondition` also when
  `SEV.SlackChannelID` is unset (no channel to join).
- **Security gate, not present in Phase 10's design and worth calling out
  explicitly**: before resolving/inviting, check the caller has full (non
  visibility-restricted) access to the SEV via `store.SEVAccessStore` — the same
  check sensitive-SEV field-level visibility already relies on elsewhere.
  Self-service Slack join must not become a side-channel around sensitive-SEV
  restrictions, since Slack channel membership itself isn't gated by Sevitout
  RBAC once granted. `codes.PermissionDenied` when the caller lacks full access
  to a sensitive SEV.
- RBAC floor: `store.OrgRoleViewer` (any authenticated user with real access to
  the SEV, gated further by the access check above — not an Incident-Commander
  or Responder-only action).
- Frontend: a "Join Slack channel" button (SEV detail page, near the Slack/chat
  area — exact placement decided alongside 10e's "Add to chat" button since
  they're visually adjacent), gated per 11b.

**11d. Slack: invite the SEV creator when the channel is created**

**Status**: ✅ shipped early (see `demo/integration-user-profiles.md`'s "10h"
section) — reported as a bug immediately after Phase 10 shipped ("a user
with a Slack identity isn't invited to a SEV's channel unless also assigned
a role"), and pulled forward since it depended on nothing else in Phase 11.

- `cmd/slackbot/notify.go`: add `CreatedBy string \`json:"created_by"\`` to
  `sevPayload` (currently silently dropped by `json.Unmarshal` since the field
  isn't declared) so `handleSEVCreated` actually has it.
- Thread `sev.CreatedBy` through to `createIncidentChannel`'s invite-building
  step.
- Resolve `CreatedBy` (a Sevitout user ID) via the directory lookup this phase
  requires from Phase 10a: `ListUserDirectory(ids: [CreatedBy])` → stored
  `SlackUserID` if present, else the returned email → `LookupUserIDByEmail`.
- **Combine into one invite call**: build a single invite list per
  channel-creation event — Phase 10d's role-holder invites + this creator invite
  + `takePendingOpener`'s Slack-native ID (the existing `/sev open` path) —
  deduped by resolved Slack user ID, then one `InviteUsers` call (today's
  `inviteOnCall` already does a single call for its narrower role-holder-only
  list; this just widens what feeds it).
- **Known limitation, documented not fixed**: for SEVs opened via `/sev open`,
  `CreatedBy` is not currently set to the human's Sevitout identity (the
  slash-command handler never sets `created_by` on `CreateSEVRequest`) — the
  creator-invite resolves to the bot's service account for that path (a harmless
  no-op/skip), not a duplicate of the human already invited via
  `takePendingOpener`. Threading the real identity through `/sev open` is a
  named follow-up, out of scope here.

**11e. Tests + demo doc**

- Go: `ConfigService` test for `ListEnabledIntegrations` (Viewer floor, correct
  filtering of credential-only vs. settings-only vs. unconfigured rows);
  `RoleService` test for `JoinSlackChannel` (resolution order, `FailedPrecondition`
  on missing channel/identity, `PermissionDenied` on restricted sensitive-SEV
  access); `cmd/slackbot` test for creator-invite resolution and the deduped
  combined-invite-list construction, including the `/sev open` no-op case.
- Frontend: `TasksPanel.test.tsx` extended for conditional button rendering
  (Jira-only, GitHub-only, neither, both); a test for the new "Join Slack
  channel" button's gating and success/error states.
- `demo/integration-aware-sev-ui.md` (existing template): walkthrough configures
  only Jira and shows GitHub's create-issue option absent; unconfigures Slack and
  shows chat actions absent; creates a SEV as a non-role-holder and shows they're
  auto-invited to the channel; demonstrates "Join Slack channel" for a different
  viewer. Known limitations restate: env-var-only integration activation isn't
  reflected in `ListEnabledIntegrations`; `/sev open`-originated SEVs don't
  attribute `CreatedBy` to the human opener yet.

**Estimate**: ~3.5-4.5 days (11a ~0.5 day, 11b ~0.75-1 day, 11c ~1-1.25 days, 11d
~0.75-1 day, 11e ~0.5-0.75 day) — smaller than Phase 10 since it's UI-gating plus
two focused additions layered on infrastructure Phase 10 already builds, not new
infrastructure of its own. **Depends on**: **Phase 10** (hard dependency — needs
`User.SlackUserID`/etc., `ListUserDirectory`, `SEV.SlackChannelID`, and
`RoleServer`'s Slack-client dependencies; must be sequenced strictly after, not
just related to, Phase 10). Loosely benefits from Phase 9's "configured" concept
but doesn't hard-block on it.

---

## Phase 12 — Per-service SLA targets and breach indicators

**Status**: ✅ shipped, see [`demo/service-sla-breach-indicators.md`](../demo/service-sla-breach-indicators.md)

SEVs compute MTTD/MTTM/MTTR (`internal/sev/metrics.go`) but nothing defines what
those numbers *should* be, and there's no way to tell at a glance whether a SEV is
on track. This phase adds per-service, per-severity-level SLA targets for the three
headline metrics, and a live breach indicator wired into the SEV response and UI —
modeled directly on the existing Task "Overdue" badge, which already derives its
status from `now` on every read rather than trusting a stored flag
(`internal/api/grpc/task.go`'s `isOverdue`/`computeDueDate`, rendered via
`TasksPanel.tsx:185-189`'s `<Badge variant="destructive"><AlertTriangle />Overdue</Badge>`).
DTTM is excluded — it's a secondary derived metric (detect→mitigate), not one of
the three targets a team sets an SLA against.

**12a. Backend: schema + store**

- New table `service_slas` (migration `000016_service_sla.{up,down}.sql`): `id
  BIGSERIAL PK`, `service_id TEXT REFERENCES services(id) ON DELETE CASCADE`,
  `severity_level SMALLINT CHECK (BETWEEN 1 AND 4)`, three nullable
  `*_target_seconds BIGINT` columns (`mttd_target_seconds`, `mttm_target_seconds`,
  `mttr_target_seconds`), `created_at`/`updated_at`, `UNIQUE (service_id,
  severity_level)`. A nil target column means "no SLA set for this metric" — not
  an instant breach.
- New `store.ServiceSLA` struct in `internal/store/models.go`, alongside `Service`
  (`internal/store/models.go:409-420`) and mirroring `RetentionConfig`'s doc-comment
  style (`internal/store/models.go:470-481`).
- New `store.ServiceSLAStore` interface in `internal/store/store.go`, next to
  `ServiceStore` (`internal/store/store.go:107-114`):
  `Upsert(ctx, *ServiceSLA) error`, `Get(ctx, serviceID string, severityLevel
  int16) (*ServiceSLA, error)`, `Delete(ctx, serviceID string, severityLevel
  int16) error`, `ListByService(ctx, serviceID string) ([]*ServiceSLA, error)`
  (admin editor), `ListForServices(ctx, serviceIDs []string, severityLevel int16)
  ([]*ServiceSLA, error)` (the batch lookup breach evaluation needs).
- In-memory fake `internal/store/memory/service_sla.go`, following
  `internal/store/memory/service.go`'s `sync.RWMutex` + defensive-copy shape;
  Postgres implementation `internal/store/postgres/service_sla.go` + sqlc queries
  in `internal/store/sql/service_slas.sql`, matching `services.sql`'s
  `-- name: X :one/:many/:exec` convention.

**12b. Backend: SLA evaluation domain logic**

- New `internal/sev/sla.go`, alongside `metrics.go` and `statemachine.go`.
- `SLATargets{MTTDTargetSeconds, MTTMTargetSeconds, MTTRTargetSeconds *int64}` and
  `MostStrictSLA(rows []*store.ServiceSLA) SLATargets` — reduces every attached
  service's SLA row at the SEV's severity level to one effective target per
  metric by taking the minimum non-nil value per metric ("if a SEV has multiple
  services, the most strict SLAs should be used"). A service with no row for that
  severity level simply doesn't participate.
- `SLAMetricStatus` string enum: `not_applicable` (no target configured, or no
  baseline timestamp yet), `ok`, `at_risk` (still in progress, elapsed already
  exceeds target), `breached` (final timestamp recorded and exceeded target).
- `EvaluateSLA(s *store.SEV, targets SLATargets, now time.Time) SLAEvaluation`,
  returning per-metric status plus `Overall` (worst of the three). Per metric,
  same shape as MTTD/MTTM/MTTR's own nil-safety in `ComputeMetrics`: if the final
  `*Seconds` value is already set, compare it to target (`breached` if over, else
  `ok`); if not set but the baseline timestamp (`StartedAt`) is, compare
  `now.Sub(StartedAt)` to target (`at_risk` if over, else `ok`); if no baseline
  yet, `not_applicable`. This means an at-risk MTTD flips cleanly to
  `breached`/`ok` the moment `DetectedAt` is finally recorded — no separate
  transition logic needed.

**12c. Backend: API surface**

- `proto/sevitout/v1/sev.proto`: new `message SLAStatus` (`mttd`, `mttm`, `mttr`,
  `overall` status strings + the three resolved `*_target_seconds`, 0 = not
  applicable), added to `SEVResponse` at the next free field number (after
  `slack_channel_id`, currently ending at 38 — confirm exact number at
  implementation time).
- `proto/sevitout/v1/config.proto`: `ServiceSLAResponse`,
  `UpsertServiceSLARequest`, `GetServiceSLARequest`, `DeleteServiceSLARequest`,
  `ListServiceSLAsRequest`/`Response` — same shape as the existing
  `Service*`/`RetentionConfig*` messages (`config.proto:243-290`).
- `ConfigServer` (`internal/api/grpc/config_service.go`) gets a new
  `service_sla.go` file with `UpsertServiceSLA`, `GetServiceSLA`,
  `DeleteServiceSLA`, `ListServiceSLAs`, following `CreateService`/`GetService`'s
  exact error-mapping pattern (`store.ErrNotFound` → `codes.NotFound`, etc.).
  RBAC additions in `internal/auth/rbac.go` (next to the `ConfigService/*Service`
  block, `internal/auth/rbac.go:92-96`): `GetServiceSLA`/`ListServiceSLAs` →
  `store.OrgRoleViewer` (matches `GetService`/`ListServices`, since the same
  numbers are already implicitly exposed to any Viewer via `SEVResponse.sla_status`
  below), `UpsertServiceSLA`/`DeleteServiceSLA` → `store.OrgRoleAdmin` (matches
  `UpdateService`/`DeleteService`).
- `SEVServer` (`internal/api/grpc/sev.go`) gets a new `serviceSLAs
  store.ServiceSLAStore` dependency, wired in `cmd/server`. In every handler that
  builds a `SEVResponse` (`GetSEV`, `ListSEVs`, `CreateSEV`, `UpdateSEV`,
  `TransitionStatus` — the existing `sev.ComputeMetrics(record)` call sites at
  `internal/api/grpc/sev.go:269,511,753` plus the plain-read path): look up
  `serviceSLAs.ListForServices(ctx, record.AffectedServices,
  record.SeverityLevel)`, reduce via `sev.MostStrictSLA`, evaluate via
  `sev.EvaluateSLA(record, targets, time.Now())`, attach as `SLAStatus` alongside
  the existing `sevToProto(record)` call (`internal/api/grpc/sev.go:818`) rather
  than folding the store lookup into `sevToProto` itself, keeping that function
  free of I/O.
- **Accepted tradeoff, stated explicitly**: `ListSEVs` does one small indexed
  `ListForServices` query per returned SEV rather than a single batched query —
  matches this codebase's existing lack of batching elsewhere (e.g. Create's
  single on-call lookup, `internal/api/grpc/sev.go:305-306`) and is bounded by
  page size. A batched variant (one query per distinct severity level in the
  page, filtered client-side per SEV) is a named follow-up if list-page latency
  becomes a real problem.

**12d. Frontend: admin SLA editor**

- New `web/src/pages/admin/AdminServiceSLAPage.tsx` (or a per-service expandable
  panel reached from `AdminServicesPage.tsx`), built directly on
  `AdminRetentionPage.tsx`'s existing 4-row-per-severity-level table pattern
  (`web/src/pages/admin/AdminRetentionPage.tsx:12,84-128`: one row per `SEV-{1..4}`,
  numeric inputs, a per-row Save button, per-row inline error) — swapping
  "retention days / hard delete" for three numeric target inputs
  (MTTD/MTTM/MTTR, displayed in minutes, converted to/from seconds at the API
  boundary).
- `web/src/lib/api.ts`: add `api.config.serviceSLA.{list, upsert, delete}`,
  mirroring the existing `api.config.retention.*` calls.
- `web/src/types/api.ts`: add `ServiceSLAResponse`, `SLAMetricStatus = 'ok' |
  'at_risk' | 'breached' | 'not_applicable'`, `SLAStatus`, and extend
  `SEVResponse` (`web/src/types/api.ts:198-253`) with `sla_status?: SLAStatus`.

**12e. Frontend: SEV breach indicators**

- New `SLABadge({ status }: { status?: SLAMetricStatus })` in
  `web/src/components/sev/badges.tsx`, alongside `SeverityBadge`/`StatusBadge`
  (`web/src/components/sev/badges.tsx:12-18`): renders nothing for
  `ok`/`not_applicable`/undefined, an amber "At risk" badge for `at_risk`, a
  `variant="destructive"` "SLA breached" badge (with `AlertTriangle`, matching
  `TasksPanel.tsx`'s Overdue badge) for `breached`.
- `web/src/components/sev/LifecyclePanel.tsx`: render `<SLABadge
  status={sev.sla_status?.mttd} />` etc. next to each of the existing
  `MetricField`s (`LifecyclePanel.tsx:108-110`), so a breach is visible right on
  the metric it belongs to.
- `web/src/pages/SevDetailPage.tsx` and `web/src/pages/SevListPage.tsx`: an
  overall `<SLABadge status={sev.sla_status?.overall} />` rendered next to the
  existing `SeverityBadge`/`StatusBadge` pair, so a breach is visible without
  opening the SEV.

**12f. Tests + demo doc**

- Go: `internal/sev/sla_test.go` for `MostStrictSLA` (multi-service min-reduction,
  partial-configuration cases) and `EvaluateSLA` (all four status outcomes per
  metric, the at-risk→breached/ok transition once the final timestamp lands);
  `internal/store/memory/service_sla_test.go`; `config_service_test.go` additions
  for the new RPCs' RBAC floors and not-found/conflict mapping; `sev_test.go`
  additions asserting `SEVResponse.sla_status` reflects the most-strict target
  across two attached services.
- Frontend: `AdminServiceSLAPage.test.tsx` (new, mirrors
  `AdminRetentionPage.test.tsx`); `badges.test.tsx` additions for `SLABadge`'s
  three render states.
- `demo/service-sla-breach-indicators.md` (existing template: What was built /
  Prerequisites / Walkthrough / Known limitations). Known limitations to restate:
  an `AffectedServices` entry that doesn't resolve to a real `Service.ID` (loose
  free-text entry, `web/src/components/sev/ServiceChipEditor.tsx:9-12`) is
  silently excluded from the most-strict computation, same as it's already
  excluded from `ServiceStore.Get` lookups elsewhere; `ListSEVs`'s per-row SLA
  lookup isn't batched (12c).

**Also considered and explicitly deferred**:
- An org-wide default/fallback SLA for SEVs with no attached service, or attached
  services with no configured row — out of scope; no target means no indicator,
  full stop, for this phase.
- Automated breach notifications (e.g. a Slack ping when a SEV crosses `at_risk`)
  — this phase is indicators-only; escalation/notification is a separate future
  phase if wanted.
- SLA compliance reporting/analytics (e.g. "% of SEVs within SLA this quarter") —
  out of scope; this phase is per-SEV live status only, not an aggregate rollup.

**Estimate**: ~4.5-6 days (12a ~0.75-1 day, 12b ~0.5 day, 12c ~1-1.5 days, 12d
~0.75-1 day, 12e ~0.75-1 day, 12f ~0.5-0.75 day). **Depends on**: nothing — reuses
the existing `Service` registry and `SEVServer`/`ConfigServer` wiring unchanged;
independent of Phases 8-11's integration work, so it can be sequenced whenever SLA
tracking becomes a priority.

---

## Phase 13 — Per-service SLA compliance reporting

**Status**: ✅ shipped, see [`demo/service-sla-compliance-reporting.md`](../demo/service-sla-compliance-reporting.md)

Phase 12 added per-service SLA targets and a live, per-SEV breach indicator
(`internal/sev/sla.go`'s `MostStrictSLA`/`EvaluateSLA`, `SEVResponse.sla_status`)
but explicitly deferred the aggregate view: *"SLA compliance
reporting/analytics (e.g. '% of SEVs within SLA this quarter') — out of
scope; this phase is per-SEV live status only, not an aggregate rollup"*
(docs/roadmap.md Phase 12, "Also considered and explicitly deferred"). This
phase closes that gap: for each service and severity level, over a
selectable trailing window (30/60/90/180 days), show how many SEVs
occurred, how many were within SLA, and the average MTTD/MTTM/MTTR — landing
in `ReportService` (`internal/api/grpc/report.go`,
`proto/sevitout/v1/report.proto`) alongside the existing dashboard/trends/
export RPCs (docs/requirements.md §17), extending its existing
`frequencyByServiceAndLevel`-style per-(service, severity) grouping rather
than introducing a new aggregation pattern. The frontend table is built
first against sample data (13a) and reviewed before the backend contract is
written, so column/shape changes stay cheap while nothing downstream
depends on them yet.

**13a. UX demo: static mockup, reviewed before backend work starts**

- Build `web/src/components/reports/ServiceSLAComplianceTable.tsx` (the
  30/60/90/180-day button-group selector plus the table itself — see 13e for
  its final column set) against a hardcoded fixture array shaped like the
  planned `ServiceLevelMetrics[]`, covering a handful of services across
  multiple severities with varied compliance percentages, at least one row
  with no SLA configured (`not_applicable`), and the table's empty state —
  no backend call, no proto/RPC yet, just a local `useState` fixture in
  place of the eventual `useQuery`.
- Added after the initial mock review, then revised once more per a second
  round of feedback: a **Service** and a **Severity** filter, each a
  button (`"All service"`/`"Severity (N)"`-style summary label) that opens
  a checkbox-list popover — `MultiSelectDropdown`, a new local component in
  the same file, no Radix (matching `select.tsx`/`checkbox.tsx`/
  `dialog.tsx`/`tooltip.tsx`'s existing "plain element over a new
  dependency" convention: a plain positioned `<div>` under the trigger
  button, closed on outside click or Escape). Checkbox semantics inside the
  popover are the same as `SevListPage.tsx`'s existing severity/status
  filters (empty selection = no filter, otherwise narrow to the checked
  set) — only the presentation collapsed from an always-visible row into a
  dropdown, to keep two multi-value filters from crowding the card's header
  next to the window selector. Each popover also has a "Select all"/"Clear"
  pair above the checkbox list (a third round of feedback) — "Select all"
  disables once every option is already checked, "Clear" disables once
  nothing is, so neither ever offers a no-op. Service options come from the same service
  registry `ReportsPage.tsx` already fetches for its `serviceName()` lookup
  (no second fetch); severity options are the fixed SEV-1..4 set. Both
  filter the fixture rows client-side in this mock (13e wires the service
  filter into the real request; see there for why severity stays
  client-side even after that). A distinct "No SEVs match the selected
  filters" message covers the filtered-to-empty case, separate from the
  window's own "No SEVs opened in the selected window" empty state.
- Wire it into `web/src/pages/ReportsPage.tsx` under the same `<Section
  title="SLA Compliance by Service">` wrapper the final version will use, so
  it's reviewable live in the running app (`npm run dev`) rather than as a
  disconnected mock.
- Walk through it for feedback — column set, severity/compliance formatting,
  empty state, window-selector and filter interaction — and iterate
  directly on the fixture-driven component. Once the layout is approved,
  its exact shape becomes the literal template for 13b's proto message and
  13d's `ServiceLevelMetrics` type, rather than the other way around.
- No dependency on Phase 12 or any other 13-lettered step — it's pure
  frontend against fixture data, so it can start immediately regardless of
  where Phase 12 or the rest of this phase stands.

**13b. Backend: proto + aggregation domain logic**

- `proto/sevitout/v1/report.proto`: new `GetServiceMetricsRequest{ int32
  window_days = 1; repeated string service_ids = 2; }` (`window_days` one of
  30/60/90/180, default 30 — validated in the handler, not the proto);
  `ServiceLevelMetrics{ service_id, severity_level, sev_count,
  avg_mttd_seconds, avg_mttm_seconds, avg_mttr_seconds, sla_ok_count,
  sla_at_risk_count, sla_breached_count, sla_not_applicable_count,
  compliance_pct }`; `ServiceMetricsResponse{ repeated ServiceLevelMetrics
  service_level_metrics = 1; int32 window_days = 2; }` (echoing back the
  resolved window, same defensive pattern `MTTRTrend.window_days` uses so
  the frontend never has to track what it asked for separately from what it
  got). New RPC `GetServiceMetrics` on `ReportService`, `GET
  /v1/reports/service-metrics`.
- `internal/api/grpc/report.go`: `ReportServer` gains a `serviceSLAs
  store.ServiceSLAStore` field (`NewReportServer` gets a fourth param),
  mirroring `SEVServer`'s existing `serviceSLAs` dependency
  (`internal/api/grpc/sev.go`).
- New `serviceLevelMetrics(records []*store.SEV, slaLookup
  map[serviceLevelKey]*store.ServiceSLA, now time.Time)
  []*pb.ServiceLevelMetrics` next to `frequencyByServiceAndLevel`: groups by
  `serviceLevelKey{service, level}` exactly like that function, but per
  group accumulates SEV count, per-metric sums/sample-counts for
  MTTD/MTTM/MTTR averages (nil-safe — only completed values contribute, same
  discipline as `mttrTrends`), and calls `sev.EvaluateSLA(r,
  sev.MostStrictSLA(rows), now)` per SEV, where `rows` is a 1-element slice
  containing `slaLookup[key]` **only when that key is present** — and empty
  otherwise. **Correction from an earlier draft of this section**: `rows`
  must never contain a nil element. `sev.MostStrictSLA` dereferences every
  row it's given, so passing `[]*store.ServiceSLA{slaLookup(service,
  level)}` when the lookup misses (nil) would panic, not degrade gracefully
  — there is no "nil-tolerant reduction" for a nil *element*, only for an
  *empty slice* (a missing service simply not participating). Bucket the
  per-SEV result into `ok`/`at_risk`/`breached`/`not_applicable` via
  `.Overall`. `compliance_pct` is `ok / (ok + at_risk + breached)`, `0` when
  that denominator is `0`.
- Handler builds `slaLookup` efficiently: collect the distinct severity
  levels present in the filtered SEV set (≤4), call
  `serviceSLAs.ListForServices(ctx, serviceIDsAtThatLevel, level)` once per
  level, and index the results into a `map[serviceLevelKey]*store.ServiceSLA`
  — at most 4 store round-trips regardless of how many SEVs are in the
  window, an improvement on `sevToProtoWithSLA`'s already-accepted per-SEV
  tradeoff (12c) rather than a repeat of it.
- Window filtering reuses `store.SEVFilter{StartedAfter: now.AddDate(0, 0,
  -windowDays), ServiceIDs: req.GetServiceIds(), ExcludeSensitive: true,
  Limit: reportFanoutLimit}` — same shape `ExportSEVs` already builds, same
  `reportFanoutLimit` guard as `fetchAllSEVs`.

**13c. Backend: RBAC + wiring**

- `internal/auth/rbac.go`: `GetServiceMetrics` → `store.OrgRoleViewer`, next
  to `GetDashboardMetrics`/`GetSEVTrends`/`ExportSEVs`'s existing "all
  read-only, same Viewer floor" comment block.
- `cmd/server/main.go`: pass the already-constructed `store.ServiceSLAStore`
  (already wired for `SEVServer` per Phase 12) into the `NewReportServer(...)`
  call site.

**13d. Frontend: types + API client**

- `web/src/types/api.ts`: `ServiceLevelMetrics`, `ServiceMetricsResponse`,
  matching the proto shape (field names as returned by grpc-gateway's default
  JSON, snake_case, same convention every other type in this file already
  follows).
- `web/src/lib/api.ts`: `api.reports.serviceMetrics: (windowDays?: 30 | 60 |
  90 | 180, serviceIds?: string[]) => request<ServiceMetricsResponse>(...)`,
  next to `dashboardMetrics`/`trends`.

**13e. Frontend: wire the reviewed mockup to the real API**

- `ServiceSLAComplianceTable.tsx` (built in 13a) swaps its fixture
  `useState` array for `useQuery({ queryKey: ['reports', 'service-metrics',
  windowDays, selectedServiceIds], queryFn: () =>
  api.reports.serviceMetrics(windowDays, selectedServiceIds.length ?
  selectedServiceIds : undefined) })` — 13a's service filter now drives the
  real `service_ids` request field (already planned above in 13b/13d)
  instead of filtering an already-fetched response, so checking a service
  narrows what's fetched, not just what's displayed. The severity filter
  stays client-side exactly as in the 13a mock: a window's response already
  contains every severity level in one call, and the set is a fixed four,
  so there's no round-trip to save by adding a server-side filter for it.
  The JSX/columns themselves don't change unless the 13a review asked for
  it. Table columns (as approved in 13a): Service | Severity | SEV Count |
  Avg MTTD | Avg MTTM | Avg MTTR | Compliance — same `overflow-x-auto` +
  `border-b border-border` table shell `ServiceHeatmap`/
  `RecurringPatternsTable` already use in this file, durations formatted
  with the existing `lib/format.ts` helper the rest of the app uses for
  MTTD/MTTM/MTTR display. Compliance is a plain percentage cell; no new
  badge component needed since this is an aggregate rate, not a live
  per-SEV status (`SLABadge` stays scoped to individual SEVs, per Phase
  12e).
- Confirm the fixture's shape from 13a matches the real
  `ServiceMetricsResponse` from 13d one-for-one; if the backend ended up
  differing from what was mocked, reconcile here rather than carrying a
  silent mismatch.

**13f. Tests + demo doc**

- Go: `internal/api/grpc/report_test.go` additions — `serviceLevelMetrics`
  with multiple services/severities, partial-metric SEVs (nil MTTM etc. not
  skewing other averages), no-SLA-configured rows landing in
  `sla_not_applicable_count`, and the window cutoff boundary (a SEV just
  outside `window_days` excluded); RBAC floor test for `GetServiceMetrics`.
- Frontend: `ReportsPage.test.tsx` additions for the new section, including
  a window-switch triggering a refetch with the new `window_days` param, a
  service-filter selection triggering a refetch with `service_ids` set, and
  a severity-filter selection narrowing rendered rows without triggering a
  refetch.
- `demo/service-sla-compliance-reporting.md` (existing template: What was
  built / Prerequisites / Walkthrough / Known limitations). Known
  limitations to state: compliance is a point-in-time snapshot over the
  selected window, not a historical trend (a compliance-over-time chart is
  explicitly deferred, see below); an `AffectedServices` entry that doesn't
  resolve to a real `Service.ID` is silently excluded, same accepted gap as
  Phase 12 — including from the service filter itself, which only lists
  services present in the registry (`ConfigService.ListServices`).

**Also considered and explicitly deferred**:
- A time-series compliance trend (e.g. weekly compliance % over the last
  quarter, mirroring `mttrTrends`'s rolling-window shape) — this phase is a
  snapshot over one selected window, not a trend; a natural follow-up once
  this snapshot view is validated.
- CSV/JSON export of the aggregated per-service-per-severity rows —
  `ExportSEVs` already covers raw per-SEV record export; a dedicated
  aggregate export can be added later if requested, out of scope here.
- Per-user or per-team breach leaderboards — out of scope; this phase
  aggregates by (service, severity) only, matching
  `frequencyByServiceAndLevel`'s existing grouping.
- Automated alerts when compliance drops below a threshold — out of scope;
  no notification layer exists yet, same reasoning Phase 12 gave for
  deferring automated breach notifications.

**Estimate**: ~3.5-5.25 days (13a ~0.75-1 day, 13b ~1-1.5 days, 13c ~0.25-0.5
day, 13d ~0.25-0.5 day, 13e ~0.5-0.75 day, 13f ~0.75-1 day). **Depends on**:
Phase 12 for 13b onward — reuses `service_slas`/`ServiceSLAStore` and
`internal/sev/sla.go`'s `MostStrictSLA`/`EvaluateSLA` directly, so those
steps cannot be built before Phase 12 ships. 13a is the exception: it's
pure frontend against fixture data, so it has no dependency and can start
immediately. Independent of Phases 8-11's integration work.

---

## Phase 14 — Per-service SEV leveling criteria

**Status**: ✅ shipped, see [`demo/per-service-leveling-criteria.md`](../demo/per-service-leveling-criteria.md)

SEVs use the SEV-1 through SEV-4 taxonomy with generic, org-wide descriptions
(`docs/requirements.md` §3: "Total outage or data loss," "Significant
degradation," etc.), but what those descriptions mean in practice varies by
service — a payments service's SEV-1 bar (say, ">50% of checkout traffic
failing") looks nothing like an internal admin tool's. Phase 12/13 built SLA
targets and compliance reporting that key off whatever severity level a SEV
is assigned (`internal/sev/sla.go`'s `MostStrictSLA`/`EvaluateSLA`,
`ServiceSLAResponse`), so picking the *right* level for a given service
matters — an incident leveled too low silently gets a looser SLA target than
it should, one leveled too high gets held to a bar it never needed. This
phase adds a second, independent per-(service, severity) table holding
free-text guidance authored by team leads, surfaced next to the severity
picker on SEV creation and again, read-only, on the postmortem page so the
assigned level can be sanity-checked against the service's own criteria
during writeup. **This is guidance only and is never validated or
enforced** — nothing blocks opening or transitioning a SEV based on it; see
"Also considered and explicitly deferred" below.

It deliberately does not extend `service_slas` (Phase 12): that table holds
numeric operational thresholds evaluated by `sla.go` on every SEV read,
owned by the same lifecycle as breach computation; this table holds
descriptive text authored and read by humans, never evaluated by any domain
logic, and its own row's lifecycle (create/edit/delete guidance) has nothing
to do with whether an SLA target exists for that same (service, severity)
pair. Sharing a table would couple two concerns that should be free to
change independently — e.g. deleting a criteria row should never risk
touching an SLA row's `updated_at` or vice versa.

**14a. Backend: schema + store**

- New table `service_leveling_criteria` (migration
  `000019_service_leveling_criteria.{up,down}.sql`, the next free migration
  number after `000018_rtpc_column_rename_repair`): `id BIGSERIAL PK`,
  `service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE`,
  `severity_level SMALLINT NOT NULL CHECK (severity_level BETWEEN 1 AND 4)`,
  `criteria TEXT NOT NULL`, `created_at`/`updated_at TIMESTAMPTZ NOT NULL
  DEFAULT NOW()`, `UNIQUE (service_id, severity_level)` — same granularity
  and constraint shape as `service_slas` (`migrations/000016_service_sla.up.sql`),
  but one free-text column instead of four numeric target columns. Also add
  `CREATE INDEX IF NOT EXISTS idx_service_leveling_criteria_service_id ON
  service_leveling_criteria(service_id)`, matching
  `idx_service_slas_service_id`. Down: `DROP TABLE IF EXISTS
  service_leveling_criteria;`. `criteria` is `NOT NULL` rather than
  nullable-with-empty-meaning-unset (unlike `service_slas`'s nullable target
  columns) — a row only exists when there's something to say; "no guidance
  configured" is simply "no row," enforced by the handler rejecting an
  empty/whitespace-only `criteria` on upsert (14b) rather than by the schema
  allowing an empty-string row to sit alongside "no row" as a second way to
  mean the same thing.
- New `store.ServiceLevelingCriteria` struct in `internal/store/models.go`,
  alongside `ServiceSLA` (`internal/store/models.go:429-446`), with a doc
  comment in the same style — cite this phase, state plainly that this is
  advisory text never read by any evaluation logic (unlike `ServiceSLA`,
  which `EvaluateSLA` dereferences), and cross-reference the two consuming
  frontend surfaces (`SevCreatePage.tsx`, `PostmortemPage.tsx`) so a future
  reader doesn't go looking for a `sla.go`-style consumer that doesn't
  exist.
- New `store.ServiceLevelingCriteriaStore` interface in
  `internal/store/store.go`, next to `ServiceSLAStore`
  (`internal/store/store.go:116-130`): `Upsert`, `Get`, `Delete`,
  `ListByService(ctx, serviceID string) ([]*ServiceLevelingCriteria, error)`
  (admin editor, 0-4 rows ordered by severity level), and
  `ListForServices(ctx, serviceIDs []string, severityLevel int16)
  ([]*ServiceLevelingCriteria, error)` — a **no-reduction** batch read
  (contrast `ServiceSLAStore.ListForServices`, which feeds `MostStrictSLA`'s
  min-across-services reduction), since guidance text for multiple services
  is just listed side-by-side, never collapsed to one value. No
  `internal/sev`-side reduction helper is needed at all — this store method
  alone is sufficient for both frontend consumers.
- In-memory fake `internal/store/memory/service_leveling_criteria.go`,
  copying `internal/store/memory/service_sla.go`'s shape exactly: a
  `{serviceID, severityLevel}` map key, `sync.RWMutex`, `atomic.Int64` ID
  sequence, `Upsert` preserving the existing row's `ID`/`CreatedAt` on
  update, `ListByService` iterating severity levels 1-4 in order,
  `ListForServices` iterating the given `serviceIDs` in order and skipping
  any with no row (never a nil element in the returned slice). Postgres
  implementation `internal/store/postgres/service_leveling_criteria.go` +
  sqlc queries in `internal/store/sql/service_leveling_criteria.sql`,
  mirroring `service_sla.go`/`service_slas.sql`'s `-- name: X
  :one/:many/:exec` convention 1:1, with `criteria` replacing the four
  `*_target_seconds` columns and upsert via `ON CONFLICT (service_id,
  severity_level) DO UPDATE SET criteria = EXCLUDED.criteria, updated_at =
  NOW() RETURNING id`. Run `make generate` (sqlc) after adding the `.sql`
  file.

**14b. Backend: API surface**

- `proto/sevitout/v1/config.proto`: new RPC block, "Per-service SEV leveling
  criteria (docs/roadmap.md Phase 14)," inserted right after the SLA block
  (`config.proto:52-79`), following that block's own RBAC-comment
  convention (`:52-57`): reads Viewer+ (same reasoning as
  `GetServiceSLA`/`ListServiceSLAs` — exactly what `SevCreatePage.tsx`/
  `PostmortemPage.tsx` need for any authenticated user opening or reviewing
  a SEV), writes Admin-only. RPCs: `GetLevelingCriteria`/
  `UpsertLevelingCriteria`/`DeleteLevelingCriteria`/`ListLevelingCriteria`
  at `/v1/config/services/{service_id}/leveling-criteria[/{severity_level}]`
  (GET/PUT/DELETE/GET, mirroring the SLA block's URL shape exactly), plus
  `ListLevelingCriteriaForServices` at a top-level `/v1/config/leveling-criteria`
  (GET, `repeated string service_ids` + `severity_level` query params —
  grpc-gateway maps a repeated field to repeated query params automatically)
  since it spans multiple services rather than nesting under one. Messages
  (`LevelingCriteriaResponse{service_id, severity_level, criteria,
  created_at, updated_at}` plus one request message per RPC) placed next to
  the `ServiceSLA*` block (`:322-366`). Run `make proto` after editing to
  regenerate `internal/api/pb`.
- New sibling handler file `internal/api/grpc/config_leveling_criteria.go`
  (alongside `config_service_sla.go`, 139 lines), following its exact
  shape: `GetLevelingCriteria`/`UpsertLevelingCriteria` (validates
  `service_id` non-empty, severity level via the existing
  `validateSeverityLevel` helper in `internal/api/grpc/config_retention.go:74`,
  confirms the referenced service exists via `s.services.Get` before
  upserting — same "an orphaned row could never be resolved" reasoning
  `UpsertServiceSLA` gives, plus rejects an empty/whitespace-only `criteria`
  with `codes.InvalidArgument`, since clearing existing guidance is
  `DeleteLevelingCriteria`'s job, not an empty upsert's, matching the
  `criteria TEXT NOT NULL` schema decision in 14a)/`DeleteLevelingCriteria`/
  `ListLevelingCriteria` (mirror `GetServiceSLA`/`UpsertServiceSLA`/
  `DeleteServiceSLA`/`ListServiceSLAs`'s `store.ErrNotFound` →
  `codes.NotFound` mapping and `internalError(ctx, ...)` fallback 1:1) plus
  a new `ListLevelingCriteriaForServices` (no service-existence check
  needed — purely a read, silently omits any service ID with no configured
  row) and a `levelingCriteriaToProto` helper mirroring
  `serviceSLAToProto`.
- `ConfigServer` struct and `ConfigServerParams`
  (`internal/api/grpc/config.go:57-70,79-89`) each get a new field:
  `levelingCriteria`/`LevelingCriteria store.ServiceLevelingCriteriaStore`,
  positioned right after the existing `serviceSLAs`/`ServiceSLAs` fields.
- RBAC additions in `internal/auth/rbac.go`, immediately after the existing
  `ServiceSLA` block (`internal/auth/rbac.go:102-105`), same comment style:
  `GetLevelingCriteria`/`ListLevelingCriteria`/
  `ListLevelingCriteriaForServices` → `store.OrgRoleViewer`,
  `UpsertLevelingCriteria`/`DeleteLevelingCriteria` → `store.OrgRoleAdmin`.
- Server wiring in `cmd/server/main.go`: `Stores` struct gets
  `LevelingCriteria store.ServiceLevelingCriteriaStore` next to the
  existing `ServiceSLA` field (`:463`); memory instantiation
  `memory.NewServiceLevelingCriteriaStore()` next to
  `memory.NewServiceSLAStore()` (`:512`); Postgres instantiation
  `postgres.NewServiceLevelingCriteriaStore(pool)` next to
  `postgres.NewServiceSLAStore(pool)` (`:540`); passed into
  `NewConfigServer`'s params next to `ServiceSLAs: stores.ServiceSLA`.
  **Unlike Phase 12's `ServiceSLAStore`, this store is *not* also wired
  into `NewSEVServer` or `NewReportServer`** — `SEVResponse` gains no new
  field and `ReportService` computes nothing from this table; both frontend
  consumers (14d) call `ConfigService` RPCs directly, since there is no
  server-side evaluation step analogous to `EvaluateSLA` that would need
  this data attached to a SEV read.

**14c. Frontend: admin editor UI**

- `web/src/lib/api.ts`: add `api.config.levelingCriteria.{list, upsert,
  delete, listForServices}`, mirroring `api.config.serviceSLA.*`
  (`web/src/lib/api.ts:439-448`) — `listForServices(serviceIds: string[],
  severityLevel: number)` builds the repeated-query-param GET request
  against `/v1/config/leveling-criteria`.
- `web/src/types/api.ts`: add `LevelingCriteriaResponse`,
  `ListLevelingCriteriaResponse`, `UpsertLevelingCriteriaRequest`,
  `ListLevelingCriteriaForServicesResponse`, matching the proto shape —
  none of these fields are proto3 int64 (unlike `ServiceSLAResponse`'s
  `*_target_seconds`), so there's no string-vs-number serialization quirk
  to account for here.
- New `web/src/components/admin/LevelingCriteriaEditor.tsx`, built directly
  on `ServiceSLAEditor.tsx`'s structure (`web/src/components/admin/ServiceSLAEditor.tsx`):
  same `useQuery`/per-row `forms`/`errors` state keyed by severity level,
  same `upsertMutation`/`clearMutation` pair (`clearMutation` calling
  `delete`, shown only when a row already has a saved value) — but each
  `SEVERITY_LEVELS` row renders a single `<Textarea aria-label={`Leveling
  criteria for SEV-${level}`}>` bound to a plain string instead of
  `ServiceSLAEditor`'s four numeric `<Input>` columns, so no
  `hoursToSeconds`-style unit conversion is needed. Reuse `SEVERITY_LEVELS`
  from `@/lib/slaTargets.ts` directly rather than duplicating the same
  `[1, 2, 3, 4]` constant in a new file — its current doc comment ties it
  rhetorically to SLA forms, but the value itself is generic; leave a
  one-line note in the new file explaining the reuse. Intro text above the
  table (mirroring `ServiceSLAEditor.tsx`'s helper paragraph) should say
  plainly that this is guidance shown on SEV creation and the postmortem
  page, and is never enforced.
- `web/src/pages/admin/AdminServicesPage.tsx`: add a second per-row icon
  button next to the existing "Manage SLAs" `Gauge` button (`:404-407`
  region) — e.g. a `NotebookText`/`ListChecks` icon from `lucide-react`,
  `aria-label={`Leveling criteria for ${svc.name}`}` — toggling a second,
  independent `criteriaServiceId` state (parallel to the existing
  `slaServiceId` at `:59`, same "separate concerns, not mutually exclusive"
  comment already there for `slaServiceId` vs `editingId`). When toggled,
  render `<LevelingCriteriaEditor serviceId={svc.id} />` in a second
  expandable `<tr colSpan={7}>` following the existing conditional-row
  pattern. Two independent sibling icon toggles is the recommended shape
  over inventing a tab strip inside one expanded row — there's no existing
  precedent in this codebase for tabs within an expanded admin-table row,
  whereas a second parallel toggle is a same-page, zero-new-pattern change,
  and both panels being independently and simultaneously openable costs
  nothing.

**14d. Frontend: SEV-creation-form + postmortem-page surfacing**

- New shared read-only `web/src/components/sev/LevelingCriteriaPanel.tsx`,
  taking `{ severityLevel: number; serviceIds: string[] }`. Fetches via
  `useQuery({ queryKey: ['levelingCriteria', serviceIds, severityLevel],
  queryFn: () => api.config.levelingCriteria.listForServices(serviceIds,
  severityLevel), enabled: serviceIds.length > 0 })`. Renders nothing when
  `serviceIds` is empty (no services picked/attached yet — nothing to show
  guidance for); once populated, one small block per returned row (service
  name — resolved the same way `ServiceChipEditor.tsx` already does, via
  the service registry query it fetches — plus the `criteria` text); a
  quiet "No leveling criteria configured for the selected service(s) at
  SEV-{level}" note when the query succeeds but returns zero rows, so an
  empty result reads as "nothing configured" rather than looking broken.
  Built as one shared component (not duplicated per page) since both call
  sites need identical fetch/empty-state/render logic and only differ in
  which props they pass — consistent with this codebase's existing
  shared-component convention (`SLABadge`, `ServiceChipEditor`).
- `web/src/pages/SevCreatePage.tsx`: render `<LevelingCriteriaPanel
  severityLevel={severityLevel} serviceIds={affectedServices} />` directly
  below the "Affected services" field (`:102-105`), inside the same `Card`,
  so it re-queries live as the reporter changes either the severity
  `<Select>` (`:87-95`) or the `ServiceChipEditor` selection — the goal is
  to help pick the right level *while filling out the form*, not after
  submission.
- `web/src/pages/PostmortemPage.tsx`: render it as a new read-only
  `<Section title="Leveling criteria reference">` placed before the
  existing `<AIDraftPanel sevId={sevId} ... />` (`:182`), passing
  `severityLevel={record.severity_level}` and
  `serviceIds={record.affected_services ?? []}` from the SEV record already
  fetched by this page's `sev` `useQuery`. Purely informational here — no
  edit affordance, no call to action, nothing gating
  `PostmortemStatusControl`'s transitions — consistent with the confirmed
  "guidance only, never enforced" decision; it exists to help whoever is
  writing the postmortem visually compare "here's what SEV-{level} is
  supposed to mean for this service" against what actually happened, not to
  block anything.

**14e. Tests + demo doc**

- Go: `internal/store/memory/memory_test.go` additions —
  `TestServiceLevelingCriteriaStore` mirroring `TestServiceSLAStore`
  (`internal/store/memory/memory_test.go:1145`): Upsert/Get/Delete/
  ListByService/ListForServices, including ListForServices skipping a
  service ID with no row and preserving the given ID order.
  `internal/api/grpc/config_test.go` additions mirroring the existing
  `TestUpsertServiceSLA_*`/`TestGetServiceSLA_*`/`TestDeleteServiceSLA_*`/
  `TestListServiceSLAs_*` functions (`config_test.go:1201-1345`):
  `TestUpsertLevelingCriteria_Valid`, `_UnknownService`,
  `_InvalidSeverityLevel`, `_EmptyCriteriaRejected` (this feature's analog
  to `TestUpsertServiceSLA_ZeroFieldClearsTarget`, but the opposite outcome
  — an empty submission is rejected outright rather than treated as "clear
  this field," per 14b's schema/handler decision), `TestGetLevelingCriteria_NotFound`,
  `TestDeleteLevelingCriteria_Valid`/`_NotFound`,
  `TestListLevelingCriteria_ReturnsOnlyConfiguredSeverityLevels`,
  `TestListLevelingCriteriaForServices_SkipsUnconfiguredServices`; RBAC
  floor coverage for all five RPCs (Viewer can read, Admin required to
  write, matching this file's existing RBAC-floor test pattern for the
  `ServiceSLA` RPCs).
- Frontend: `web/src/pages/admin/AdminServicesPage.test.tsx` additions
  covering the new "Leveling criteria" toggle and inline
  `LevelingCriteriaEditor` render/save/clear flow, mirroring this file's
  existing `ServiceSLAEditor`-related assertions. New
  `LevelingCriteriaPanel.test.tsx` covering the empty (`serviceIds=[]`),
  populated (multi-service), and zero-rows-returned render states.
  `SevCreatePage.test.tsx` additions asserting the panel refetches when
  severity or affected-services state changes; `PostmortemPage.test.tsx`
  additions asserting the panel renders using the SEV's own
  severity/affected-services and never renders any edit control.
- `demo/per-service-leveling-criteria.md` (existing template: What was
  built / Prerequisites / Walkthrough / Known limitations). Known
  limitations to state: exactly the same `AffectedServices`-entry-that-
  doesn't-resolve-to-a-real-`Service.ID` gap Phase 12/13 already accept
  (`ServiceChipEditor.tsx:9-12`'s free-text entries are silently excluded
  from `ListLevelingCriteriaForServices`, same as they're excluded from
  `ServiceStore.Get` lookups elsewhere); the guidance is explicitly never
  validated against the chosen severity — a SEV can be opened, transitioned,
  and resolved at any level regardless of what the criteria panel shows, by
  design.

**Also considered and explicitly deferred**:

- **Enforcing or validating the chosen severity against configured
  criteria** (e.g. a warning or a required override reason when a SEV's
  level looks inconsistent with the service's own text) — explicitly not
  wanted; this phase is guidance surfaced to a human, not a gate.
  Automating a judgment call this qualitative and context-dependent is also
  a poor fit for hard validation regardless.
- **An org-wide default/fallback criteria set** for services with no rows
  configured — out of scope; no configured criteria simply means the panel
  shows nothing (or its "no criteria configured" note), same posture Phase
  12 took for services with no SLA row.
- **Versioning/history of criteria changes** (e.g. seeing that "checkout"'s
  SEV-1 bar changed from ">30%" to ">50%" last quarter) — `Upsert` is a
  destructive full-replace with no audit trail beyond the row's own
  `updated_at`; a real history/diff view is a reasonable future addition if
  criteria start changing often enough to matter, but adds real schema
  complexity (either an append-only log table or reusing the existing audit
  log) that isn't justified yet.
- **AI-assisted "does this look like the right level" suggestion** — a
  natural future tie-in to the existing AI plugin system
  (`internal/ai`/`internal/api/grpc/config_ai.go`, which already drives
  postmortem draft suggestions via `AIDraftPanel`), comparing a SEV's
  actual impact description against its service's configured criteria text
  and flagging a likely mismatch. Deliberately out of scope here — this
  phase only builds the raw guidance data and its two display surfaces;
  wiring an AI plugin action on top of it is a separate, later phase that
  can be scoped once this data actually exists to point one at.

**Estimate**: ~3-4.25 days (14a ~0.5-0.75 day, 14b ~0.75-1 day, 14c
~0.5-0.75 day, 14d ~0.75-1 day, 14e ~0.5-0.75 day) — smaller than Phase 12
since there's no evaluation/reduction domain logic to write (no
`sla.go`-equivalent) and no response-shape change to `SEVResponse`, just a
new table, a straightforward CRUD+batch-read API surface, an admin editor
built by copying an existing pattern, and two read-only display surfaces.
**Depends on**: nothing — like Phase 12, it only needs the existing
`Service` registry (`ServiceStore`/`services` table) and the
`ConfigServer`/`AdminServicesPage.tsx` wiring, both already in place
independent of Phase 12/13's work. It shares no table, no store interface,
and no domain logic with `service_slas`/`sla.go` — the two phases are
thematically related (both are per-service, per-severity-level
configuration reached from the same admin page) and conventionally
sequenced near each other in this document for that reason, but neither is
a hard prerequisite for the other; Phase 14 could equally have shipped
before Phase 12, or in parallel with Phase 13.

---

## Phase 15 — Notifications & Alerting

**Status**: ✅ shipped, see [`demo/notifications-alerting.md`](../demo/notifications-alerting.md)

`docs/requirements.md` §16 and §18.5 are the last major unimplemented section
of the original functional spec: no `NotificationConfig` RPC/service and no
admin page exist for any of it today. A `notification_config` table already
sits in the schema (`migrations/000002_schema.up.sql`, columns `role`,
`event`, `channel_type` (`slack`|`email`), `channel_target`) but nothing in
the codebase reads or writes it — confirmed by grep, there is no store
interface, no memory/postgres implementation, and no handler. What ships
today instead is a partial substitute: live WebSocket updates in the web app,
and hard-coded Slack pushes for status changes and `external`/`status-page`
announcements (§13.1, `cmd/slackbot/notify.go`). This phase builds the real
thing: an admin-configurable, role/event-driven routing table across Slack
and (new) email, a SEV-1-without-IC escalation scanner, and — closing the
exact gap Phase 12 and Phase 13 each explicitly deferred for "no
notification layer exists yet" — a second scanner that fires once when a
SEV's overall SLA status (`internal/sev.EvaluateSLA`) becomes at-risk, and
again if it's later confirmed breached.

The `notification_config` schema's shape (`role`, `event`, `channel_target` —
no `user_id`, no `sev_id`) tells us the intended model: each row is a fixed
broadcast route — "for org role X, on event Y, post to channel/address Z" —
not per-user or per-incident personalization. Personalized delivery (e.g. DM
the specific incident's actual assigned IC) is a different targeting model
and is explicitly deferred, not built here.

**15a. Backend: schema + store**

- Migration `000020_notification_config.{up,down}.sql` (next free number
  after `000019_service_leveling_criteria`):
  - `ALTER TABLE notification_config ADD COLUMN max_severity_level SMALLINT;`
    — nullable; `NULL` fires for every severity, a value `N` fires only when
    the triggering SEV's `severity_level <= N`. This expresses "management
    notified of SEV-1/SEV-2 opens only" as one row (`max_severity_level = 2`)
    rather than inventing a new event type per severity band.
  - New table `escalation_config`, mirroring `retention_config`'s
    per-severity-level shape: `id BIGSERIAL PK`, `severity_level SMALLINT
    NOT NULL CHECK (BETWEEN 1 AND 4) UNIQUE`, `threshold_minutes INT NOT
    NULL`, `enabled BOOLEAN NOT NULL DEFAULT false`, `created_at`/`updated_at`.
    All four severity levels pre-seeded disabled, matching
    `internal/store/memory/retentionconfig.go`'s pre-seeded-defaults
    precedent.
  - `ALTER TABLE sevs ADD COLUMN escalated_at TIMESTAMPTZ;` — nullable,
    set the first time the escalation scanner fires for a SEV so it notifies
    once per incident rather than every scan interval; cleared back to
    `NULL` once an Incident Commander is assigned or the SEV leaves
    Open/Investigating.
- `store.NotificationConfig` / `store.EscalationConfig` structs in
  `internal/store/models.go`, doc-commented alongside `RetentionConfig`/`ServiceSLA`.
- `store.NotificationConfigStore` (`internal/store/store.go`, next to
  `ServiceLevelingCriteriaStore`): `Upsert`, `Delete(ctx, role, event,
  channelType string)`, `List(ctx) ([]*NotificationConfig, error)` (admin
  table — no per-service scoping needed), `ListForEvent(ctx, event string,
  severityLevel *int16) ([]*NotificationConfig, error)` (what the dispatcher
  calls; filters `max_severity_level` server-side).
- `store.EscalationConfigStore`: `Upsert`, `Get(ctx, severityLevel int16)`,
  `List(ctx) ([]*EscalationConfig, error)` — same 4-row CRUD shape as
  `RetentionConfigStore`.
- In-memory fakes (`internal/store/memory/notificationconfig.go`,
  `escalationconfig.go`) and Postgres implementations + sqlc queries,
  copying `service_leveling_criteria.go`'s file shape 1:1.
- `store.SEVStore` gains a narrow `SetEscalatedAt(ctx, sevID string, at
  *time.Time) error` mutator for the scanner — not a general-purpose field
  update, matching this codebase's existing preference for narrow mutators
  over widening `Update`.

**15b. Backend: `internal/notify` — dispatch + escalation domain logic**

New package `internal/notify`, parallel to `internal/telemetry`/`internal/share`:

- `Event{Type string; SEV *store.SEV; Message string}` — `SEV` is nil for any
  event type with no severity to filter on.
- Consumer-owned interfaces (per this file's "interfaces belong to the
  consumer" principle): `SlackSender interface { PostMessage(ctx, channel,
  text string) error }` (already satisfied by
  `internal/integrations/slack.Client`) and `EmailSender interface {
  Send(ctx context.Context, to, subject, body string) error }` (satisfied by
  the new `internal/integrations/email.Client`, 15d).
- `Dispatcher{configs store.NotificationConfigStore, integrations
  store.IntegrationConfigStore, crypto Encryptor, slackFactory func(botToken
  string) SlackSender, emailFactory func(creds map[string]string)
  EmailSender}`. `Notify(ctx, ev Event) error` is best-effort — logs and
  swallows per-row delivery errors, never fails the caller's mutation,
  exactly like `auditAppendBestEffort`'s contract — looks up
  `configs.ListForEvent(ctx, ev.Type, severityLevelOf(ev.SEV))`, and for each
  matching row builds the right client from `integrations.Get(ctx,
  string(row.ChannelType))` (reusing `DecryptIntegrationCredentials`, exactly
  as `internal/api/grpc/role.go` already does for Slack) and sends.
- `EvaluateEscalations(ctx, sevs []*store.SEV, roles map[string][]*store.SEVRole,
  configs []*store.EscalationConfig, now time.Time) []*store.SEV` — a pure,
  table-testable function (same shape as `internal/sev/sla.go`'s
  `EvaluateSLA`): for each open SEV with an enabled config at its severity
  level and no `EscalatedAt` set, checks for an `SEVRoleIncidentCommander`
  holder; if none and `now.Sub(sev.StartedAt) > threshold`, includes it.
- Escalation scanner in `cmd/server/main.go`,
  `startEscalationScanner(ctx, log, sevs store.SEVStore, roles
  store.SEVRoleStore, escalations store.EscalationConfigStore, notifier
  *notify.Dispatcher)`, identical ticker shape to `startMetricsRefresher`
  (`main.go`'s existing `ticker`/`select { case <-ctx.Done(): / case
  <-ticker.C: }` loop, started with a bare `go` call), `const
  escalationScanInterval = 1 * time.Minute`. Per breach: `notifier.Notify(ctx,
  notify.Event{Type: "sev.escalation_no_ic", SEV: sev})`, then
  `sevs.SetEscalatedAt(ctx, sev.ID, &now)`.
- SLA risk scanner, same ticker shape and 1-minute interval, in
  `cmd/server/main.go`'s `startSLARiskScanner`/`scanSLARisk`. Wider status
  filter than the escalation scan — includes Resolved and Postmortem In
  Progress, since RTPC (`internal/sev.EvaluateSLA`, measured
  ResolvedAt → PostmortemCompletedAt) is still live post-resolution.
  Batches the `ServiceSLAStore.ListForServices` lookup by severity level
  (at most 4 round-trips per scan, mirroring `report.go`'s
  `serviceLevelMetrics`), reduces each SEV's attached services via
  `sev.MostStrictSLA`, evaluates via `sev.EvaluateSLA`, and fires
  `sev.sla_at_risk` the first time a SEV's `Overall` reads `at_risk`, or
  `sev.sla_breached` the first time it reads `breached` — tracked via a new
  `SEV.SLANotifiedStatus` marker (nullable string, "at_risk"/"breached")
  so neither event re-fires for the same SEV once notified at that level.
  Monotonic by construction: a SEV whose elapsed time already exceeds
  target can only have its eventual final value land at or above that same
  elapsed time, so `at_risk` can't un-happen short of an admin loosening
  the SLA target or affected-services list after the fact (an accepted
  edge case, not handled) — "breached" always takes priority in the
  notify decision so a SEV that jumps straight from on-track to breached
  (final value lands over target before ever reading at-risk) still gets
  exactly one notification, not a skipped one.
- Wire `notify.Dispatcher` into `SEVServer`, `AnnouncementServer`,
  `PostmortemServer` (new `notifier *notify.Dispatcher` field on each,
  threaded through their `*ServerParams` structs like every other shared
  dependency), called right after the existing `publishProto`/`publishJSON`
  call at each already-published event site (`sev.created`, `sev.updated`,
  `sev.status_changed`, `announcement.created`), plus two new call sites:
  `postmortem.go`'s transition-to-Resolved path (fires `postmortem.due`) and
  transition-to-Approved path (fires `postmortem.approved`), reusing
  `internal/postmortem/statemachine.go`'s existing transition validation.

**15c. Backend: API surface**

- `proto/sevitout/v1/config.proto`: `NotificationConfigResponse{role, event,
  channel_type, channel_target, max_severity_level}`,
  `UpsertNotificationConfigRequest`, `DeleteNotificationConfigRequest{role,
  event, channel_type}`, `ListNotificationConfigsResponse{repeated
  NotificationConfigResponse}`; same pattern for
  `EscalationConfigResponse{severity_level, threshold_minutes, enabled}` +
  Upsert/List. New RPCs on `ConfigService`: `UpsertNotificationConfig`/
  `DeleteNotificationConfig`/`ListNotificationConfigs`,
  `UpsertEscalationConfig`/`ListEscalationConfigs`.
- New `internal/api/grpc/config_notification.go`, copying
  `config_leveling_criteria.go`'s structure 1:1 (validation before store
  access, `store.ErrNotFound` → `codes.NotFound`, `internalError(ctx, ...)`
  fallback).
- `internal/auth/rbac.go`: reads (`List*`) → `store.OrgRoleViewer`; writes
  (`Upsert*`/`Delete*`) → `store.OrgRoleAdmin`, matching §18's "Admins are
  the only role with write access to configuration resources."
- `ConfigServer`/`ConfigServerParams` and `cmd/server/main.go`'s `Stores`
  struct + memory/postgres instantiation each gain the two new store fields,
  following the `ServiceLevelingCriteria` precedent exactly.

**15d. Backend: email delivery + catalog entry**

- New `internal/integrations/email` package: `Client{host string; port int;
  username, password, from string}`, `NewClient(host string, port int,
  username, password, from string) *Client`, `Send(ctx, to, subject, body
  string) error` using `net/smtp` with `crypto/tls` STARTTLS — standard
  library only, no new dependency, matching this codebase's existing
  integration clients.
- Extend `internal/integrations/catalog` with a 6th entry, "email":
  credential fields `smtp_host`, `smtp_port`, `smtp_username`,
  `smtp_password`, settings field `from_address`. `GetIntegrationCatalog` and
  `UpsertIntegrationConfig`'s validation need no new code beyond the catalog
  entry itself — confirming Phase 9's prediction that "a 6th integration is a
  one-file catalog change."
- `Dispatcher.emailFactory` (15b) builds an `email.Client` the same way
  Slack's factory builds a Slack client: decrypt the "email" integration
  config, construct, send.

**15e. Frontend: admin Notifications page**

- `web/src/lib/api.ts`: `api.config.notifications.{list, upsert, delete}` and
  `api.config.escalation.{list, upsert}`, same one-line-arrow-function shape
  as every existing `api.config.*` slice.
- `web/src/types/api.ts`: `NotificationConfigResponse`,
  `EscalationConfigResponse` and request types, doc-commented "Roadmap Phase
  15" like every existing type in that file.
- New `web/src/pages/admin/AdminNotificationsPage.tsx`: a routing-rules table
  (role, event, channel type, channel target, max severity, delete) with an
  "Add rule" form above it, built on `ServiceSLAEditor.tsx`'s
  query/mutation/per-row-error pattern, plus a second `<Section>` for the
  4-row (SEV-1..4) escalation threshold table modeled directly on
  `AdminRetentionPage.tsx`'s existing per-severity-level table.
- Register the route in `web/src/pages/admin/AdminRoutes.tsx` and add a tab
  in `AdminLayout.tsx`'s `ADMIN_TABS` array — no new client-side role check
  needed, the whole `/admin/*` subtree is already gated by `App.tsx`'s
  `<ProtectedRoute minRole="admin">`.

**15f. Tests + demo doc**

- Go: `internal/notify/dispatcher_test.go` (event→row matching, severity
  filtering, best-effort error swallowing) and `escalation_test.go`
  (`EvaluateEscalations`: no-IC-and-over-threshold fires, IC-present doesn't,
  already-escalated doesn't re-fire, disabled config doesn't fire); store
  tests for both new stores mirroring `service_leveling_criteria_test.go`;
  `config_notification_test.go` for RBAC floors and validation; additions to
  `sev_test.go`/`announcement_test.go`/`postmortem_test.go` asserting the
  notifier is invoked with the right event type at each call site.
- Frontend: `AdminNotificationsPage.test.tsx` covering add/list/delete for
  both tables.
- `demo/notifications-alerting.md` (existing template: What was built /
  Prerequisites / Walkthrough / Known limitations). Known limitations to
  state: routing is role/event → fixed-channel broadcast, not per-user or
  per-incident-assignee personal delivery; admin config mutations here are
  unaudited, consistent with every sibling Config RPC (none of the
  `config_*.go` handlers write to the audit log today); escalation only
  checks for a missing Incident Commander, not any other role;
  `sev.sla_at_risk`/`sev.sla_breached` fire on the SEV's `Overall` status
  only (worst of MTTD/MTTM/MTTR/RTPC), not per-metric, and — like the
  `at_risk`/`breached` state itself — an admin loosening a service's SLA
  target after a SEV already read `at_risk` won't un-notify it.

**Also considered and explicitly deferred**:

- **Per-user notification preferences** (e.g. opt in/out of email pings) on
  `ProfilePage.tsx` — no plumbing exists today (`WhoAmIResponse` has no such
  field); a natural follow-up once the org-wide routing table above is in
  place and there's a real need to override it per person.
- **In-app toast/badge delivery** — the existing WS hub
  (`internal/api/ws/hub.go`) is SEV-scoped per client with no per-user
  filtering; a global cross-cutting in-app alert would need either a new
  app-shell-level `BroadcastRoom` subscriber with client-side "is this for
  me" filtering, or a separate delivery path. Out of scope; Slack/email
  cover the requirement.
- **Personal/per-incident delivery** (DM the specific SEV's actual assigned
  IC/on-call, rather than a fixed broadcast channel for "whoever holds this
  org role") — would reuse Phase 10's `SlackUserID`/email identity fields,
  but is a materially different targeting model from the
  `notification_config` schema as designed; scope as its own follow-up if
  the broadcast model proves insufficient in practice.

**Follow-up, shipped (migration `000022`)**: routing rules originally covered
exactly one event each (`(role, event, channel_type)` as the natural key).
Widened `notification_config.event` to `events TEXT[]` (`= ANY(...)`
matching, the same idiom `sevs.affected_services`/`service_slas.service_id`
already use) so one rule can cover several events — e.g. a single Slack
rule for both `sev.sla_at_risk` and `sev.sla_breached` instead of two
identical rows differing only in event. A multi-event rule can no longer
serve as its own natural key, so `NotificationConfigStore`/the API moved
from natural-key `Upsert`/`Delete` to `Create`/`Update`/`Delete` by `id`
(the same shape `AIPluginStore` already uses) — see
`demo/notifications-alerting.md` for the current exact schema, RPCs, and a
live-verified walkthrough. The admin UI's event picker became a multi-select
checkbox group; everything else about a rule (role, channel type, channel
target, max severity) stayed single-valued, per the original design.

**Follow-up, shipped ("Send test")**: `ConfigService.TestNotificationConfig`
(POST `/v1/config/notifications/test`) sends one real test message per event
straight to a rule's own channel — bypassing `ListForEvent`'s event/severity
matching entirely — so an admin can verify a Slack channel or email address
actually works without waiting for a real SEV. It takes the same fields as
`CreateNotificationConfigRequest` (no `id`), so it works for an already-saved
rule (the admin page's per-row "Send test" button, passing that row's
current values) or a rule still being drafted in the Add-rule form (same
button, enabled once role/events/channel/target are filled in). `Notifier`
gained a parallel `Test` method that returns each event's delivery error to
the caller instead of only logging it — `Notify`'s best-effort/never-block
contract stays unchanged for real event dispatch; `Test` exists precisely to
surface the error a real admin needs to see. Admin-only and unaudited, like
every other `ConfigService` mutation.

**Estimate**: ~7-9 days (15a ~1 day, 15b ~1.5-2 days, 15c ~1-1.5 days, 15d
~1 day, 15e ~1-1.5 days, 15f ~1-1.25 days) — comparable to Phase 10's scope,
since it spans a new schema, a new domain package, a new integration client,
a new background worker, and a new admin page. **Depends on**: nothing
already in the roadmap — reuses `Publisher`, `IntegrationConfigStore`/
`DecryptIntegrationCredentials`, the integration catalog, and the
`ConfigServer` wiring pattern, all already in place.

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
| 10 | Per-user integration profiles (Slack/GitHub/Jira identity) | — | 6-8 days |
| 11 | Integration-aware SEV UI (hide unconfigured actions, self-join Slack, creator invite) | 10 | 3.5-4.5 days |
| 12 | Per-service SLA targets and breach indicators | — | 4.5-6 days |
| 13 | Per-service SLA compliance reporting | 12 (except 13a's UX mockup) | 3.5-5.25 days |
| 14 | Per-service SEV leveling criteria (guidance, not enforced) | — | 3-4.25 days |
| 15 | Notifications & Alerting (routing + email + escalation) | — | 7-9 days |

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
can be sequenced whenever the sidebar-based UI redesign becomes a priority. Phase 10
is likewise independent of everything above it, but internally its 10c sub-step (a
role-assignment user picker) is a hard dependency for 10d/10e (Slack invite
expansion) — see the phase's own "possible split" note if it needs to be broken up
across multiple PRs/milestones. Phase 11 is the first phase in this document with a
real hard dependency on another unshipped phase: it needs Phase 10's per-user Slack
identities, `ListUserDirectory` RPC, and persisted `SEV.SlackChannelID`, so it must
be sequenced strictly after Phase 10 rather than run independently like 8/9/10.
Phase 12 is independent of every phase above it — it only needs the existing
`Service` registry, not any integration work from Phases 6-11 — so it can be
sequenced whenever SLA tracking becomes a priority. Phase 13 is the second
phase in this document with a real hard dependency on another unshipped
phase: from 13b onward it aggregates directly over Phase 12's
`service_slas` table and `sla.go` evaluation logic, so those steps must
ship strictly after Phase 12. 13a is a deliberate exception — a UX mockup
built against fixture data, with no dependency on Phase 12 or any other
step — so it can be done first, or even in parallel with Phase 12, to get
the table/selector design reviewed before the backend contract is locked
in. Phase 14 is likewise independent of every phase above it, including
12/13 — it reuses the same `Service` registry and admin-page pattern as
Phase 12's SLA editor, but shares no table or evaluation logic with
`service_slas`/`sla.go`, so it can be sequenced whenever the leveling-
guidance need becomes a priority, independently of where Phase 12/13
stand. Phase 15 is independent of every phase above it in the same way —
it reuses the existing `Publisher`, `IntegrationConfigStore`/
`DecryptIntegrationCredentials`, integration catalog, and `ConfigServer`
wiring, but introduces its own schema, domain package, and background
worker, sharing no table or evaluation logic with Phases 12-14. It is,
however, the phase that Phases 12 and 13 each named as their own
prerequisite for "automated breach notifications" / "automated alerts when
compliance drops below a threshold" — those two follow-ups can only be
built once Phase 15's routing table and dispatcher exist.

Each phase, once implemented, gets its own `demo/<topic>.md` runbook following the
existing template (What was built / Prerequisites / Walkthrough / Known
limitations — see `demo/logging-observability.md`,
`demo/sensitive-sev-visibility.md`). Update this document's status line per phase
as it ships (e.g. "✅ shipped, see `demo/request-scoped-logging.md`") rather than
rewriting the whole document.

See [`docs/architecture-evolution.md`](architecture-evolution.md) for the resulting
architecture shape.
