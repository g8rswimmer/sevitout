# Demo — `GET /healthz` (Roadmap Phase 4)

## What was built

Before this change there was no generic liveness/readiness endpoint — only
`GET /admin/integrations/health` (`internal/api/grpc/integrations_health.go`),
which is authenticated, admin-only, and checks *third-party* integration
connectivity (PagerDuty/GitHub/Slack), not process or store liveness. Neither
a container orchestrator's liveness probe nor a load balancer's readiness
check can use it — it needs a bearer token and answers the wrong question.

- **`grpchandler.Pinger`** (new file, `internal/api/grpc/healthz.go`) — a
  one-method interface, `Ping(ctx) error`, declared at the consumer per
  `CLAUDE.md`'s interface convention.
- **`grpchandler.NewHealthzHandler(pinger)`** — serves `GET /healthz`:
  `200 {"status":"ok"}` when `pinger.Ping` succeeds, `503
  {"status":"unavailable"}` when it doesn't. The underlying error (e.g. a
  Postgres driver error) is logged server-side at Error via
  `telemetry.LoggerFromContext` — see `demo/request-scoped-logging.md` — but
  never crosses the wire, mirroring `internalError`'s (`errors.go`, Phase 3)
  reasoning that error detail belongs in the log, not in a response an
  unauthenticated caller can read.
- **`(*Stores).Ping`** (`cmd/server/main.go`) satisfies `Pinger`: delegates to
  `*pgxpool.Pool.Ping` when running against real Postgres, and is a no-op that
  always succeeds against the in-memory dev fallback (`Pool` is `nil` there —
  there's no connection to lose).
- Wired up in `main()` as `httpMux.Handle("/healthz",
  grpchandler.NewHealthzHandler(stores))`, alongside `/metrics`.

## Design notes

**Unauthenticated and un-logged on success**, same rationale already applied
to `GET /metrics` (Phase 2): an orchestrator's probe carries no credentials,
and polls every few seconds — an access-log line per poll would be pure
noise. A failure is still worth a log line, so that branch logs at Error
regardless.

**Checks DB reachability only**, not every integration — that's what
`/admin/integrations/health` is for. A liveness/readiness probe should answer
"is this process able to serve requests," not "is PagerDuty currently up";
conflating the two would make a transient third-party outage take the whole
service out of a load balancer's rotation for no reason.

**`Stores.Ping` lives on `cmd/server`'s own `Stores` type**, not as a new
method added to `store.SEVStore` or any other repository interface in
`internal/store` — reachability isn't a per-domain-store concern, and every
store implementation already shares the one `*pgxpool.Pool` `Stores` holds.

## Prerequisites

- `go build ./... && go test ./...` passing

## Walkthrough

```bash
make up

curl -i http://localhost:8080/healthz
# HTTP/1.1 200 OK
# Content-Type: application/json
# {"status":"ok"}

curl -i -X POST http://localhost:8080/healthz
# HTTP/1.1 405 Method Not Allowed
```

Triggering the `503` path requires the backing Postgres to actually be
unreachable (e.g. `docker stop` the `db` container while `api` is still up) —
see `TestHealthzHandler_Unreachable_ReturnsServiceUnavailable`
(`healthz_test.go`) for the equivalent exercised via a scripted `Pinger`,
since the demo stack's own store doesn't fail on demand.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

New coverage: `healthz_test.go` (`internal/api/grpc`, reachable/unreachable/
wrong-method) and `stores_ping_test.go` (`cmd/server`, nil-`Pool` no-op path).

## Known limitations

- `/healthz` reports readiness for the DB only — it doesn't check that the
  gRPC/gateway/WebSocket layers are themselves accepting connections. In
  practice a process that can reach the DB but has otherwise wedged its
  listener is not a failure mode this codebase has seen; revisit if that
  changes.
- No separate liveness vs. readiness distinction (a single endpoint serves
  both) — the process has no state where it's "alive but not ready" longer
  than the brief startup window before `main()` reaches `mx.Serve()`, so a
  single check covers both meanings by orchestrator convention.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 4) and
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) (§6) for
the fuller design rationale.
