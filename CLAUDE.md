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
