# Demo — Structured Logging & Observability

## What was built

The API server (`cmd/server`) already logged its own startup/shutdown events
through a JSON `slog.Logger`, but almost nothing in between — a handful of
scattered calls in `internal/api/grpc/sev.go` and `config.go` were the only
per-request logging that existed, and (a real pre-existing bug) those calls
went to `slog`'s plain-text-to-stderr default logger instead of the JSON
logger `main()` builds, because nothing ever called `slog.SetDefault`. This
adds comprehensive, structured logging across the whole request path:

- **`slog.SetDefault(log)`** in `main()` — every package-level
  `slog.InfoContext`/`WarnContext`/`ErrorContext` call anywhere in the
  process (existing and new) now lands in the same JSON stream as
  everything else, with the same `LOG_LEVEL` and source-location settings.
- **`LOG_LEVEL`** env var (`debug`/`info`/`warn`/`error`, default `info`) —
  `cmd/server/main.go`'s `parseLogLevel`. `AddSource: true` is always on, so
  every log line names the file/line/function that wrote it.
- **`grpchandler.LoggingUnaryInterceptor`/`LoggingStreamInterceptor`**
  (`internal/api/grpc/logging.go`) — logs every RPC: method, duration,
  resulting gRPC status code, and the caller's `user_id` once authenticated.
  Level is chosen by the status code: `OK` is Info, an expected client error
  (`NotFound`, `InvalidArgument`, `PermissionDenied`, ...) is Warn, and
  `Internal`/`Unknown`/`Unavailable`/`DataLoss` is Error — so a log-level
  alert only fires for the calls actually worth paging on. Because every
  REST endpoint is really a loopback gRPC call proxied through
  grpc-gateway (`cmd/server/main.go`'s `gwMux`), this one interceptor covers
  the entire REST API too, not just native gRPC clients.
- **`auth.authenticate`** (`internal/auth/interceptor.go`) now logs its own
  rejections (Warn: missing/malformed header, invalid/expired token,
  unknown/inactive user, insufficient permissions) — necessary because it
  runs *outside* the logging interceptor in the chain (see "Design notes"
  below), so a rejected call never reaches `LoggingUnaryInterceptor` at all.
- **`internal/auth/password.go`** (`/auth/login`, `/auth/register`) now logs
  every outcome: successful login/registration (Info, with `user_id`/`email`/
  `org_role`), unknown email / wrong password / deactivated user (Warn — the
  message is logged, the password never is), and any actual internal error
  (Error).
- **WebSocket layer** (`internal/api/ws`): connect/disconnect (Info, with
  `user_id` and subscribed `sev_ids`), connection rejections (Warn),
  malformed control frames (Debug — matches a doc comment that had claimed
  this for a while without actually doing it), and a dropped event when a
  client's buffer overflows (Warn) — previously silent, and exactly the
  kind of thing "why didn't my SEV page update live" debugging needs.
- **Three standalone HTTP handlers** that sit outside the gRPC server
  (`/ws`, `/admin/integrations/health`, `/s/{token}`) are wrapped in
  `loggingMiddleware` (`cmd/server/main.go`) for the same method/path/
  status/duration visibility the gRPC interceptor gives everything else.
  `/auth/*` isn't wrapped this way since it already logs richer,
  business-level detail directly; the grpc-gateway route (`/`) isn't either,
  since every request there already reaches the gRPC interceptor.
- **AI dispatcher** (`internal/ai/dispatcher.go`): added Info-level
  "running action" / "action succeeded" around the shared `run` core (used
  by both the proactive trigger path and the on-demand `Run`/`StreamAction`
  path), and Debug-level "skipped" logs for the eligibility gates
  (sensitive/AI-disabled/severity-too-low) that previously returned
  silently.
- **Outbound integration calls** (`internal/integrations/{pagerduty,slack,
  tasktracker/github}`): a Debug-level line per API call (operation + key
  identifiers) and a Warn on failure/non-2xx response — visibility into the
  third-party calls that are a common real-world source of "why isn't X
  working."

## Design notes

**Interceptor ordering matters and is easy to get backwards.** `auth.
UnaryInterceptor` attaches `*auth.UserContext` to a *new* `context.Context`
value (contexts are immutable) — that only propagates to interceptors
*nested inside* it, not back out to one that already called it. So the
chain in `cmd/server/main.go` is:

```go
grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(jwtSigner, userStore), grpchandler.LoggingUnaryInterceptor(log))
```

auth outermost, logging innermost — the opposite of what "log everything,
including rejections" naively suggests. Getting this backwards compiles,
passes a naive test that calls each interceptor standalone, and silently
drops `user_id` from every single log line. `internal/api/grpc/logging_test.go`'s
`TestLoggingAndAuthInterceptorsChained_EndToEnd` runs both interceptors
chained through a real in-process gRPC server specifically to catch this;
`auth.authenticate` logging its own rejections directly is what recovers
the "rejections are visible too" property without needing the reverse order.

## Prerequisites

- `go build ./... && go test ./...` passing

## Walkthrough

```bash
make up

# A failed login attempt
curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"nobody@example.com","password":"whatever123"}'

docker logs sevitout-api-1 --tail 5
```

Expected: a `WARN` line, `"msg":"login failed: unknown email"`, with the
email but never the password.

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

curl -s http://localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" >/dev/null
docker logs sevitout-api-1 --tail 5
```

Expected: an `INFO` line, `"msg":"rpc completed"`,
`"method":"/sevitout.v1.SEVService/ListSEVs"`, with your `user_id` and
`"code":"OK"`.

```bash
curl -s http://localhost:8080/v1/sevs -H "Authorization: Bearer garbage" >/dev/null
docker logs sevitout-api-1 --tail 5
```

Expected: a `WARN` line, `"msg":"rpc rejected: invalid or expired token"`.

Raise verbosity to see outbound integration calls and WebSocket fan-out:

```bash
# in .env: LOG_LEVEL=debug
make down && make up
```

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

New coverage: `internal/api/grpc/logging_test.go` (interceptor level
selection, user-ID attribution, the chained end-to-end ordering test above),
`internal/auth/interceptor_logging_test.go` and `password_logging_test.go`
(rejection/login/register logging), `internal/api/ws/logging_test.go`
(dropped-event warning), `cmd/server/main_test.go` (`parseLogLevel`).

## Known limitations

- `internal/api/grpc`'s existing per-handler `slog` calls (a handful in
  `sev.go`/`config.go` predating this work) weren't consolidated into the
  new interceptor — they log domain-specific detail (e.g. "recurrence
  auto-link create failed") the generic interceptor can't know about, so
  both coexist by design.
- No log sampling/rate-limiting — a client hammering a rejected endpoint
  produces one log line per request. Acceptable at this system's
  single-org scale (`docs/requirements.md` §19); would need revisiting for
  a public-internet-facing deployment.
- No correlation/request ID threaded across log lines yet — grepping by
  `method`+timestamp window or `user_id` is today's way to follow one
  request through `rpc completed`/rejection lines.
