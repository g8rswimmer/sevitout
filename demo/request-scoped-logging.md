# Demo — Request-Scoped Logging (Roadmap Phase 1)

## What was built

Structured logging (`demo/logging-observability.md`) already covered every gRPC
call and the REST surface built on top of it via grpc-gateway, but had two
documented gaps: no request/correlation ID anywhere, and no way for handler code
to get an already-enriched logger without re-deriving `user_id` from
`auth.UserFromContext` by hand at every call site. This closes both:

- **`internal/telemetry`** (new package) — `WithRequestID`/`RequestIDFromContext`
  and `WithLogger`/`LoggerFromContext`, the same `context.WithValue` shape as
  `internal/auth/context.go`'s `WithUser`/`UserFromContext`, but with zero
  dependency on `internal/auth` or `internal/store` — request correlation is
  unrelated to authentication. `LoggerFromContext` falls back to `slog.Default()`
  when nothing's attached (e.g. inside `internal/ai.Dispatcher`'s background
  worker pool, which runs against the process-lifetime context, not any single
  request's).
- **`grpchandler.RequestIDUnaryInterceptor`/`RequestIDStreamInterceptor`**
  (`internal/api/grpc/request_id.go`) — attaches a request ID to `ctx`, reusing
  one supplied via the `x-request-id` gRPC metadata key
  (`grpchandler.RequestIDMetadataKey`) when present, or minting a fresh
  `github.com/google/uuid` otherwise. Chained **outermost** in
  `cmd/server/main.go`, ahead of `auth.UnaryInterceptor`, which is itself ahead
  of `LoggingUnaryInterceptor` — three deep now instead of two.
- **`LoggingUnaryInterceptor`/`LoggingStreamInterceptor`**
  (`internal/api/grpc/logging.go`) now bind a `*slog.Logger` — pre-enriched with
  `request_id` and `user_id` via `log.With(...)` — into `ctx` *before* calling
  the handler, not just after. Handler code anywhere downstream can now do
  `log := telemetry.LoggerFromContext(ctx)` once and get a logger that already
  carries both fields.
- **`auth.authenticate`** (`internal/auth/interceptor.go`) now includes
  `request_id` on every one of its own rejection logs — necessary because a
  rejected call never reaches `LoggingUnaryInterceptor` at all, so this is the
  one log line for that request that has to attach the field itself.
- **`cmd/server/main.go`'s `loggingMiddleware`**, wrapping the three standalone
  `net/http` handlers (`/ws`, `/admin/integrations/health`, `/s/{token}`), now
  reuses an incoming `X-Request-Id` header or mints a fresh UUID, echoes it back
  in the response header, and binds a request-scoped logger into the request's
  context the same way the gRPC path does. This gives **`share_view.go`** and
  **`integrations_health.go`** their first `slog` calls ever — both used to call
  `http.Error` on every failure path with zero accompanying log line.
- **`gatewayMetadata`** (`cmd/server/main.go`, extracted from an inline closure
  for testability) bridges an incoming `X-Request-Id` HTTP header into the
  `x-request-id` gRPC metadata key grpc-gateway forwards on its loopback call —
  so a REST caller's request ID survives the REST→gRPC hop instead of a fresh
  one being minted at that boundary.

## Design notes

**Three-deep interceptor ordering, extending the existing rule.**
`context.WithValue` returns a *new* `context.Context` value that only
propagates to interceptors nested *inside* the one that set it — this is why
`auth.UnaryInterceptor` already had to run ahead of `LoggingUnaryInterceptor`
(documented in `internal/api/grpc/logging.go`). The same reasoning extends one
layer further out: `RequestIDUnaryInterceptor` has to be outermost of all
three, or `auth.authenticate`'s own rejection logs — which never reach
`LoggingUnaryInterceptor` — would have no request ID to attach.
`internal/api/grpc/logging_test.go`'s
`TestLoggingAndAuthAndRequestIDInterceptorsChained_EndToEnd` chains all three
through a real in-process gRPC server specifically to catch a reordering
mistake, mirroring the two-deep version of that test from the prior logging
work.

**`gwMux` doesn't echo `X-Request-Id` back to REST callers.** Unlike
`loggingMiddleware`'s three handlers, a gRPC-gateway-proxied REST call (e.g.
`GET /v1/sevs`) doesn't get `X-Request-Id` set on its HTTP response — the header
is only bridged *inbound*, into gRPC metadata, for the request-ID interceptor to
pick up server-side. Echoing it back on every gateway response would need
either a custom `ServeMux` wrapper or forwarding it through gRPC trailer
metadata, neither of which this phase does. A client that wants to correlate
its own logs against the server's should supply its own `X-Request-Id` up
front (which *is* honored — see the walkthrough below) rather than relying on
the response to tell it what ID was used.

## Prerequisites

- `go build ./... && go test ./...` passing
- `make up` started (or `go run ./cmd/server` locally, with
  `ALLOW_INSECURE_JWT_SECRET=true` for local dev — see `demo/config-package.md`)

## Walkthrough

```bash
make up

# A gRPC-gateway-backed REST call, with a client-supplied X-Request-Id.
curl -s -o /dev/null -H 'X-Request-Id: my-grpc-req-id' http://localhost:8080/v1/sevs

docker logs sevitout-api-1 --tail 5
```

Expected: a `WARN` line, `"msg":"rpc rejected: missing authorization header"`,
`"method":"/sevitout.v1.SEVService/ListSEVs"`, `"request_id":"my-grpc-req-id"` —
the client-supplied ID survived the REST→loopback-gRPC hop and shows up on
`auth.authenticate`'s own rejection log.

```bash
# One of the three standalone net/http handlers, with no X-Request-Id supplied.
curl -s -D - -o /dev/null http://localhost:8080/admin/integrations/health | grep -i x-request-id
```

Expected: a freshly minted UUID in the response header, e.g.
`X-Request-Id: 41e571ba-adb0-4e8f-a901-f3bd12bf3cf9`.

```bash
docker logs sevitout-api-1 --tail 5
```

Expected: two JSON lines sharing that same `request_id` — the handler's own
`"integrations health rejected: missing bearer token"` (no token supplied), and
`loggingMiddleware`'s `"http request"` access-log line with
`"status":401`.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

New coverage: `internal/telemetry/context_test.go` (request-ID and logger
round-trips, the `slog.Default()` fallback), `internal/api/grpc/request_id_test.go`
(minting vs. reusing a caller-supplied ID, unary and stream), the extended
`internal/api/grpc/logging_test.go` (request-ID attribution, the bound-logger
retrieval, the three-deep chained end-to-end ordering test),
`internal/auth/interceptor_logging_test.go` (request ID on a rejection log),
new tests in `internal/api/grpc/share_view_test.go` and
`integrations_health_test.go` (the previously-unlogged internal-error and
auth-rejection paths, using a small fault-injecting store wrapper since the
in-memory stores never return anything but `ErrNotFound`), and
`cmd/server/logging_middleware_test.go` /
`cmd/server/gateway_metadata_test.go` (the two `cmd/server/main.go` functions
extracted for testability).

## Known limitations

- **`POST /auth/login` and `POST /auth/register` don't get a request ID.**
  `passwordHandler` is registered directly on `httpMux`, not wrapped in
  `loggingMiddleware` — deliberately, since it already logs richer
  business-level detail itself (`internal/auth/password.go`) and wrapping it
  would double-log every call, the same reason `gwMux` itself isn't
  `loggingMiddleware`-wrapped either. Giving it a request ID without that
  double-logging would need a lighter, log-nothing-itself piece of middleware
  plus updating `password.go` to read `telemetry.LoggerFromContext` instead of
  the package-level `slog` calls it uses today — out of scope for this phase,
  called out explicitly rather than left as a silent gap.
- Background work (`internal/ai.Dispatcher`'s worker pool) runs against the
  process-lifetime context, not any single request's, so it never has a bound
  logger to retrieve — `telemetry.LoggerFromContext` falls back to
  `slog.Default()` there, same as it always has, with no request ID attached.
- No log sampling/rate-limiting, unchanged from `demo/logging-observability.md`.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 1) and
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) (§3) for
the fuller design rationale.
