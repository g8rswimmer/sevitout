# Demo — `internal/config` Package (Roadmap Phase 0)

## What was built

`cmd/server/main.go`'s `main()` used to read roughly ten environment variables
directly via `os.Getenv`, scattered across the function alongside the logic that
uses each one (`DATABASE_URL`, `JWT_SECRET`, `ALLOW_INSECURE_JWT_SECRET`,
`JWT_TTL_HOURS`, `LOG_LEVEL`, `PAGERDUTY_API_KEY`, `GITHUB_TOKEN`,
`ENCRYPTION_KEY`). `internal/config/` existed only as an empty `.gitkeep`
placeholder from the original milestone plan.

This adds a real `internal/config` package:

- **`config.Config`** — a typed struct holding every one of those settings.
- **`config.Load()`** — reads them all in one pass, called once near the top of
  `main()`. It does no I/O beyond `os.Getenv` and never calls `os.Exit` itself —
  it reports problems via its `error` return, leaving `main()` as the single
  place that decides whether a given failure is fatal. This keeps `Load` fully
  unit-testable without a real process exit.
- **`config.ParseLogLevel`** — moved here from `cmd/server/main.go` unchanged
  (was `parseLogLevel`), since log-level parsing is itself a piece of
  configuration parsing.

`main()`'s existing fail-closed security decisions stay exactly where they were,
now reading from `cfg` instead of calling `os.Getenv` directly:

- The `JWT_SECRET` check (refuse to start with a fixed signing secret unless
  `ALLOW_INSECURE_JWT_SECRET=true` is explicitly set) is unchanged in behavior
  and location — still a visible branch in `main()`, not buried inside the
  config loader.
- The `ENCRYPTION_KEY` decode-or-exit check is likewise unchanged.

## Design notes

**One new, small behavior change**: `JWT_TTL_HOURS`, if set, now has to be a
positive integer or the server refuses to start (`config: JWT_TTL_HOURS must be
a positive integer, got "..."`, printed to stderr since there's no logger yet at
that point — the logger's own level is itself part of `cfg`). Previously, an
invalid value was silently ignored and the default of 24 hours was used instead.
This is a deliberate tightening, consistent with the existing fail-closed
treatment of `JWT_SECRET`/`ENCRYPTION_KEY` elsewhere in `main()`: a typo'd TTL
is exactly the kind of misconfiguration that's better caught at startup than
silently producing a session lifetime nobody intended.

**Why `Load()` can't log its own errors**: the JSON `slog.Logger` built in
`main()` needs `cfg.LogLevel` to construct, so `config.Load()` necessarily runs
before any logger exists. A `Load()` failure is therefore reported with a bare
`fmt.Fprintln(os.Stderr, ...)` rather than a structured log line — the one place
in the startup path that isn't JSON-structured, and unavoidably so.

## Prerequisites

- `go build ./... && go test ./...` passing

## Walkthrough

```bash
# Normal startup — LOG_LEVEL flows through cfg.LogLevel as before.
ALLOW_INSECURE_JWT_SECRET=true LOG_LEVEL=debug go run ./cmd/server
```

Expected: the same JSON startup log lines as before this change (in-memory
store warning, integration-disabled notices, "sevitout api starting").

```bash
# A malformed JWT_TTL_HOURS now fails closed before the server starts.
JWT_TTL_HOURS=notanumber go run ./cmd/server
```

Expected: `config: JWT_TTL_HOURS must be a positive integer, got "notanumber"`
on stderr, and the process exits `1` immediately — no store, no listener, no
partial startup.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./...
```

New coverage: `internal/config/config_test.go` (`Load`'s defaults, every field
round-tripping through its env var, `ALLOW_INSECURE_JWT_SECRET`'s exact-`"true"`
matching, `JWT_TTL_HOURS`'s valid/zero/negative/non-numeric cases,
`ParseLogLevel`'s level table — the latter moved verbatim from the removed
`cmd/server/main_test.go`).

## Known limitations

- `cmd/slackbot` has its own, separate `os.Getenv` scatter (`SLACK_APP_TOKEN`,
  `SLACK_BOT_TOKEN`, `API_GRPC_ADDR`, `SLACKBOT_SERVICE_EMAIL`,
  `SLACKBOT_SERVICE_PASSWORD`) untouched by this change — a natural follow-up
  once this pattern is proven out here, not done as part of this phase.
- This is plumbing only: no new environment variables were introduced, and no
  existing default value changed (the one behavior change — `JWT_TTL_HOURS`
  validation — only affects a value that was already invalid and silently
  discarded before).

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 0) and
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) (§2) for
the fuller design rationale.
