# Demo — Metrics (Roadmap Phase 2)

## What was built

Zero operational metrics existed before this — no `prometheus/client_golang` in
`go.mod`, no OpenTelemetry code (the OTel packages visible in `go.sum` are
transitive, pulled in by something else, unused by any code), no `expvar`. This
adds a Prometheus scrape endpoint and a minimum viable metric set:

- **`GET /metrics`** (`promhttp.Handler()`, `cmd/server/main.go`) — deliberately
  unauthenticated, matching standard Prometheus scrape convention and the
  already-unauthenticated `GET /openapi.json` on the same `httpMux`. Not
  wrapped in `loggingMiddleware`, unlike `/ws`/`/admin/integrations/health`/
  `/s/{token}` — a scraper polling every few seconds would turn that into pure
  log noise.
- **`internal/telemetry/metrics.go`** (new file, same package as
  [`demo/request-scoped-logging.md`](request-scoped-logging.md)'s context
  helpers — both are cross-cutting per-request/operational infrastructure, a
  deliberately different meaning of "metrics" from `internal/sev.ComputeMetrics`'
  business metrics):
  - `sevitout_rpc_requests_total{method,code}` (counter) and
    `sevitout_rpc_duration_seconds{method,code}` (histogram) — recorded from
    `internal/api/grpc/logging.go`'s `logRPC`, which already resolves both
    labels exactly once per call for its own log line. Since every REST call
    is a loopback gRPC call proxied through grpc-gateway, this covers the
    entire public API surface, not just native gRPC clients.
  - `sevitout_ws_connections` (gauge) — `Inc()`/`Dec()` at
    `internal/api/ws/handler.go`'s existing "websocket connected"/
    "websocket disconnected" log sites.
  - `sevitout_ai_action_total{outcome}` (counter, `success`/`error`/`skipped`)
    — `internal/ai/dispatcher.go`'s shared `run` core now has named returns
    and a `defer` that records `success`/`error` from whichever internal step
    actually returns (rate limit, provider build, context build, the action
    call, or the store write) — one observation point per call, the same
    principle `logRPC` uses. `runTrigger`'s two proactive-only eligibility
    gates (SEV not eligible, severity too low for `sev.opened`) record
    `skipped`.
  - `sevitout_open_sevs{severity}` and `sevitout_db_pool_idle_conns`/
    `_used_conns`/`_max_conns` (gauges) — populated every 30s by a new
    background goroutine (`cmd/server/main.go`'s `startMetricsRefresher`),
    not incremented at mutation sites. `sevitout_open_sevs` uses the same
    "open" status set (Open, Investigating, Mitigated) as `SearchService`'s
    `open` quick-view preset, and excludes sensitive SEVs from the count —
    consistent with sensitive SEVs already being excluded from search/reports
    elsewhere. The DB pool gauges read `pgxpool.Pool.Stat()`, exposed via a
    new `Stores.Pool` field (`nil` for the in-memory dev fallback, in which
    case these three gauges just stay at zero).

**A gap found and closed during manual verification, not originally scoped**:
`auth.authenticate`'s own rejection logs (added in Phase 1, since a call auth
rejects never reaches `LoggingUnaryInterceptor`) had the identical blind spot
for metrics — every auth failure would have been invisible to
`sevitout_rpc_requests_total`/`sevitout_rpc_duration_seconds`, exactly the kind
of thing a metrics dashboard most needs to catch (e.g. a bad deploy causing
every request to fail auth would show as *zero* problems on `/metrics`).
`authenticate` now records both metrics itself on every rejection branch,
mirroring its existing direct-logging pattern — see Design notes.

## Design notes

**Why `prometheus/client_golang`, not OpenTelemetry.** The OTel packages
already present transitively in `go.sum` don't make adopting OTel free — using
it for real still means new integration work (SDK, exporter, typically a
collector), for a framework whose main value (distributed tracing) doesn't
match the near-term need: a `/metrics` endpoint an existing
Prometheus/Grafana setup, or plain `curl`, can scrape. Distributed tracing
remains an explicit non-goal — see
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) §7.

**Auth rejections needed their own metric recording, for the same reason they
needed their own logging.** `logRPC` (where every other RPC metric is
recorded) only runs from inside `LoggingUnaryInterceptor`, which
`auth.UnaryInterceptor` never calls when it rejects a request. `authenticate`
now has a small `reject(code, msg)` helper — parallel to its existing `warn`
closure — that records `RPCRequestsTotal`/`RPCDurationSeconds` and returns the
`status.Error`, so every one of its four rejection branches gets both in one
call. `authenticate` measures its own elapsed time from its own entry point
rather than plumbing a start time in from the interceptor.

**One observation point per call, even with multiple failure branches.**
`internal/ai.Dispatcher.run` switched to named return parameters
(`out *store.AIOutput, err error`) specifically so a single `defer` can look
at whichever error the function actually returns — from any of its five
possible return points — without scattering a metric-increment call at each
one individually. `internal/api/grpc.logRPC` already used the equivalent
pattern (one function, called once per RPC, observing the final result).

## Prerequisites

- `go build ./... && go test ./...` passing
- `make up` started (or `go run ./cmd/server` locally)

## Walkthrough

```bash
make up

# An authenticated call and an auth-rejected one, to populate both outcomes.
curl -s -o /dev/null http://localhost:8080/v1/sevs   # rejected: no token

curl -s -X POST http://localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' >/dev/null
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
curl -s -o /dev/null -H "Authorization: Bearer $TOKEN" http://localhost:8080/v1/sevs

curl -s http://localhost:8080/metrics | grep '^sevitout_'
```

Expected: `sevitout_rpc_requests_total` with two rows for
`method="/sevitout.v1.SEVService/ListSEVs"` — one `code="Unauthenticated"` and
one `code="OK"` — plus `sevitout_open_sevs{severity="1".."4"}` all present
(even at `0`), and `sevitout_ws_connections 0`.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

New coverage: `internal/telemetry/metrics_test.go` (every metric exercised in
isolation, using unique label values per test since these are process-wide
globals shared within one test binary), `internal/api/grpc/logging_test.go`
(`logRPC` records both RPC metrics), `internal/auth/interceptor_logging_test.go`
(an auth rejection also records them), `internal/api/ws/handler_test.go`
(the connection gauge increments/decrements around a real dial/close, and
stays untouched by a rejected connection), `internal/ai/dispatcher_test.go`
(all three outcome labels, using before/after deltas), and
`cmd/server/metrics_refresher_test.go` (`refreshMetrics`'s severity bucketing,
sensitive/closed exclusion, a nil pool and a store error both handled without
panicking, and the refresher goroutine stopping when its context is
canceled).

## Known limitations

- `sevitout_open_sevs` and the `sevitout_db_pool_*` gauges are refreshed every
  30 seconds, not on every mutation — a dashboard reading them can lag reality
  by up to that window. Acceptable for a dashboard gauge; nothing here is
  meant to be alerted on with sub-30s latency requirements.
- Per-query database latency histograms were deliberately not added —
  `internal/store/postgres` calls sqlc-generated query functions across many
  files, and wrapping each individually is a large surface for a speculative
  win. The connection-pool gauges are the interim signal; per-query
  instrumentation should wait for an actual slow-query investigation that
  needs it.
- `/metrics` has no cardinality guard on the `method` label today — it's
  bounded in practice by the fixed set of gRPC service methods this server
  registers, but nothing enforces that structurally.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 2) and
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) (§4) for
the fuller design rationale.
