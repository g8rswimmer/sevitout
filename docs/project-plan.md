# Sevitout — V1 Project Plan

**Version**: 0.1 (draft)
**Scope**: V1 as defined in `docs/requirements.md` and `docs/architecture.md`

---

## Overview

Each milestone produces a runnable, testable slice of the system. Milestones are ordered so that each one builds on the previous and can be demonstrated independently. A `demo/` directory at the repo root holds a markdown runbook for each milestone — step-by-step instructions to spin up, exercise, and verify the work.

### Completion criteria for every milestone
- Unit tests written and passing (`go test ./...`)
- Linter passing (`golangci-lint run`)
- Demo runbook (`demo/MXX-*.md`) executable top-to-bottom with no errors
- Docker Compose stack starts cleanly with the milestone's services

---

## Milestone Dependency Graph

```
M00 ──► M01 ──► M02 ──► M03 ──► M04 ──► M05 (Postmortem)
                  │       │       │
                  │       │       ├──► M06 (Announcements / Chat / Linked SEVs)
                  │       │       │
                  │       │       ├──► M07 (Linked Tasks / GitHub Issues)
                  │       │       │
                  │       ├──► M08 (Search & Filtering)
                  │       │
                  │       └──► M09 (WebSocket / Real-time)
                  │
                  └──► M10 (Configuration API)
                           │
                           ├──► M11 (Slack Bot)  ← also needs M09
                           │
                           └──► M12 (AI Plugin System)  ← also needs M05
                                         │
                           M13 (Reporting & Analytics / Public Links)
                                ← needs M04, M05, M06, M07
                                         │
                           M14 (React Frontend)
                                ← needs all backend milestones
```

---

## Milestones

---

### M00 — Project Foundation

**Goal**: Establish the project skeleton, tooling, and local development environment. Nothing runs yet except the database.

**Deliverables**:
- `go.mod` initialized (`github.com/g8rswimmer/sevitout`)
- Full directory structure created (`cmd/`, `internal/`, `proto/`, `migrations/`, `web/`, `deploy/`, `demo/`)
- `Makefile` with targets: `build`, `test`, `lint`, `up`, `down`, `migrate`
- `golangci-lint` config (`.golangci.yml`) with: `errcheck`, `gosimple`, `govet`, `staticcheck`, `unused`, `gofmt`, `goimports`
- `deploy/docker-compose.yml` with `postgres` and `migrate` services only
- First migration: empty schema with `schema_migrations` table
- `.env.example` with all required environment variable keys
- `demo/M00-foundation.md`

**Dependencies**: none

**Tests**: none yet (no source code)

---

### M01 — Database Schema & Store Interfaces

**Goal**: All PostgreSQL tables created via migrations. `sqlc` generates type-safe query code. Repository interfaces defined. In-memory fakes implemented for all interfaces (used in unit tests throughout the project).

**Deliverables**:
- Migrations for all tables: `sevs`, `sev_status_history`, `sev_roles`, `sev_announcements`, `sev_chat_log`, `sev_linked_tasks`, `sev_links`, `sev_slis`, `postmortems`, `audit_log`, `ai_outputs`, `services`, `users`, `oncall_rotations`, `ai_plugins`, `integration_config`, `notification_config`, `retention_config`, `shareable_links`
- `audit_writer` PostgreSQL role created in migration; `audit_log` GRANT restricted to INSERT only
- `search_vector tsvector` column on `sevs` and `sev_announcements` with GIN indexes and update trigger
- `sqlc.yaml` config; `internal/store/queries/` populated with generated Go code
- Repository interfaces in `internal/store/`:
  - `SEVStore`, `PostmortemStore`, `AuditStore`, `AnnouncementStore`, `ChatStore`, `TaskStore`, `SEVLinkStore`, `SLIStore`, `UserStore`, `ServiceStore`, `OnCallStore`, `AIPluginStore`, `IntegrationConfigStore`, `ShareStore`
- In-memory implementations of all interfaces in `internal/store/memory/`
- `demo/M01-database.md`

**Dependencies**: M00

**Tests**:
- Migration applies and rolls back cleanly
- In-memory store implementations pass interface compliance tests
- `audit_writer` role cannot UPDATE or DELETE `audit_log` (verified in integration test against real DB)

---

### M02 — SEV Core API (Unauthenticated)

**Goal**: A running gRPC + REST gateway server that can create, read, update, and transition SEVs. No auth yet — all endpoints open. This is the first milestone where you can make real API calls.

**Deliverables**:
- `proto/sevitout/v1/sev.proto`: `SEVService` with RPCs: `CreateSEV`, `GetSEV`, `UpdateSEV`, `ListSEVs`, `TransitionStatus`
- `proto/sevitout/v1/audit.proto`: `AuditService` with `ListAuditEntries`
- `cmd/server/main.go`: gRPC server + grpc-gateway HTTP mux on `:8080`, `cmux` listener
- `internal/api/grpc/sev.go`: service handler implementations
- `internal/sev/` domain package: SEV model, status state machine, derived metrics computation (MTTD/MTTM/MTTR/DTTM)
- Audit log written on every SEV mutation
- OpenAPI spec generated and served at `GET /openapi.json`
- Docker Compose: `api` service added
- `demo/M02-sev-api.md`

**Dependencies**: M01

**Tests**:
- State machine unit tests: valid and invalid transitions, all paths
- Derived metrics computation unit tests (fixed timestamps → expected durations)
- gRPC handler unit tests using in-memory store fakes
- Integration test: create → transition → verify audit log entries

---

### M03 — Authentication & Authorization

**Goal**: OAuth 2.0 login via Google and GitHub. JWT issuance. RBAC enforced on all gRPC endpoints. Users created on first login.

**Deliverables**:
- `proto/sevitout/v1/auth.proto`: `AuthService` with `InitiateOAuth`, `OAuthCallback`, `WhoAmI`
- `internal/auth/` package: OAuth 2.0 flow (Google + GitHub), JWT sign/validate (`golang-jwt/jwt/v5`), RBAC role definitions and permission map
- gRPC unary + stream interceptors: JWT validation → user context attachment → RBAC enforcement
- `users` table populated on first OAuth callback
- HTTP endpoints for OAuth redirect and callback (served via gateway, not gRPC)
- `JWT_SECRET`, `JWT_TTL_HOURS`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` env vars wired
- `demo/M03-auth.md`

**Dependencies**: M02

**Tests**:
- JWT sign and validate unit tests (valid, expired, tampered)
- RBAC unit tests: each role × each RPC → allow/deny table
- OAuth callback handler unit test with mocked provider
- Integration test: unauthenticated call → 401; authenticated call → 200

---

### M04 — SEV Roles, People & PagerDuty On-Call

**Goal**: Role assignments on a SEV (IC, on-call, comms lead, etc.). PagerDuty on-call auto-population on SEV create. Derived metrics stored when timestamps are set.

**Deliverables**:
- `proto/sevitout/v1/role.proto`: `RoleService` with `AssignRole`, `RemoveRole`, `ListRoles`
- `internal/api/grpc/role.go`: handler
- `internal/integrations/pagerduty/` package: `OnCallLookup(serviceID string) (string, error)` — calls PagerDuty API; returns current on-call user name/email
- On SEV create: if `service_id` provided and PagerDuty configured, auto-populate on-call role
- Derived metrics (MTTD/MTTM/MTTR/DTTM) computed and stored on `sevs` each time a relevant timestamp is set via status transition
- `PAGERDUTY_API_KEY` env var; integration disabled gracefully if not set
- `demo/M04-roles-oncall.md`

**Dependencies**: M03

**Tests**:
- Role assignment and removal unit tests
- Derived metrics unit tests for all four formulas
- PagerDuty client unit test with HTTP mock
- Integration test: create SEV for a configured service → verify on-call field populated

---

### M05 — Postmortem

**Goal**: Postmortem document attached to every SEV. Full status workflow. Post-postmortem lock requiring a written reason to unlock.

**Deliverables**:
- `proto/sevitout/v1/postmortem.proto`: `PostmortemService` with `GetPostmortem`, `UpdatePostmortem`, `TransitionPostmortemStatus`, `UnlockSEV`
- `internal/api/grpc/postmortem.go`: handler
- `internal/postmortem/` package: postmortem state machine (`Draft → In Review → Approved`), lock enforcement, unlock-reason validation
- Postmortem auto-created (empty Draft) when a SEV is created
- `postmortems.content` stored as Markdown text
- On SEV transition to `Postmortem Complete`: `sevs.locked = true`; all subsequent SEV mutation handlers reject writes without a valid unlock token
- Unlock: `UnlockSEV` RPC accepts `reason` string, writes to audit log, returns short-lived unlock token (JWT scoped to that SEV ID); token required on next write
- `demo/M05-postmortem.md`

**Dependencies**: M04

**Tests**:
- Postmortem state machine unit tests (valid/invalid transitions)
- Lock enforcement unit tests: write before lock (ok), write after lock without token (rejected), write with valid token (ok), re-lock after save
- Unlock reason audit log unit test
- Integration test: full SEV lifecycle → postmortem approve → verify lock → unlock with reason → edit → verify re-lock

---

### M06 — Announcements, Chat Log & Linked SEVs

**Goal**: Time-ordered status updates with audience targeting. Chat log capture. Typed SEV-to-SEV relationships.

**Deliverables**:
- `proto/sevitout/v1/announcement.proto`: `AnnouncementService` — `CreateAnnouncement`, `ListAnnouncements`
- `proto/sevitout/v1/chat.proto`: `ChatService` — `AddChatEntry`, `ListChatEntries`
- `proto/sevitout/v1/sev_link.proto`: `SEVLinkService` — `LinkSEVs`, `UnlinkSEVs`, `ListLinkedSEVs`
- Audience types enforced: `internal`, `external`, `status-page`
- SEV links bidirectional: linking A→B inserts two rows (A→B and B→A)
- Announcement `search_vector` populated by DB trigger
- `demo/M06-announcements-chat-links.md`

**Dependencies**: M03

**Tests**:
- Announcement ordering and audience filter unit tests
- Bidirectional SEV link unit tests (link, verify both directions, unlink)
- Chat log entry ordering unit test
- Integration test: post announcements with mixed audiences → list filtered by audience

---

### M07 — Linked Tasks & GitHub Issues Integration

**Goal**: Link GitHub Issues to a SEV. Create new GitHub Issues pre-filled with SEV context. SLA due dates auto-set by priority. Overdue detection.

**Deliverables**:
- `proto/sevitout/v1/task.proto`: `TaskService` — `LinkTask`, `UnlinkTask`, `ListTasks`, `UpdateTaskDueDate`
- `internal/api/grpc/task.go`: handler
- `internal/integrations/github/` package: `GetIssue(owner, repo, number)`, `CreateIssue(owner, repo, title, body)` — uses `shurcooL/githubv4`
- SLA due date logic: on link, default due date = SEV `resolved_at` + 30 days (critical) or 90 days (non-critical); `resolved_at` not yet set → due date calculated when SEV resolves
- Overdue flag: task is overdue if `due_date < now` and issue is not closed
- `GITHUB_TOKEN` env var; integration disabled gracefully if not set
- `demo/M07-linked-tasks.md`

**Dependencies**: M03

**Tests**:
- SLA due date calculation unit tests (critical, non-critical, manual override, resolved vs. unresolved SEV)
- Overdue detection unit tests
- GitHub client unit tests with HTTP mock (get issue, create issue)
- Integration test: link task → verify due date → override due date → verify overdue flag after date passes

---

### M08 — Search & Filtering

**Goal**: Full-text search across SEVs and announcements. All filter, sort, and quick-view capabilities.

**Deliverables**:
- `proto/sevitout/v1/search.proto`: `SearchService` — `SearchSEVs` with filter message: severity, status, service IDs, on-call user, date range, tags, root cause category, detected-by; sort options; quick-view presets
- `internal/api/grpc/search.go`: handler
- PostgreSQL queries using `tsvector @@ to_tsquery` for full-text; parameterized filter clauses
- Quick views implemented as pre-built filter presets: `open`, `my_sevs`, `awaiting_postmortem`
- `demo/M08-search.md`

**Dependencies**: M01, M02

**Tests**:
- Full-text search query builder unit tests (input string → expected tsquery)
- Filter combination unit tests (all filter fields independently and in combination)
- Quick view preset unit tests
- Integration test: seed several SEVs with varied attributes → run searches → verify result sets

---

### M09 — WebSocket / Real-time

**Goal**: Authenticated WebSocket endpoint. Clients subscribe to SEV IDs and receive typed push events on any mutation.

**Deliverables**:
- `internal/api/ws/` package: hub, room-per-SEV, subscribe/unsubscribe, broadcast
- WebSocket endpoint `/ws` added to the HTTP mux (after JWT validation)
- All existing mutation handlers (SEV, announcement, chat, role, task, postmortem) publish events to the hub after a successful write
- Event envelope: `{ "type": "sev.updated", "sev_id": "...", "payload": {...} }`
- Event types: `sev.updated`, `sev.status_changed`, `announcement.created`, `chat.created`, `role.changed`, `task.linked`, `task.updated`, `postmortem.updated`
- `demo/M09-websocket.md`

**Dependencies**: M02

**Tests**:
- Hub unit tests: subscribe, broadcast to room, unsubscribe, multiple rooms
- Event fan-out unit test: single mutation → correct clients receive event, others do not
- Integration test: open two WS connections subscribed to the same SEV → trigger mutation → both receive event; connection subscribed to different SEV receives nothing

---

### M10 — Configuration API

**Goal**: Full admin configuration API: service registry, user management, on-call config, integration credentials (AES-256-GCM encrypted), data retention policy.

**Deliverables**:
- `proto/sevitout/v1/config.proto`: `ConfigService` with service registry CRUD, user role management, on-call rotation CRUD, integration config CRUD, retention config CRUD
- `internal/api/grpc/config.go`: handler
- `internal/store/crypto/` package: `Encrypt(plaintext, key []byte) ([]byte, error)` and `Decrypt(ciphertext, key []byte) ([]byte, error)` using AES-256-GCM; `ENCRYPTION_KEY` env var (32 bytes, base64-encoded)
- All `integration_config` credential fields encrypted before DB write, decrypted after read
- Integration health-check endpoint: `GET /admin/integrations/health` — tests connectivity for each configured integration
- `demo/M10-config-api.md`

**Dependencies**: M03

**Tests**:
- AES-256-GCM encrypt/decrypt unit tests (round-trip, tampered ciphertext rejected, wrong key rejected)
- Service registry CRUD unit tests
- On-call config unit tests (manual entry, time window precedence)
- Integration test: write encrypted credential → read back → decrypt → matches original; verify DB column is not plaintext

---

### M11 — Slack Bot

**Goal**: Fully functional Slack bot: slash commands, auto-create incident channel on SEV-1/2, announcement push, chat capture, bot notifications.

**Deliverables**:
- `cmd/slackbot/main.go`: Socket Mode event loop using `slack-go/slack`
- `internal/integrations/slack/` package: channel create, invite users, post message, fetch channel history
- Slash commands: `/sev open`, `/sev update <id>`, `/sev resolve <id>` — calls API server over gRPC
- Bot notifications: on SEV open/status change/resolve → post to configured default channel
- Auto-create incident channel on SEV-1 or SEV-2 open: channel name follows convention from `integration_config` (e.g., `#inc-{level}-{id}`); invite IC and on-call; post SEV link
- Announcement push: announcements with audience `external` or `status-page` are pushed to Slack after creation
- Chat capture: `/sev capture <id>` pulls last N messages from current channel into SEV chat log
- Slack bot authenticates to API server using a service-account JWT (issued at startup from `SLACKBOT_SERVICE_TOKEN`)
- Docker Compose: `slackbot` service added
- `demo/M11-slack-bot.md`

**Dependencies**: M09 (for event-driven notification triggers), M10 (for Slack credentials from config)

**Tests**:
- Slash command parser unit tests (valid, malformed, missing args)
- Channel name generation unit tests (all SEV levels, ID formats)
- Announcement push filter unit tests (internal audience not pushed; external/status-page pushed)
- Integration test (with Slack test workspace or mock): open SEV-1 → verify channel created, IC invited, SEV link posted

---

### M12 — AI Plugin System

**Goal**: Pluggable AI provider interface. Proactive lifecycle triggers. User-triggered actions. Plugin config via the Config API.

**Deliverables**:
- `proto/sevitout/v1/ai.proto`: `AIService` — `TriggerAction`, `StreamAction`, `ListPlugins`
- `internal/ai/` package:
  - `Provider` interface: `Summarize`, `SuggestRootCause`, `DraftPostmortem`, `SuggestTasks`, `FindSimilar`, `StreamAction`
  - `Dispatcher`: buffered channel + goroutine worker pool; receives lifecycle events, routes to configured provider, stores result in `ai_outputs`, broadcasts `ai.output` WS event
  - Built-in Anthropic provider (Claude) using HTTP API
  - HTTP provider: generic implementation calling a configured external endpoint
- Proactive triggers wired into lifecycle hooks (§M02 state machine events):
  - SEV-1/2 open → suggest IC/responders
  - Mitigated → draft mitigation summary
  - Resolved → draft postmortem skeleton
  - Postmortem In Review → suggest action items
- AI outputs stored in `ai_outputs`; marked AI-generated in all responses
- Per-SEV AI disable flag honored before dispatching
- Rate limiting: per-plugin requests/minute enforced in dispatcher
- `demo/M12-ai-plugin.md`

**Dependencies**: M05 (postmortem), M10 (plugin config and encrypted API keys)

**Tests**:
- Provider interface mock for all unit tests (no real API calls)
- Dispatcher unit tests: lifecycle event → correct provider method called, result stored, WS event emitted
- Rate limiter unit tests: burst allowed, limit enforced
- Per-SEV disable flag unit test
- Built-in Anthropic provider unit test with HTTP mock
- Integration test: configure mock HTTP provider → resolve SEV → verify `ai_outputs` row written with postmortem draft content

---

### M13 — Reporting, Analytics & Public Shareable Links

**Goal**: Dashboard metrics API. Trend/recurrence detection. CSV export. Overdue task surfacing. Public read-only shareable links for SEVs.

**Deliverables**:
- `proto/sevitout/v1/report.proto`: `ReportService` — `GetDashboardMetrics`, `GetSEVTrends`, `ExportSEVs`
- `proto/sevitout/v1/share.proto`: `ShareService` — `CreateShareLink`, `RevokeShareLink`, `GetSharedSEV`
- Dashboard metrics: active SEV count by level, MTTR trend (7/30/90 day), SEV frequency by service and level, postmortem completion rate, overdue task count
- Trend detection query: SEVs sharing same service + root cause category → flagged as recurring; new SEV auto-linked on create
- CSV export: filtered SEV list to downloadable CSV via REST (`GET /v1/sevs/export.csv`)
- Shareable links: signed token (HMAC-SHA256 of SEV ID + expiry, using `JWT_SECRET`); stored in `shareable_links`; `GET /s/:token` returns curated public view (title, severity, status, timestamps, external announcements, business impact only); sensitive SEVs blocked from link generation
- `demo/M13-reporting-sharing.md`

**Dependencies**: M04, M05, M06, M07

**Tests**:
- Dashboard metric calculation unit tests (fixed seed data → expected values)
- Trend detection unit tests (same service+category → recurring flag; different → not flagged)
- CSV export format unit test (headers, row values, encoding)
- Shareable link token sign/verify unit tests (valid, expired, tampered)
- Sensitive SEV block unit test
- Public view field filter unit test (internal fields not present in response)
- Integration test: create two SEVs with same service and root cause → verify recurring link; generate share link → fetch without auth → verify response contains only public fields

---

### M14 — React Frontend

**Goal**: Full web application consuming all backend APIs. All routes implemented. Real-time updates via WebSocket.

**Sub-milestones** (implement in order; each independently mergeable):

#### M14a — Shell, Auth & Dashboard
- Vite + React 18 + TypeScript + Tailwind + shadcn/ui setup
- Login page (Google/GitHub OAuth redirect)
- Auth state (JWT from `httpOnly` cookie, `WhoAmI` call on load)
- Dashboard page (`/`): active SEVs list, MTTR trend chart, overdue task count
- Shared layout: nav, breadcrumbs, user menu

#### M14b — SEV List & SEV Detail
- SEV list page (`/sevs`): search bar, filter sidebar, sort controls, quick-view tabs
- SEV create page (`/sevs/new`): form with all required fields
- SEV detail page (`/sevs/:id`): all SEV sections rendered read-only with edit-in-place for open SEVs
  - Lifecycle timestamps and derived metrics
  - Roles panel
  - Announcements feed
  - Chat log
  - Linked tasks (with overdue indicator)
  - Linked SEVs
  - SLIs
- WebSocket client initialized; subscribed to current SEV; updates applied on events

#### M14c — Postmortem Editor
- Postmortem page (`/sevs/:id/postmortem`): TipTap editor with Markdown extension
- Status workflow controls (Draft → In Review → Approved)
- Locked state: read-only with "Unlock" button → unlock reason modal → edit mode → auto-lock on save
- AI draft suggestion rendered inline (clearly marked AI-generated) with "Apply" action

#### M14d — Admin Pages
- Admin layout (`/admin`)
- Service registry (`/admin/services`): CRUD table
- User management (`/admin/users`): role assignment, deactivate
- On-call config (`/admin/oncall`): rotation CRUD, manual override
- Integration settings (`/admin/integrations`): per-integration credential form, health status badge
- AI plugin config (`/admin/ai`): plugin register/enable/disable, provider/model/key form
- Data retention (`/admin/retention`): per-severity-level retention fields

#### M14e — Public Share View & Reporting
- Shareable link view (`/s/:token`): public read-only SEV summary, no auth required
- Reporting page: trend charts, service heatmap, postmortem completion rate
- CSV export button on SEV list

**Dependencies**: All backend milestones (M02–M13)

**Tests** (frontend):
- Component unit tests with Vitest + React Testing Library for each page
- API client mock tests (all REST calls mocked)
- WebSocket client unit tests (event → query invalidation)

**Demo**: `demo/M14-frontend.md`

---

## Environment Variables Reference

| Variable | Used in | Description |
|---|---|---|
| `DATABASE_URL` | api, migrate | PostgreSQL connection string |
| `JWT_SECRET` | api | JWT signing key (min 32 chars) |
| `JWT_TTL_HOURS` | api | Token lifetime (default: 24) |
| `ENCRYPTION_KEY` | api | AES-256 key, base64-encoded 32 bytes |
| `GOOGLE_CLIENT_ID` | api | OAuth Google client ID |
| `GOOGLE_CLIENT_SECRET` | api | OAuth Google client secret |
| `GITHUB_CLIENT_ID` | api | OAuth GitHub client ID |
| `GITHUB_CLIENT_SECRET` | api | OAuth GitHub client secret |
| `PAGERDUTY_API_KEY` | api | PagerDuty REST API key (optional) |
| `GITHUB_TOKEN` | api | GitHub PAT for Issues API (optional) |
| `SLACK_APP_TOKEN` | slackbot | Slack Socket Mode app token |
| `SLACK_BOT_TOKEN` | slackbot | Slack bot OAuth token |
| `API_GRPC_ADDR` | slackbot | gRPC address of api service (e.g., `api:8080`) |
| `SLACKBOT_SERVICE_TOKEN` | slackbot | Pre-issued JWT for bot→api auth |
| `VITE_API_BASE_URL` | web | Base URL for REST API calls |

---

## Demo Directory Structure

```
demo/
├── M00-foundation.md
├── M01-database.md
├── M02-sev-api.md
├── M03-auth.md
├── M04-roles-oncall.md
├── M05-postmortem.md
├── M06-announcements-chat-links.md
├── M07-linked-tasks.md
├── M08-search.md
├── M09-websocket.md
├── M10-config-api.md
├── M11-slack-bot.md
├── M12-ai-plugin.md
├── M13-reporting-sharing.md
└── M14-frontend.md
```

Each demo file follows the structure:
1. **What was built** — one paragraph summary
2. **Prerequisites** — which prior milestones must be complete; required env vars
3. **Start the stack** — `make up` commands
4. **Walkthrough** — numbered steps with exact commands (curl, wscat, browser URLs) and expected output
5. **Verify tests pass** — `go test ./...` scope and `golangci-lint run` command
6. **Known limitations** — what is deferred to a later milestone

---

## Milestone Summary Table

| Milestone | Name | Depends On |
|---|---|---|
| M00 | Project Foundation | — |
| M01 | Database Schema & Store Interfaces | M00 |
| M02 | SEV Core API (Unauthenticated) | M01 |
| M03 | Authentication & Authorization | M02 |
| M04 | SEV Roles, People & PagerDuty | M03 |
| M05 | Postmortem | M04 |
| M06 | Announcements, Chat Log & Linked SEVs | M03 |
| M07 | Linked Tasks & GitHub Issues | M03 |
| M08 | Search & Filtering | M01, M02 |
| M09 | WebSocket / Real-time | M02 |
| M10 | Configuration API | M03 |
| M11 | Slack Bot | M09, M10 |
| M12 | AI Plugin System | M05, M10 |
| M13 | Reporting, Analytics & Public Links | M04, M05, M06, M07 |
| M14 | React Frontend | M02–M13 |
