# Sevitout — System Architecture

**Version**: 0.2 — as-built, updated as the system changes (no longer a
pre-implementation draft; see `docs/roadmap.md` for what's shipped and what's next)
**Stack**: Go (backend) · React/TypeScript (frontend) · PostgreSQL · gRPC + REST gateway · WebSockets · Docker Compose

---

## 1. Overview

Sevitout is composed of three runtime processes and a PostgreSQL database, orchestrated via Docker Compose for self-hosted deployment.

```
┌─────────────────────────────────────────────────────────┐
│  Browser (React)        Slack Workspace                  │
│       │                      │                           │
│  REST / WebSocket        Slack Events API                │
│       │                      │                           │
│  ┌────▼──────────┐    ┌──────▼────────┐                 │
│  │  API Server   │    │   Slack Bot   │                 │
│  │  (Go)         │◄───│   (Go)        │                 │
│  │  gRPC         │    │  gRPC client  │                 │
│  │  REST gateway │    └───────────────┘                 │
│  │  WebSocket    │                                       │
│  └──────┬────────┘                                       │
│         │                                                 │
│  ┌──────▼────────┐                                       │
│  │  PostgreSQL   │                                       │
│  └───────────────┘                                       │
└─────────────────────────────────────────────────────────┘
```

| Process | Description |
|---|---|
| `api` | gRPC server + REST gateway + WebSocket hub. Single binary; all core logic. |
| `slackbot` | Slack Events / Socket Mode handler. Calls the API server over gRPC. |
| `web` | React SPA served by nginx. Communicates with `api` over REST and WebSocket. |
| `postgres` | PostgreSQL 16. Single database for all application data. |

---

## 2. Repository Layout

```
sevitout/
├── cmd/
│   ├── server/         # API server entrypoint
│   └── slackbot/       # Slack bot entrypoint
├── internal/
│   ├── sev/            # Core domain: models, lifecycle state machine, business logic
│   ├── postmortem/     # Postmortem workflow and locking
│   ├── ai/             # AI plugin interface and provider implementations
│   ├── integrations/
│   │   ├── slack/      # Slack client (announcements, channel creation, chat capture)
│   │   ├── pagerduty/  # PagerDuty on-call lookup
│   │   ├── tasktracker/
│   │   │   ├── github/ # GitHub Issues link/create
│   │   │   └── jira/   # Jira Issues link/create
│   │   ├── catalog/    # Static field-schema registry driving the admin
│   │   │                # integrations UI and its upsert validation
│   │   ├── email/      # SMTP client for notification delivery (§4.2, Phase 15)
│   │   └── monitoring/ # Unused placeholder (.gitkeep only) — Monitoring is
│   │                    # settings-only (tool + base URL via catalog above),
│   │                    # with no live client of its own
│   ├── store/          # Repository interfaces + PostgreSQL implementations
│   │   ├── postgres/
│   │   └── queries/    # sqlc-generated query code
│   ├── auth/           # JWT, RBAC middleware/interceptors
│   ├── telemetry/      # Request-ID + context-bound *slog.Logger propagation (§3.4)
│   ├── api/
│   │   ├── grpc/       # gRPC service handler implementations
│   │   ├── gateway/    # Unused placeholder (.gitkeep only) — the actual
│   │   │                # grpc-gateway REST transcoding setup lives directly
│   │   │                # in cmd/server/main.go, not in a separate package
│   │   └── ws/         # WebSocket hub and event broadcasting
│   └── config/         # Typed env-var configuration, loaded once at startup
├── proto/
│   └── sevitout/v1/    # Protobuf definitions (source of truth for all APIs)
├── migrations/         # PostgreSQL migration files (golang-migrate)
├── web/                # React frontend
│   ├── src/
│   │   ├── pages/
│   │   ├── components/
│   │   └── lib/        # API client, WebSocket client, auth
│   └── ...
├── deploy/
│   └── docker-compose.yml
└── docs/
```

---

## 3. API Layer

### 3.1 gRPC + REST Gateway

All API capabilities are defined in Protocol Buffers under `proto/sevitout/v1/`. The REST API is generated automatically by [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway) from proto annotations — there is no hand-written REST routing.

**gRPC services** (15, one file per service under `proto/sevitout/v1/`):

| Service | Responsibility |
|---|---|
| `SEVService` | CRUD for SEV records, status transitions |
| `RoleService` | Assign/remove/list roles on a SEV (IC, Communications Lead, Recorder, Responders, On-call) |
| `SEVAccessService` | Grant/revoke/list per-user visibility into a Sensitive SEV |
| `SEVLinkService` | Typed bidirectional SEV-to-SEV relationships (related, caused-by, duplicate, recurrence-of) |
| `PostmortemService` | Postmortem CRUD, status transitions, lock/unlock |
| `SearchService` | Full-text search and filtered listing of SEVs |
| `ReportService` | Dashboard metrics, MTTR/frequency trends, CSV export |
| `ConfigService` | Service registry, users, on-call, integration config + catalog, AI plugins, retention, service SLA targets, per-service leveling criteria, notification routing rules, escalation thresholds |
| `AIService` | Trigger AI actions, stream AI output, list AI plugin configurations |
| `AuditService` | Read audit log entries for a SEV |
| `AuthService` | Login/register, `WhoAmI` |
| `AnnouncementService` | Announcements and updates on a SEV |
| `ChatService` | Chat/communication log entries |
| `TaskService` | Linked task management (GitHub Issues, Jira Issues) |
| `ShareService` | Generate and revoke public shareable links |

The REST gateway runs on the same port as the gRPC server using the `cmux` multiplexer — gRPC (h2c) and HTTP/1.1 are served on the same listener.

OpenAPI v3 documentation is generated from proto annotations and served at `/openapi.json`.

### 3.2 WebSocket

The WebSocket endpoint (`/ws`) is served by the same `api` binary. On connect, the authenticated client subscribes to one or more SEV IDs. When any mutation occurs on a subscribed SEV, the hub broadcasts a typed event to all connected subscribers.

**Event types pushed over WebSocket:**

| Event | Trigger |
|---|---|
| `sev.updated` | Any field on the SEV record changes |
| `sev.status_changed` | Status transition |
| `announcement.created` | New announcement posted |
| `chat.created` | New chat log entry |
| `role.changed` | Role assignment added or removed |
| `task.linked` / `task.updated` | Task linked or SLA updated |
| `ai.output` | AI plugin produced output (streamed chunk or complete) |
| `postmortem.updated` | Postmortem content or status changed |

The React frontend uses WebSocket events to invalidate TanStack Query cache entries, triggering re-fetches of affected data without full-page refreshes.

### 3.3 Auth Interceptor

All gRPC calls pass through a server-side unary and stream interceptor that:

1. Validates the JWT from the `Authorization: Bearer` header
2. Resolves the user's organization role from the database
3. Attaches the user context to the request
4. Enforces RBAC — unauthenticated or unauthorized calls are rejected before reaching the service handler

Public shareable link views bypass JWT auth and use a signed token in the URL path (`/s/:token`), validated separately in the HTTP gateway layer.

### 3.4 Observability

Three unauthenticated endpoints share `httpMux` alongside `/openapi.json`
(deliberately: this matches the standard scrape/probe convention for each,
not an accidental gap in the otherwise fully JWT-gated surface):

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness/readiness — checks only that the database is reachable (`Stores.Ping`; a no-op against the in-memory dev fallback). Distinct from `GET /admin/integrations/health` below |
| `GET /metrics` | Prometheus text format (`prometheus/client_golang`): RPC request/duration counters and histograms (folded into the existing `logRPC` helper rather than a second interceptor), a WebSocket connection gauge, an AI-dispatch outcome counter, `pgxpool.Pool.Stat()`-derived DB pool gauges, and a periodically-refreshed open-SEV-count gauge by severity |
| `GET /admin/integrations/health` | Admin-only (JWT + RBAC checked by hand, since this is a plain `net/http` handler bypassing the gRPC interceptor chain) — live connectivity check against each *configured* third-party integration (PagerDuty/GitHub/Jira/Slack), run concurrently. Distinct from `/healthz`: this is about third-party reachability, not process liveness |

**`internal/telemetry`** provides the cross-cutting request-ID + bound-logger
plumbing every one of the above (and every other RPC) rides on:

- `RequestIDUnaryInterceptor`/`RequestIDStreamInterceptor` sit outermost in
  the interceptor chain (ahead of the auth interceptor in §3.3, ahead of
  logging) — `context.WithValue` only propagates inward, so request-ID
  generation has to run before anything that wants to log with it,
  including an auth *rejection*.
- `telemetry.WithLogger`/`LoggerFromContext` stash and retrieve a
  `*slog.Logger` pre-bound with `request_id` and `user_id` via `log.With(...)`;
  handlers call `telemetry.LoggerFromContext(ctx)` once instead of
  re-deriving `user_id` from `auth.UserFromContext` at each call site.
  Background work with no live request (the AI dispatcher's worker pool)
  falls back to `slog.Default()`.
- The same request ID is bridged through grpc-gateway (`X-Request-Id`
  header ↔ `x-request-id` gRPC metadata) and through the three standalone
  `net/http` handlers (`/ws`, `/admin/integrations/health`, `/s/{token}`),
  so one correlation ID survives every hop for a given request.
- A shared `internalError(ctx, msg, err)` helper (`internal/api/grpc/errors.go`)
  logs `err`'s real detail via this bound logger at Error level while still
  returning the same generic `status.Error(codes.Internal, msg)` to the
  caller — the wire contract stays unchanged; the previously-discarded
  detail now reaches the logs.

See `docs/architecture-evolution.md` for the full design rationale (library
choice, interceptor ordering, non-goals) and `demo/healthz.md`,
`demo/metrics.md`, `demo/request-scoped-logging.md`,
`demo/internal-error-cleanup.md` for exact, live-verified walkthroughs.

---

## 4. Core Domain (`internal/sev`)

The SEV domain package owns the core business logic and does not import any integration or transport packages.

### 4.1 State Machine

Status transitions are enforced by a state machine. Invalid transitions are rejected at the service layer before any database write.

```
Open ──► Investigating ──► Mitigated ──► Resolved ──► Postmortem In Progress ──► Postmortem Complete
  │                                                                                      │
  └──────────────────────── (re-open) ◄─────────────────────────────────────────────────┘
```

Every transition records: `from_status`, `to_status`, `user_id`, `transitioned_at` in `sev_status_history`.

### 4.2 Lifecycle Hooks

There is no generic internal event bus — each side effect is a direct call
from the mutating `internal/api/grpc` handler (`SEVServer`,
`AnnouncementServer`, `PostmortemServer`) after its write succeeds, mirroring
the existing `publishProto`/`auditAppendBestEffort` calls already made at
each of those sites:

- **AI plugin dispatcher** (`internal/ai.Dispatcher`) — triggers proactive AI
  actions (§11.1 of requirements) via a buffered-channel worker pool (§8)
- **Notifier** (`internal/api/grpc/notify.go`, Phase 15) — evaluates
  admin-configured routing rules and delivers a Slack message and/or email
  for SEV create/update/status-change, announcement, postmortem due/approved,
  and escalation/SLA-risk events; best-effort, same contract as
  `auditAppendBestEffort`. Two background scanners (started in
  `cmd/server/main.go` alongside the metrics refresher) also call into it on
  a 1-minute tick rather than from a request handler: one flags SEVs open too
  long with no Incident Commander (`sev.escalation_no_ic`), the other flags
  SEVs newly at-risk-of or in breach of their service's SLA
  (`sev.sla_at_risk`/`sev.sla_breached`) — see `docs/roadmap.md` Phase 15 and
  Phase 12 for the SLA evaluation logic it reuses (`internal/sev/sla.go`)
- **Slack integration** — auto-creates incident channel on every SEV open,
  regardless of severity; owned by `cmd/slackbot`, not the API server (§7)
- **WebSocket hub** (`Publisher`) — broadcasts `sev.status_changed` and the
  other event types in §3.2 to subscribed clients
- **Metrics recorder** — computes and stores MTTD/MTTM/MTTR/DTTM when relevant timestamps are set

### 4.3 Post-Postmortem Lock

When a SEV transitions to `Postmortem Complete`, the record is flagged `locked = true`. All mutation handlers check this flag and reject writes unless an unlock token is present. Unlocking requires a written reason, which is stored in the audit log alongside the user and timestamp. The record auto-relocks on the next save.

---

## 5. Database Schema (PostgreSQL)

Full-text search uses PostgreSQL's native `tsvector`/`tsquery` with GIN indexes — no external search engine required.

### Core tables

| Table | Purpose |
|---|---|
| `sevs` | Primary SEV record (all core fields) |
| `sev_status_history` | Immutable log of status transitions |
| `sev_roles` | People/role assignments per SEV (supports multiple per role type) |
| `sev_announcements` | Ordered status updates with audience tagging |
| `sev_chat_log` | Chat/communication log entries |
| `sev_linked_tasks` | External task references with SLA due dates |
| `sev_links` | Typed SEV-to-SEV relationships (bidirectional) |
| `sev_slis` | SLI violation records per SEV |
| `postmortems` | Postmortem document (content stored as Markdown) and status per SEV |
| `audit_log` | Immutable append-only mutation log; only the `audit_writer` DB role has INSERT — no UPDATE or DELETE granted |
| `ai_outputs` | Stored AI-generated content per SEV and trigger event |

### Configuration tables

| Table | Purpose |
|---|---|
| `services` | Service registry |
| `users` | Registered users (email + bcrypt password hash) |
| `oncall_rotations` | On-call rotation definitions and overrides |
| `ai_plugins` | Registered AI plugin configurations |
| `integration_config` | Per-integration credentials and settings |
| `notification_config` | Role/event → Slack-or-email routing rules, with an optional max-severity filter (`docs/requirements.md` §16, Phase 15) |
| `escalation_config` | Per-severity-level "no Incident Commander" alert threshold (minutes) and enabled flag (Phase 15) |
| `retention_config` | Per-severity-level retention policy |
| `service_slas` | Per-service, per-severity-level SLA targets (Phase 12) |
| `service_leveling_criteria` | Per-service severity-leveling guidance text (Phase 14) |
| `shareable_links` | Public link tokens (signed, revocable) |

### Full-text search

The `sevs` table carries a `search_vector tsvector` column populated by a trigger on insert/update. It indexes: title, description, root cause description, business impact. Announcements are indexed separately in `sev_announcements.search_vector`.

---

## 6. Key Go Libraries

| Library | Use |
|---|---|
| `google.golang.org/grpc` | gRPC server and client |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | REST transcoding from proto annotations |
| `google.golang.org/protobuf` | Proto generated code |
| `github.com/jackc/pgx/v5` | PostgreSQL driver |
| `github.com/sqlc-dev/sqlc` | Generate type-safe Go from SQL queries |
| `github.com/golang-migrate/migrate/v4` | Database migrations |
| `github.com/gorilla/websocket` | WebSocket server |
| `golang.org/x/crypto` | bcrypt password hashing |
| `github.com/golang-jwt/jwt/v5` | JWT session tokens |
| `github.com/slack-go/slack` | Slack API client (bot + events) |
| `log/slog` (stdlib) | Structured logging, request-ID/user-bound via `internal/telemetry` |
| `github.com/prometheus/client_golang` | `/metrics` — RPC, WebSocket, AI-dispatch, and DB-pool metrics |
| `net/http` (stdlib) | GitHub Issues, Jira Issues, and PagerDuty clients are all plain hand-rolled REST clients over `net/http` — no GraphQL or vendor SDK for any of the three |

SQL queries are written by hand and type-checked by `sqlc`. No ORM.

---

## 7. Slack Bot (`cmd/slackbot`)

The Slack bot runs as a separate binary and connects to Slack via **Socket Mode** (no public ingress required). It translates Slack events and slash commands into gRPC calls to the API server.

**Responsibilities:**
- Handle `/sev open`, `/sev update`, `/sev resolve` slash commands
- Respond to `@sevbot status`, `@sevbot timeline` in-channel mentions
- Create a dedicated incident channel on every SEV open, regardless of severity (via Slack API), invite IC, on-call, and the `/sev open` caller
- Push announcements from Sevitout to configured Slack channels
- Capture messages from an incident channel into the SEV chat log

The bot authenticates to the API server as a dedicated service-account user, logging
itself in via `POST /auth/login` (the same host:port as its gRPC connection — the API
server multiplexes both over one TCP port) using a durable `SLACKBOT_SERVICE_EMAIL` /
`SLACKBOT_SERVICE_PASSWORD` credential pair, rather than a manually pre-issued,
manually rotated JWT. It refreshes proactively on a fixed interval and reactively on
any RPC the server rejects as unauthenticated, so it stays authenticated indefinitely
without operator intervention.

**Credential resolution**: like PagerDuty/GitHub/Jira's own resolvers in
`cmd/server` (configured via `ConfigService.UpsertIntegrationConfig`), the
bot prefers a datastore-configured `bot_token`/`app_token` pair over the
static `SLACK_BOT_TOKEN`/`SLACK_APP_TOKEN` env vars, resolved once at startup via a
narrowly-scoped RPC (`ConfigService.GetSlackBotCredential`, gated to this one
service account specifically — the only RPC in the system that returns a
decrypted credential over the wire at all). The REST calls above (channel
creation, messages, invites, history, user lookup) pick up a *later* config
change within one polling interval, no restart needed; the long-lived Socket
Mode connection itself does not — reconnecting it live is a deferred
follow-up (see `demo/datastore-slack-bot-credentials.md`).

---

## 8. AI Plugin System (`internal/ai`)

### Interface

All AI providers implement a single Go interface. This extends the original
sketch of this interface with `SuggestResponders` and `DraftAnnouncement` so
every proactive trigger (§11.1) and user-triggered action (§11.2) has a
concrete method to call:

```go
type Provider interface {
    Summarize(ctx context.Context, sev *SEVContext) (string, error)
    SuggestRootCause(ctx context.Context, sev *SEVContext) ([]RootCauseSuggestion, error)
    DraftPostmortem(ctx context.Context, sev *SEVContext) (*PostmortemDraft, error)
    SuggestTasks(ctx context.Context, sev *SEVContext) ([]TaskSuggestion, error)
    FindSimilar(ctx context.Context, sev *SEVContext) ([]SimilarSEV, error)
    SuggestResponders(ctx context.Context, sev *SEVContext) ([]ResponderSuggestion, error)
    DraftAnnouncement(ctx context.Context, sev *SEVContext) (string, error)
    StreamAction(ctx context.Context, action Action, sev *SEVContext) (<-chan Chunk, error)
}
```

Two built-in implementations satisfy `Provider`: `AnthropicProvider` (a real
HTTP client for Anthropic's Messages API — the `handler_type = "builtin"`
case) and `HTTPProvider` (POSTs one JSON request per action to an externally
configured endpoint — `handler_type = "http"`). `StreamAction` in both runs
the action to completion and re-emits the result as a handful of
word-chunked pieces rather than true token-level streaming (a v1
simplification; see `demo/M12-ai-plugin.md`).

### Lifecycle dispatch

`Dispatcher` (`internal/ai/dispatcher.go`) is both the proactive and the
on-demand entry point:

- **Proactive** (§11.1): `internal/api/grpc`'s `SEVServer`/`PostmortemServer`
  call `Dispatcher.Dispatch(event, sevID)` after a successful mutation — SEV
  create (SEV-1/SEV-2 only), transition to Mitigated/Resolved, and postmortem
  transition to In Review. `Dispatch` enqueues onto a buffered channel a pool
  of worker goroutines drains; it never blocks the calling RPC, and a full
  queue drops the task (logged) rather than delaying the mutation. Each
  trigger event maps to one or more `Action`s and only runs for plugins with
  the matching `trigger_on_*` flag enabled.
- **On-demand** (§11.2, `AIService.TriggerAction`/`StreamAction`):
  `Dispatcher.Run`/`StreamOne` execute synchronously on the caller's
  goroutine instead of going through the queue.

Both paths funnel through the same core: resolve the plugin, enforce its
per-minute rate limit (`RateLimiter`, a fixed-window counter), decrypt its API
key, build the `Provider`, assemble a `SEVContext` (the SEV's fields, its
status-history-and-announcements timeline, and up to 5 same-service SEVs as
similarity candidates), call the action, store the result in `ai_outputs`,
and broadcast an `ai.output` WebSocket event.

A SEV opts out of all dispatch (proactive and on-demand) via its
`ai_disabled` flag (§11.3). Sensitive SEVs are *always* excluded from
proactive dispatch, regardless of `ai_disabled` — their content is never
sent to a configured AI plugin, consistent with their other field-level
visibility restrictions.

AI outputs are stored separately from the SEV record (`ai_outputs`, exposed
via `AIService.ListOutputs`) and are clearly marked as AI-generated in the
UI. They do not mutate SEV fields directly — users must explicitly apply
suggestions.

### Plugin registration

Plugins are registered in the `ai_plugins` table. Admin CRUD
(`CreateAIPlugin`/`GetAIPlugin`/`UpdateAIPlugin`/`DeleteAIPlugin`/`ListAIPlugins`)
lives on `ConfigService` (§18.6), alongside every other admin resource. Each
plugin record holds: name, version, handler type (`builtin` or `http`),
provider, model, encrypted API key, enabled flag, per-trigger-event
enable/disable flags, and a per-minute rate limit. `AIService.ListPlugins` is
a separate, read-only, non-admin RPC any authenticated user can call to see
which enabled plugins are available to trigger an action against — it never
returns credentials or handler internals.

---

## 9. Frontend (React)

**Stack:** React 18 · TypeScript · Vite · React Router v6 · TanStack Query · Tailwind CSS · shadcn/ui · TipTap with Markdown extension (rich text; serializes to/from Markdown for storage)

### Routes

| Route | Page |
|---|---|
| `/` | Dashboard — active SEVs, MTTR trend, overdue tasks |
| `/sevs` | SEV list with search, filter, sort |
| `/sevs/new` | Create SEV |
| `/sevs/:id` | SEV detail — live view during incident |
| `/sevs/:id/postmortem` | Postmortem editor |
| `/admin` | Admin overview |
| `/admin/services` | Service registry |
| `/admin/users` | User management |
| `/admin/oncall` | On-call configuration |
| `/admin/integrations` | Integration settings |
| `/admin/ai` | AI plugin configuration |
| `/admin/retention` | Data retention policy |
| `/admin/notifications` | Notification routing rules and escalation thresholds (Phase 15) |
| `/s/:token` | Public shareable SEV view (no auth required) |

### Data fetching

TanStack Query manages all server state. REST API calls are made to the gRPC-Gateway endpoints. The WebSocket client subscribes to the active SEV's room and calls `queryClient.invalidateQueries` on relevant events, triggering background refetches.

### Auth flow

1. User submits email + password to `POST /auth/login` (or `POST /auth/register` for first-time setup)
2. Backend verifies bcrypt hash (or hashes and stores on register), issues a JWT
3. Token returned in JSON response body and set as an `httpOnly` cookie
4. Cookie sent automatically on all subsequent API requests
5. On 401, React Router redirects to login page

---

## 10. Deployment (Docker Compose)

```yaml
services:
  postgres:
    image: postgres:16
    volumes: [postgres_data:/var/lib/postgresql/data]
    environment: [POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD]

  migrate:
    build: .
    command: migrate -path /migrations -database $DATABASE_URL up
    depends_on: [postgres]

  api:
    build: .
    command: /app/server
    ports: ["8080:8080"]   # gRPC + REST gateway + WebSocket
    depends_on: [migrate]
    environment: [DATABASE_URL, JWT_SECRET, JWT_TTL_HOURS, ENCRYPTION_KEY]

  slackbot:
    build: .
    command: /app/slackbot
    depends_on: [api]
    environment: [SLACK_APP_TOKEN, SLACK_BOT_TOKEN, API_GRPC_ADDR]

  web:
    build: ./web
    ports: ["3000:80"]     # nginx serving React SPA
    environment: [VITE_API_BASE_URL]
```

All secrets are passed via environment variables (or a `.env` file for local development). Production deployments should use a secrets manager.

---

## 11. Key Architectural Decisions

| Decision | Choice | Reason |
|---|---|---|
| API definition | Protobuf + gRPC-Gateway | Single source of truth for gRPC and REST; OpenAPI docs auto-generated; strong typing for Slack bot ↔ API calls |
| SQL access | sqlc (no ORM) | Type-safe queries, no reflection overhead, full control over query plans |
| Full-text search | PostgreSQL native (`tsvector`) | Avoids an additional Elasticsearch/Typesense dependency; sufficient for single-org scale |
| Real-time | WebSocket (gorilla/websocket) | Active SEV views need instant updates; SSE would require multiple connections for multiplexed events |
| Slack connectivity | Socket Mode | No public ingress required for self-hosted deployment |
| AI dispatch | In-process goroutine pool | No external queue dependency for v1; replaceable with a queue later if volume requires it |
| Frontend state | TanStack Query + WS invalidation | Server state stays in the query cache; WebSocket events trigger targeted invalidations rather than full refetches |
| Postmortem storage | Markdown in PostgreSQL | Portable and human-readable outside the UI; TipTap Markdown extension handles editor serialization |
| Credential encryption | AES-256-GCM (application layer) | Encrypt on write / decrypt on read in the store layer; DB never holds plaintext; single `ENCRYPTION_KEY` env var covers all secrets |
| Audit log immutability | DB role + application layer | `audit_writer` role has INSERT-only on `audit_log`; store exposes only `Append` — defense-in-depth against both app bugs and direct DB access |
| Session management | JWT-only, 24h default TTL | Stateless; no session table; short TTL limits post-deactivation window; configurable via `JWT_TTL_HOURS` |

---

## 12. Resolved Architectural Decisions

1. ~~**Rich text storage**: Should postmortem documents be stored as ProseMirror JSON (TipTap native), Markdown, or HTML in PostgreSQL? ProseMirror JSON preserves structure best but is editor-specific.~~
   - **Answered**: Postmortem documents are stored as Markdown in PostgreSQL. The TipTap editor will use a Markdown extension for serialization/deserialization. Markdown is portable, human-readable outside the UI, and easy to export.
2. ~~**AI API key encryption**: What encryption mechanism for AI provider keys at rest — application-level AES-GCM with a key from env, or defer to PostgreSQL transparent data encryption?~~
   - **Answered**: Application-level AES-256-GCM. The encryption key is provided via environment variable (`ENCRYPTION_KEY`). Keys are encrypted before write and decrypted after read in the store layer — the database never holds plaintext. The same mechanism applies to any other stored credentials (integration tokens, OAuth secrets).
3. ~~**Audit log immutability**: Enforce at DB level (append-only role, no UPDATE/DELETE on `audit_log`) or application level only?~~
   - **Answered**: Enforced at both levels. At the DB level: a dedicated `audit_writer` PostgreSQL role has INSERT-only privileges on `audit_log` — no UPDATE or DELETE granted. At the application level: the store layer exposes only an `Append` method; no update or delete operations exist in code. Defense-in-depth ensures immutability even if application logic is bypassed.
4. ~~**Session management**: JWT-only (stateless) or store sessions in PostgreSQL to support instant revocation on user deactivation?~~
   - **Answered**: JWT-only (stateless). Tokens are short-lived (configurable, default 24h) to limit the window after user deactivation. No session table required.
5. ~~**Authentication provider**: OAuth 2.0 via Google/GitHub, or internal email+password?~~
   - **Answered**: Internal email+password with bcrypt hashing (`golang.org/x/crypto/bcrypt`, cost 12). OAuth was removed because requiring external provider credentials creates friction for local development and self-hosted deployments. The first registered user receives the Admin role automatically for bootstrapping.
