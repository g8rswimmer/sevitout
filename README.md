# sevitout

**Sevitout** is a self-hosted Severity Event (SEV) management system: a single
place to open, track, and learn from incidents. It's built for a single
organization — no multi-tenancy — and every feature is available through a
REST/gRPC API first, with a React web app and a Slack bot as consumers of that
same API.

## Overview

Sevitout covers the full lifecycle of an incident:

- **Open and track a SEV** through its lifecycle (`Open → Investigating →
  Mitigated → Resolved → Postmortem In Progress → Postmortem Complete`), with
  MTTD/MTTM/MTTR/DTTM metrics computed automatically from lifecycle
  timestamps.
- **Assign roles** (Incident Commander, Communications Lead, Recorder,
  Responders, On-call) and auto-populate on-call from PagerDuty.
- **Write a postmortem** for every SEV — required, not optional — with a
  status workflow and a post-completion lock that requires a written reason
  to unlock.
- **Announce, chat-log, and link** related SEVs and external tasks (GitHub
  Issues today; Jira/Linear planned).
- **Search, filter, and report** across every SEV, export to CSV, and share a
  curated read-only view of a SEV publicly without a login.
- **Run it from Slack** — open, update, transition, and resolve SEVs with
  slash commands; every SEV gets its own auto-created incident channel.
- **Ask an AI plugin** to summarize, suggest a root cause, draft a
  postmortem, or suggest responders — proactively at key lifecycle
  transitions, or on demand.

Three surfaces, one API:

| Surface | What it's for |
|---|---|
| **REST / gRPC API** | The source of truth — every other surface is a consumer of it |
| **Web application** | React SPA — dashboard, SEV detail, postmortem editor, admin config |
| **Slack bot** | `/sev open`/`update`/`transition`/`resolve`/`capture`, `@sevbot status`/`timeline` |

## Architecture at a glance

Go backend (gRPC + a REST gateway generated from the same proto definitions,
multiplexed on one port), PostgreSQL storage, a separate Slack bot binary
talking to the API over gRPC, and a React/TypeScript frontend. See
[`docs/architecture.md`](docs/architecture.md) for the full system design.

## Quickstart

**Prerequisites**: Docker + Docker Compose, Go 1.25+ (only needed if you want
to run the API without Docker), Node.js 22+ (only needed for frontend
development outside the `web` container).

```bash
git clone <this repo> && cd sevitout
cp .env.example .env
# Fill in at least JWT_SECRET (min 32 chars) and ENCRYPTION_KEY
# (base64-encoded 32 bytes, e.g. `openssl rand -base64 32`).

make up          # builds and starts postgres, migrate, api, slackbot, web
```

Then open **http://localhost:3000**, register an account, and start
opening SEVs — the **first user to register is automatically made Admin**
(there's no separate bootstrap step). Every user after that gets the
Viewer role until an Admin promotes them.

Other useful targets:

```bash
make down             # stop the stack
make migrate          # (re-)run pending migrations
make psql             # open a psql shell against the running database
go run ./cmd/server   # run the API server directly, without Docker
```

`make build` compiles every binary (`cmd/server`, `cmd/slackbot`) into the
repo root for local use — those binaries are gitignored, so don't be
surprised to see `server`/`slackbot` show up there after building.

For a step-by-step walkthrough of every feature with real `curl` commands,
see the per-milestone runbooks in [`demo/`](demo/) — they're the most
detailed, most exercised documentation in the repo.

## Environment variables

All configuration is via environment variables, normally supplied through a
`.env` file at the repo root (`cp .env.example .env` to start). `make up`
refuses to run without one.

| Variable | Required | Description |
|---|---|---|
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | Yes | Database name/user/password used by the `postgres` and `migrate` Compose services |
| `DATABASE_URL` | Yes | Full PostgreSQL connection string for the API server. Uses `localhost` for host-machine connections (keep in sync with the `POSTGRES_*` vars above); inside Docker, service-to-service traffic uses the `postgres` hostname instead |
| `JWT_SECRET` | Yes | JWT signing key, minimum 32 characters. Also reused to sign public share-link tokens |
| `JWT_TTL_HOURS` | No (default `24`) | Session token lifetime |
| `ENCRYPTION_KEY` | Yes for integrations/AI plugins | AES-256-GCM key (base64-encoded, 32 raw bytes) used to encrypt integration credentials and AI plugin API keys at rest |
| `LOG_LEVEL` | No (default `info`) | One of `debug`, `info`, `warn`, `error` (case-insensitive). `debug` additionally logs every outbound PagerDuty/GitHub/Slack call and every WebSocket event fan-out |
| `PAGERDUTY_API_KEY` | No | Enables PagerDuty on-call auto-population. Read-only — Sevitout never triggers pages |
| `GITHUB_TOKEN` | No | GitHub PAT with `repo` scope. Enables linking/creating GitHub Issues; without it, those endpoints return `503` and everything else still works |
| `JIRA_CLOUD_ID` / `JIRA_API_TOKEN` | No, but both are required together | Jira Cloud tenant's Cloud ID (a UUID — find it under `admin.atlassian.com`, not the site's `https://*.atlassian.net` name; see [Atlassian's API token docs](https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/), or verify it directly via `curl https://{site}.atlassian.net/_edge/tenant_info`) and API token, sent as a Bearer token (no account email needed). Enables linking/creating Jira issues; without both set, those endpoints return `503` and everything else still works |
| `JIRA_SITE_URL` | No | The Jira site's own URL (e.g. `https://acme.atlassian.net`) — independent of `JIRA_CLOUD_ID`/`JIRA_API_TOKEN` above (used for API calls, not this) and used purely to build a human-clickable `.../browse/{key}` link on created/linked Jira issues. Left unset, issue links fall back to the API's own non-browsable resource URL |
| `SLACK_APP_TOKEN` / `SLACK_BOT_TOKEN` | No, but all five Slack vars are required together | Socket Mode app-level token and bot OAuth token for the `slackbot` service |
| `API_GRPC_ADDR` | No, but required together with the other Slack vars | gRPC address of the API server as seen by the `slackbot` container (e.g. `api:8080`) |
| `SLACKBOT_SERVICE_EMAIL` / `SLACKBOT_SERVICE_PASSWORD` | No, but required together with the other Slack vars | Login credentials for the bot's own service-account user (an ordinary registered user, promoted to Admin) — the bot logs itself in and refreshes its own token, so there's no JWT to generate or rotate by hand |
| `VITE_API_BASE_URL` | No | Only used by `npm run dev`/`npm run build` in `web/` directly — Vite inlines it at build time, so it has no effect when set via Compose's `environment:`. Leave unset: the web app makes same-origin, relative-path API requests by default (Vite's dev proxy locally, nginx in the `web` container). The API server has no CORS support, so pointing this cross-origin won't work without adding it. There's a second, frontend-only `web/.env.example` with the same variable for local `npm` usage |

If any Slack bot variable is set without the rest, `slackbot` logs a warning
and exits cleanly (code `0`) rather than crash-looping — this is expected
when Slack isn't configured.

## Development

```bash
make build              # go build ./...
make test               # go test ./...
make test-integration   # integration tests against a real Postgres (needs .env / make up)
make lint                # golangci-lint run
make generate            # regenerate sqlc query code
make proto                # regenerate protobuf/gRPC/gateway/OpenAPI code from proto/
```

Frontend tests and build live under `web/` (`npm test`, `npm run build`).

## Documentation

| Doc | What it's for |
|---|---|
| [`docs/user-guide.md`](docs/user-guide.md) | How to use Sevitout — creating SEVs, understanding metrics, running postmortems, configuring integrations. Start here if you're an operator, IC, or admin |
| [`docs/architecture.md`](docs/architecture.md) | System design: services, database schema, API layer, key architectural decisions |
| [`docs/roadmap.md`](docs/roadmap.md) | The current phased plan for engineering, observability, and feature work — what's next and why, updated as phases ship |
| [`docs/architecture-evolution.md`](docs/architecture-evolution.md) | Proposed infra additions on top of `docs/architecture.md` — request-scoped logging, metrics, config package — and what's explicitly out of scope for now |
| [`docs/requirements.md`](docs/requirements.md) | Full functional specification |
| [`docs/project-plan.md`](docs/project-plan.md) | The milestone-by-milestone build plan (historical planning record — see `demo/` and `docs/user-guide.md` for how the system actually behaves today) |
| [`demo/`](demo/) | One runbook per milestone with exact `curl`/Slack/UI walkthroughs and known limitations — the most detailed reference for exact API behavior |

## License

[MIT](LICENSE)
