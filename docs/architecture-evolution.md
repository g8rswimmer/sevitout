# Sevitout — Architecture Evolution

**Version**: 0.2 — **historical: every phase described below (`docs/roadmap.md`
Phases 0–4) has shipped.** This document is kept as the design record for *why*
each piece looks the way it does (library choices, ordering constraints,
non-goals) — see `docs/architecture.md`'s §3.4 Observability and §7 Slack Bot
for the as-built description, and the `demo/` runbook linked from each phase's
status line in `docs/roadmap.md` for exact, verified behavior. Nothing below
was rewritten to read as historical prose; it's left in the present tense it
was written in, since the design reasoning itself hasn't changed — only its
status (proposed → shipped) has.

**Relationship to `docs/architecture.md`**: this document is additive, not a
rewrite. It references that document's existing section numbers (§3 API Layer,
§3.3 Auth Interceptor, §8 AI Plugin System, §12 Resolved Architectural Decisions)
rather than duplicating them, and reads as a continuation of §12's
"proposed/in-progress decisions" — but lives in its own file since, at the time
it was written, these decisions weren't resolved yet.

---

## 1. Overview

`docs/architecture.md` describes the system as built through milestone M14: gRPC
services behind a grpc-gateway REST proxy, both multiplexed on one port via
`cmux`, a WebSocket hub for real-time updates, PostgreSQL via `pgx`/`sqlc`, and a
React frontend. That design is complete and matches what's actually running — see
`docs/roadmap.md`'s "Where the project is" section.

This document describes infrastructure **layered onto** that design, not a change
to it: a configuration package, request-scoped logging, operational metrics, a
consistent internal-error-logging convention, and a generic health endpoint.
None of these change an existing API contract, database table, or service
boundary — they add cross-cutting plumbing around the request path that already
exists.

Relationship to the other planning docs:

- [`docs/roadmap.md`](roadmap.md) is the phased *execution* plan — what order this
  work happens in and why, with estimates and status tracking.
- This document describes the resulting *architecture shape* once that plan is
  executed — the "what" and "why" of the design, independent of sequencing.
- Each phase, once shipped, gets a `demo/<topic>.md` runbook recording what was
  actually built and how to exercise it — the durable, precise reference, same as
  `demo/logging-observability.md` and `demo/sensitive-sev-visibility.md` already
  are for the work that preceded this document.

---

## 2. `internal/config`

**Today**: `cmd/server/main.go`'s `main()` reads roughly ten environment variables
directly via `os.Getenv` — `DATABASE_URL`, `JWT_SECRET`,
`ALLOW_INSECURE_JWT_SECRET`, `JWT_TTL_HOURS`, `LOG_LEVEL`, `PAGERDUTY_API_KEY`,
`GITHUB_TOKEN`, `ENCRYPTION_KEY` — with parsing/validation (e.g. `parseLogLevel`)
inlined in `main()` itself. `internal/config/` exists as an empty directory
(`.gitkeep` only), a placeholder from the original milestone plan that was never
filled in.

**Proposed**: a single `internal/config.Config` struct, populated once by
`config.Load()`, called near the top of `main()`. `Load()` performs no I/O beyond
reading the environment and returns validation errors as a plain `error` — it
never calls `os.Exit` itself, so `main()` remains the one place that decides
whether a given failure is fatal, and `Load()` stays unit-testable in isolation.

**What stays in `main()`**: the fail-closed decision around `JWT_SECRET` — refusing
to start unless `ALLOW_INSECURE_JWT_SECRET=true` is explicitly set when the secret
looks weak/default — remains a visible branch in `main()`, reading `cfg.JWTSecret`
and `cfg.AllowInsecureJWTSecret` instead of two `os.Getenv` calls. This is a
security-relevant decision worth keeping legible at the call site, not buried
inside a generic config loader.

**Scale**: given the current field count (~10, growing modestly as later sections
below add a couple of flags), a single `internal/config/config.go` +
`config_test.go` is proportionate. This mirrors the *smaller* end of the
`internal/api/grpc/config_*.go` per-domain split (service registry, users,
on-call, integrations, AI plugins, retention) — that split earns its file-per-domain
structure at 6+ distinct resource types; `internal/config` doesn't need the same
treatment unless it grows well past 20 fields.

`cmd/slackbot` has its own separate `os.Getenv` scatter (`SLACK_APP_TOKEN`,
`SLACK_BOT_TOKEN`, `API_GRPC_ADDR`, etc.) — out of scope here, but a natural
follow-up once this pattern is proven in `cmd/server`.

---

## 3. Request-scoped logging

**Today**: `internal/api/grpc/logging.go`'s `LoggingUnaryInterceptor` logs every
RPC — method, duration, resulting status code, and `user_id` when auth has
already run — and, because every REST call is proxied through grpc-gateway to a
loopback gRPC call, this one interceptor covers the entire public API surface, not
just native gRPC clients. The existing interceptor chain
(`cmd/server/main.go`) is:

```
auth.UnaryInterceptor  →  grpchandler.LoggingUnaryInterceptor
(outermost)                (innermost)
```

documented in `internal/api/grpc/logging.go` itself: `context.WithValue` only
propagates to interceptors nested *inside* the one that set it, so
`LoggingUnaryInterceptor` can only read back `*auth.UserContext` via
`auth.UserFromContext` because auth runs first. A rejected call (bad/expired
token, insufficient permission) never reaches `LoggingUnaryInterceptor` at all —
`auth.authenticate` logs its own rejections directly to recover that visibility.

**Gap**: there's no request/correlation ID anywhere in the system, and no
context-bound logger — every log call site that wants `user_id` re-derives it by
hand via `auth.UserFromContext(ctx)` rather than pulling an already-enriched
logger out of context.

**Proposed**: extend the chain, applying the exact same ordering rule one layer
further out:

```
telemetry.RequestIDUnaryInterceptor  →  auth.UnaryInterceptor  →  grpchandler.LoggingUnaryInterceptor
(outermost)                                                        (innermost)
```

Request-ID generation must be outermost, ahead of auth, for the identical reason
auth must be ahead of logging: each layer's context value only flows inward. Now
even an auth rejection — logged directly by `auth.authenticate` — can carry a
request ID, since `auth.authenticate` runs *inside* the request-ID interceptor.

A new package, **`internal/telemetry`**, holds this: `WithRequestID`/
`RequestIDFromContext` (the same `context.WithValue` shape as
`internal/auth/context.go`'s `WithUser`/`UserFromContext`), and `WithLogger`/
`LoggerFromContext` for a `*slog.Logger` pre-bound with `request_id` and
`user_id` via `log.With(...)`. `LoggingUnaryInterceptor` builds this bound logger
and attaches it to `ctx` *before* invoking the handler — a restructuring from
today's logging, which only happens after the handler returns. Handlers then pull
a fully-enriched logger with `telemetry.LoggerFromContext(ctx)` once, instead of
each call site re-deriving `user_id` independently.

This is a new package rather than an extension of `internal/auth/context.go`
because request ID and a bound logger are cross-cutting infrastructure with no
relationship to authentication — `internal/auth` already depends on
`internal/store` (for `OrgRole`), and the plain-`net/http` handlers in
`cmd/server/main.go` (`/ws`, `/admin/integrations/health`, `/s/{token}`) would
otherwise need to import the auth package purely to use an unrelated concept.
`internal/telemetry` has no dependency on `internal/auth` or `internal/store`, so
it introduces no import-cycle risk with either.

The three standalone `net/http` handlers get the same treatment via
`loggingMiddleware` (`cmd/server/main.go`): generate or reuse a request ID from an
`X-Request-Id` header, bind a logger into the request's context, echo the ID back
in the response header. This is also where `share_view.go` and
`integrations_health.go` — which call `http.Error` on failure today with zero
accompanying `slog` call — get their first log lines.

Finally, `X-Request-Id` is bridged through the grpc-gateway `gwMux`
(`cmd/server/main.go`) into gRPC metadata (`x-request-id`), so a REST caller's
header is honored by `RequestIDUnaryInterceptor` instead of a fresh ID being
minted at the gateway hop — one correlation ID survives the REST→loopback-gRPC
boundary.

---

## 4. Metrics

**Today**: no metrics of any kind. `go.mod` has no `prometheus/client_golang`; the
OpenTelemetry packages visible in `go.sum` are transitive (pulled in by something
else) and unused by any code; no `expvar`.

**Library choice**: `prometheus/client_golang`. The presence of OTel packages in
`go.sum` doesn't make adopting OTel free — using it for real would still mean new
integration work (SDK setup, an exporter, typically a collector), for a framework
whose primary value — distributed tracing across services — doesn't match the
near-term need described in `docs/roadmap.md`: a `/metrics` endpoint an existing
Prometheus/Grafana setup (or plain `curl`) can scrape. See §7 for the explicit
non-goal this implies.

**Composition with the existing interceptor chain**: RPC metrics are folded into
the existing `logRPC` helper (`internal/api/grpc/logging.go`) rather than a
separate metrics interceptor — `logRPC` already resolves `method`, `code`, and
`dur` exactly once per call; a second interceptor would redundantly recompute
`status.Code(err)`. `logRPC` becomes one function with two side effects (log
line + metric record) instead of two functions each doing half the work.

**Exposure**: `GET /metrics` (`promhttp.Handler()`) is added to the existing
`httpMux` in `cmd/server/main.go`, next to `/admin/integrations/health`.
Deliberately unauthenticated — this matches standard Prometheus scrape convention
(an internal-network-only collector, not an end user) and the precedent already
set by `/openapi.json` being unauthenticated on the same mux — but it's a
deliberate step away from the otherwise mostly-authenticated surface, worth a
one-line code comment at the registration site.

**What's measured**: see `docs/roadmap.md` Phase 2 for the concrete metric list
(RPC counters/histograms, a WebSocket connection gauge, an AI-dispatcher outcome
counter, DB connection-pool gauges from `pgxpool.Pool.Stat()` in preference to
per-query histograms, and a periodically-refreshed open-SEV-count gauge by
severity). Per-query DB latency is deliberately deferred — see §7.

---

## 5. `codes.Internal` error-handling convention

**Today**: 119 call sites across `internal/api/grpc/*.go` return
`status.Error(codes.Internal, "<generic message>")` without logging or otherwise
retaining the underlying error. Since `LoggingUnaryInterceptor` only observes the
already-generic status returned by the handler, a real failure (a DB outage, a
driver error) is nearly invisible today beyond `code=Internal` in the RPC log
line.

**Proposed convention, going forward**: a small helper,

```go
func internalError(ctx context.Context, msg string, err error) error {
	telemetry.LoggerFromContext(ctx).ErrorContext(ctx, msg, "err", err)
	return status.Error(codes.Internal, msg)
}
```

used at every `codes.Internal` return site. The message that crosses the wire to
the client is unchanged (still generic, by design — internal error detail
shouldn't leak to API callers); `err`'s actual detail now reaches the log instead
of being discarded. This depends on §3's `telemetry.LoggerFromContext` existing,
and is stated here as the standing convention for **all new handler code** going
forward, not only as a one-time backfill of the 119 existing sites (which
`docs/roadmap.md` Phase 3 sequences incrementally, package by package).

---

## 6. `GET /healthz`

**Today**: the only health-adjacent endpoint is `GET /admin/integrations/health`
(`internal/api/grpc/integrations_health.go`) — authenticated, Admin-only, and it
checks connectivity to configured *third-party* integrations (PagerDuty, GitHub,
Slack). There's no generic, unauthenticated liveness/readiness probe for the
process itself.

**Proposed**: `GET /healthz`, unauthenticated, checking only whether the API
server's own dependencies (currently: the database) are reachable, via a new thin
`Stores.Ping(ctx) error` method — a no-op when the in-memory-store dev fallback is
active, since there's no external DB to be unavailable in that mode. This is what
a container orchestrator's liveness/readiness probe is meant to point at; it says
nothing about whether PagerDuty or GitHub are reachable, which remains
`/admin/integrations/health`'s job.

---

## 7. Non-goals for this phase

Explicitly out of scope for the work described in this document, consistent with
`docs/requirements.md` §19's existing framing that Sevitout is not meant to
replace a full monitoring/observability platform:

- **Distributed tracing** (OpenTelemetry spans, trace context propagation). The
  metrics work in §4 gives request-level visibility (counts, latencies, error
  rates); full tracing is a larger commitment (SDK, exporter, collector) that
  isn't justified by a felt need yet.
- **Per-query database latency histograms.** `internal/store/postgres` calls
  sqlc-generated query functions across many files; wrapping each individually is
  a large speculative surface. Connection-pool-level gauges (§4) are the interim
  signal; per-query instrumentation should wait for an actual slow-query
  investigation that needs it.
- **Live external health polling beyond what exists today** (e.g. continuous
  GitHub Issue status sync, live PagerDuty webhook ingestion) — `/admin/integrations/health`
  already does on-demand connectivity checks; a background poller is a
  meaningfully larger feature, tracked separately in `docs/roadmap.md` Phase 6 if
  ever prioritized.
- **A scheduler/cron subsystem.** No such precedent exists in the codebase today;
  features that would want one (e.g. recurring CSV export) are noted but not
  recommended in `docs/roadmap.md` Phase 6 for that reason.

---

## 8. Rollout notes

- No new *required* environment variables are introduced by this phase. `/metrics`
  and `/healthz` are always registered when the server starts — there's no
  server-side enable/disable flag, since Prometheus scraping and orchestrator
  health checks are both opt-in on the *caller's* side (nothing polls these
  endpoints unless something is configured to).
- `/metrics` and `/healthz`, like `/openapi.json` today, are deliberately
  unauthenticated. This is worth calling out explicitly because it's the
  exception, not the rule — nearly everything else behind `httpMux` requires a
  valid JWT.
- None of the changes described here alter an existing database table, proto
  message, or REST/gRPC method signature — this is additive infrastructure, not a
  breaking change to any existing API consumer (web app, Slack bot, or direct API
  client).

See [`docs/roadmap.md`](roadmap.md) for sequencing, estimates, and status tracking
as each of these lands.
