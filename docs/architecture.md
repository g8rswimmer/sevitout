# Sevitout — System Architecture

**Version**: 0.1 (draft)
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
│   │   ├── github/     # GitHub Issues link/create
│   │   └── monitoring/ # Dashboard link metadata (Datadog, Prometheus, CloudWatch)
│   ├── store/          # Repository interfaces + PostgreSQL implementations
│   │   ├── postgres/
│   │   └── queries/    # sqlc-generated query code
│   ├── auth/           # OAuth 2.0, JWT, RBAC middleware/interceptors
│   ├── api/
│   │   ├── grpc/       # gRPC service handler implementations
│   │   ├── gateway/    # gRPC-Gateway REST transcoding setup
│   │   └── ws/         # WebSocket hub and event broadcasting
│   └── config/         # App configuration (env, file)
├── proto/
│   └── sevitout/v1/    # Protobuf definitions (source of truth for all APIs)
├── migrations/         # PostgreSQL migration files (golang-migrate)
├── web/                # React frontend
│   ├── src/
│   │   ├── pages/
│   │   ├── components/
│   │   ├── hooks/
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

**gRPC services:**

| Service | Responsibility |
|---|---|
| `SEVService` | CRUD for SEV records, status transitions, role management |
| `PostmortemService` | Postmortem CRUD, status transitions, lock/unlock |
| `SearchService` | Full-text search and filtered listing of SEVs |
| `ConfigService` | Service registry, users, on-call, integration config, AI plugins, retention |
| `AIService` | Trigger AI actions, stream AI output, list AI plugin configurations |
| `AuditService` | Read audit log entries for a SEV |
| `AnnouncementService` | Announcements and updates on a SEV |
| `TaskService` | Linked task management |
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

The domain emits lifecycle events on status transitions via an internal event bus (Go channel). Subscribers:

- **AI plugin dispatcher** — triggers proactive AI actions (§11.1 of requirements)
- **Notification dispatcher** — sends Slack/email notifications
- **Slack integration** — auto-creates incident channel on SEV-1/SEV-2 open
- **WebSocket hub** — broadcasts `sev.status_changed` to subscribed clients
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
| `notification_config` | Role-based notification routing rules |
| `retention_config` | Per-severity-level retention policy |
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
| `log/slog` (stdlib) | Structured logging |
| `github.com/shurcooL/githubv4` | GitHub GraphQL API (Issues) |

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

---

## 8. AI Plugin System (`internal/ai`)

### Interface

All AI providers implement a single Go interface:

```go
type Provider interface {
    Summarize(ctx context.Context, sev *SEVContext) (string, error)
    SuggestRootCause(ctx context.Context, sev *SEVContext) ([]RootCauseSuggestion, error)
    DraftPostmortem(ctx context.Context, sev *SEVContext) (*PostmortemDraft, error)
    SuggestTasks(ctx context.Context, sev *SEVContext) ([]TaskSuggestion, error)
    FindSimilar(ctx context.Context, sev *SEVContext) ([]SimilarSEV, error)
    StreamAction(ctx context.Context, action Action, sev *SEVContext) (<-chan Chunk, error)
}
```

### Lifecycle dispatch

When a SEV status transition occurs, the lifecycle hook dispatches an async AI task via a buffered Go channel. A pool of worker goroutines picks up tasks, calls the configured provider, stores the result in `ai_outputs`, and broadcasts an `ai.output` WebSocket event to subscribed clients.

AI outputs are stored separately from the SEV record and are clearly marked as AI-generated in the UI. They do not mutate SEV fields directly — users must explicitly apply suggestions.

### Plugin registration

Plugins are registered in the `ai_plugins` table via the Config API. Each plugin record holds: name, version, handler type (`builtin` or `http`), provider, model, encrypted API key, enabled flag, and per-trigger-event enable/disable flags.

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
