# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Sevitout is a **Severity Event (SEV) management system** written in Go. It is an internal tool for a single organization to open, track, and learn from incidents. See `docs/requirements.md` for the full functional requirements.

Primary interfaces:
- REST API (Go backend, API-first — all features must be available via API)
- Web application (UI consuming the REST API)
- Slack bot (consuming the same REST API)

Key integrations: Slack, PagerDuty, Jira/GitHub Issues/Linear, Datadog/Prometheus/CloudWatch. Auth via OAuth 2.0 (Google/GitHub).

## Commands

*(Populate once `go.mod` and project structure are established)*

```bash
go build ./...          # build all packages
go test ./...           # run all tests
go test ./... -run TestName   # run a single test
go vet ./...            # static analysis
```

## Architecture

### Planned structure

```
cmd/
  server/       # API server entrypoint
  slackbot/     # Slack bot entrypoint
internal/
  sev/          # core SEV domain: models, lifecycle, state machine
  postmortem/   # postmortem workflow
  ai/           # AI plugin interface and built-in providers
  integrations/
    slack/
    pagerduty/
    tasktracker/ # Jira, GitHub Issues, Linear
    monitoring/  # Datadog, Prometheus, CloudWatch
  store/        # persistence layer (repository interfaces + implementations)
  auth/         # OAuth, session management, RBAC
  api/          # HTTP handlers, routing, middleware
docs/
  requirements.md
```

### Core domain concepts

- **SEV**: the central record. Has a lifecycle (`Open → Investigating → Mitigated → Resolved → Postmortem Complete`), severity level (SEV-1 through SEV-4), and a rich set of attached data (people/roles, SLIs, linked tasks, linked SEVs, announcements, chat log).
- **Roles on a SEV**: On-call, Detected By, Incident Commander, Communications Lead, Recorder, Responders. Multiple people may hold a role.
- **Derived metrics**: MTTD, MTTM, MTTR — computed from lifecycle timestamps and stored on the record.
- **Postmortem**: required for every SEV. Has its own status (`Draft → In Review → Approved`). Auto-seeded by the AI plugin on resolve.
- **Audit log**: immutable append-only record of every mutation to a SEV, with user and timestamp.
- **AI plugin**: pluggable skill interface. Triggered proactively at lifecycle transitions (e.g., auto-draft postmortem on resolve) and on demand. Each plugin has its own provider/model/API key configuration.

### Design principles

- The store layer uses repository interfaces; implementations are swappable (unit tests use in-memory fakes).
- All integrations (Slack, PagerDuty, etc.) are behind interfaces in `internal/integrations/`; the core domain does not import them directly.
- The AI plugin system is configuration-driven: plugins register a name, version, and handler (HTTP endpoint or built-in). Sensitive config (API keys) is encrypted at rest.
- SEVs flagged as sensitive (e.g., security incidents) enforce field-level visibility restrictions beyond normal RBAC.
- **Interfaces belong to the consumer, not the producer.** Declare an interface in the package that depends on the behavior, not the package that implements it. The implementation satisfies the interface implicitly — it never imports the interface definition. A single concrete type can then satisfy multiple independent interfaces without coordination. The `internal/store/` package is the deliberate exception: it acts as a shared contract layer imported by both handlers and store implementations.

## Database safety

`DATABASE_URL` is the same variable the dev server, `make up`, and the Postgres
integration test suite (`internal/store/postgres/postgres_integration_test.go`)
all read. That suite's `truncateAll` helper TRUNCATEs every application table
(`users`, `integration_config`, `sevs` — everything but `schema_migrations`)
before most of its tests run. A Postgres container's data volume also outlives
`docker compose down`, which removes containers, not volumes — so `docker
compose up -d postgres` against an existing project can silently reattach to a
long-lived volume full of real data rather than create a fresh one.

An incident happened exactly this way: running the integration suite against
what was assumed to be a throwaway container actually reattached to an
11-day-old dev volume, wiping its real registered users and integration
credentials with no backup to restore from.

Before running `-tags integration` tests, `make test-integration`, or any
other command that mutates a database this project's `docker compose` manages:

1. Never assume a docker volume is disposable just because you started the
   container yourself this session. Check its actual age first:
   `docker volume inspect sevitout_postgres_data --format '{{.CreatedAt}}'`.
   If it predates this session, treat it as real data until the user says
   otherwise — ask before running anything destructive against it.
2. For ad hoc verification, prefer an isolated project/volume instead of the
   default one, e.g. `docker compose -p sevitout-verify -f deploy/docker-compose.yml --env-file .env up -d postgres`,
   and tear it down with `down -v` (removes its volume too) when done — never
   plain `down` followed by assuming the data is gone.
3. The integration suite requires `ALLOW_DESTRUCTIVE_DB_TESTS=1` (see
   `newTestPool` in `postgres_integration_test.go`) for exactly this reason.
   `make test-integration` sets it for you; don't export it by hand against
   anything but a database created specifically for this run and that you are
   prepared to lose entirely.
