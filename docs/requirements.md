# Sevitout — Functional Requirements

**Version**: 0.1 (draft)
**Purpose**: Capture functional requirements for the Sevitout SEV (Severity Event) management system. This document drives the system architecture and project plan.

---

## 1. Overview

Sevitout is an internal severity event (SEV) management platform for a single organization. It provides a centralized record system for opening, tracking, and learning from incidents. The system surfaces the right information to the right people during and after an incident, and integrates with tools already in use (Slack, PagerDuty, Jira/GitHub/Linear, and monitoring platforms).

### Primary Interfaces
- **Web application** — full incident dashboard, editing, search, reporting
- **Slack bot** — open/update/close SEVs, post announcements, and capture chat during active incidents

---

## 2. SEV Lifecycle

### 2.1 Lifecycle Timestamps

Every SEV must track the following moments (all in UTC):

| Timestamp | Description |
|---|---|
| `started_at` | When the impact actually began (may be estimated) |
| `detected_at` | When the team first became aware of the issue |
| `mitigated_at` | When impact was reduced (root cause may still be open) |
| `resolved_at` | Incident fully closed |
| `postmortem_completed_at` | Postmortem approved and finalized |

### 2.2 Derived Metrics (auto-computed)

| Metric | Formula |
|---|---|
| **MTTD** — Mean Time to Detect | `detected_at − started_at` |
| **MTTM** — Mean Time to Mitigate | `mitigated_at − started_at` |
| **MTTR** — Mean Time to Resolve | `resolved_at − started_at` |
| **DTTM** — Detection to Mitigation | `mitigated_at − detected_at` |

Metrics are stored on the SEV record after each timestamp is set.

### 2.3 SEV Status

```
Open → Investigating → Mitigated → Resolved → Postmortem In Progress → Postmortem Complete
```

Status transitions must be recorded with the user and timestamp.

---

## 3. Severity Levels

SEVs use the SEV-1 through SEV-4 taxonomy:

| Level | Name | Description |
|---|---|---|
| **SEV-1** | Critical | Total outage or data loss; immediate all-hands response required |
| **SEV-2** | Major | Significant degradation; substantial customer/business impact |
| **SEV-3** | Minor | Partial degradation; limited or isolated customer impact |
| **SEV-4** | Low | Minor issue; no or negligible customer impact |

---

## 4. SEV Record

### 4.1 Core Identity

- Unique ID (system-generated, human-readable: e.g., `SEV-2026-0042`)
- Title / short description
- Detailed description / summary
- Severity level (§3)
- Status (§2.3)
- Created at / updated at / created by

### 4.2 Incident Details

- **Root cause**: root cause category (e.g., deployment, configuration, hardware, dependency) + free-form description
- **Mitigation**: what actions stopped or reduced the impact
- **Prevention / Action Items**: what will be done to prevent recurrence (narrative, distinct from linked tasks)
- **Business impact**: customer-facing description; quantified impact where possible (e.g., % error rate, requests affected, revenue, SLA breach)
- **Affected services**: list of impacted services/systems (linked to a service registry or free-form)
- **Detection method**: how the issue was discovered — alert, monitoring dashboard, customer report, synthetic test, manual discovery, Slack escalation
- **Alert name / monitoring tool**: the specific alert or tool that fired (Datadog, Prometheus, CloudWatch, PagerDuty, etc.)
- **Were the right people in the room?**: boolean flag + notes
- **Tags / labels**: arbitrary key-value tags for custom filtering

### 4.3 SLIs & SLOs

- List of impacted Service Level Indicators per affected service:
  - SLI name
  - SLO threshold
  - Measured value during the incident (how bad was it?)
  - Time period of the SLO violation
- Links to relevant dashboards and monitoring queries

---

## 5. People & Roles

A SEV records the following roles. All roles may reference a system user or free-form text (for external parties, teams, or automated systems). Multiple people may share a role.

| Role | Description |
|---|---|
| **On-call** | Person/team on rotation when the SEV occurred; auto-populated from PagerDuty integration |
| **Detected by** | Person or system (alert name) that first identified the issue |
| **Incident Commander (IC)** | Person leading the response and coordinating action |
| **Communications Lead** | Manages stakeholder and customer-facing communication |
| **Recorder / Scribe** | Captures real-time notes and timeline during the incident |
| **Responders** | All other active participants |

---

## 6. Announcements & Updates

- Ordered list of status updates published during and after the SEV
- Each update stores: timestamp, author, message body, audience (`internal` | `external` | `status-page`)
- Updates may be tagged as lifecycle milestones (e.g., "SEV opened", "mitigation complete", "resolved")
- Templated update messages (pre-filled based on status transition)
- Slack integration: push announcements to configured channels automatically

---

## 7. Chat & Communication Log

- Capture relevant chat excerpts from incident response channels during a SEV
- Each entry: timestamp, source (Slack, Teams, email, etc.), author, message content
- Entries may be added manually (copy/paste) or via Slack integration (capture from incident channel)
- Searchable within a SEV record

---

## 8. Linked Tasks

Link external task/ticket references to a SEV for tracking action items and follow-up:

- **Supported systems**: Jira, GitHub Issues, Linear (plus generic URL)
- Each link stores: task ID, URL, title/description, external system, relationship type
- Relationship types: `action-item` | `contributing-factor` | `follow-up` | `dependency`
- Priority flag: `critical` (required-to-close) vs. `non-critical` (nice-to-have)
- **SLA / due date**: tasks carry a due date, defaulted by priority at time of linking:
  - `critical` — 30 days from SEV resolved date
  - `non-critical` — 90 days from SEV resolved date
  - Due date may be overridden manually
- Overdue tasks surface in the SEV record and in the reporting dashboard
- Future: poll external system for live task status

---

## 9. Linked SEVs

Relate a SEV to other SEVs with a typed relationship:

| Type | Description |
|---|---|
| **Related** | Loosely related incidents with shared context |
| **Caused by** | This SEV was triggered by an upstream SEV |
| **Duplicate** | Same root issue, separate ticket |
| **Recurrence of** | Same failure pattern seen in a prior SEV |

Links are bidirectional — linking A→B is reflected on B's record.

---

## 10. Postmortem

A postmortem is **required for every SEV**, regardless of severity level.

- Structured document attached to the SEV with standard sections:
  - Summary
  - Customer impact
  - Timeline (auto-populated from lifecycle timestamps and updates)
  - Root cause
  - Contributing factors
  - Lessons learned
  - Action items
- Separate status: `Draft` → `In Review` → `Approved / Complete`
- Supports rich text editing in the web UI
- Lock/finalize on approval (no further edits without elevated permission)
- Blameless framing enforced by convention (configurable guideline displayed during editing)

### 10.1 Post-Postmortem Lock

- Once a SEV reaches **Postmortem Complete** status, the entire SEV record (including the postmortem document) becomes **read-only**
- To make any edit, the user must provide a written reason before the record is unlocked for editing
- The unlock reason, the user, and the timestamp are recorded in the audit log
- The record returns to read-only automatically after the editing session is saved
- Only Incident Commander and Admin roles may unlock a completed SEV

---

## 11. SEV AI Agent (Plugin)

The SEV AI agent is a pluggable skill that assists during and after incidents. Behavior is **proactive on key lifecycle events** — automatically triggered at status transitions, not only when manually invoked.

### 11.1 Proactive Triggers

| Event | AI Action |
|---|---|
| SEV opened (SEV-1 or SEV-2) | Suggest IC, responders, and relevant runbooks based on affected services |
| Mitigated | Draft mitigation summary and suggest root cause categories |
| Resolved | Auto-draft postmortem skeleton from SEV data |
| Postmortem In Review | Suggest action items based on root cause analysis |

### 11.2 User-Triggered Actions

- **Summarize** — concise narrative from the SEV data
- **Find similar SEVs** — semantic search of historical incidents
- **Draft announcement** — generate a stakeholder update message
- **Suggest prevention tasks** — generate action items from root cause

### 11.3 Plugin Configuration

- Plugin registration: name, version, description, handler type (HTTP endpoint or built-in)
- Per-organization default: enabled/disabled
- Per-SEV override: enable/disable AI for a specific incident (for sensitive SEVs)
- AI provider configuration: provider (Anthropic, OpenAI, etc.), model, API key (stored encrypted)
- Rate limiting and quota controls per org
- AI outputs are clearly marked as AI-generated and non-authoritative
- Streaming response support for long-running AI actions

---

## 12. Search & Filtering

SEVs must be searchable and filterable across all records:

- **Full-text search**: title, description, root cause, announcements
- **Filters**: severity level, status, affected service(s), on-call person/team, date range, tags, root cause category, detected-by
- **Quick views**: open SEVs, my SEVs, SEVs awaiting postmortem
- **Sorting**: by start time, severity, MTTR, last updated
- Future: AI-assisted semantic search via AI plugin

---

## 13. Integrations (v1)

### 13.1 Slack

- **Slash command**: `/sev open`, `/sev update`, `/sev resolve` — create and update SEVs from Slack
- **Bot notifications**: post to configured channels when a SEV is opened, updated, or resolved
- **Announcement push**: send announcements from Sevitout to a Slack channel
- **Chat capture**: pull messages from an incident Slack channel into the SEV chat log
- **In-channel commands**: respond to `@sevbot status`, `@sevbot timeline`, etc.
- **Auto-create incident channel**: when any SEV is opened (any severity, any origin — Slack, web UI, API, or an integration like PagerDuty), the bot automatically creates a dedicated Slack channel (e.g., `#inc-2026-0042-database-outage`), invites the IC, on-call, and (if opened via `/sev open`) the person who opened it, and posts the SEV link; all subsequent notifications and pushed announcements for that SEV route to this channel rather than the default one, keeping unrelated SEVs' discussions from mixing in a shared channel. Channel name convention is configurable in §18.4. Sensitive SEVs are excluded — no incident channel is auto-created for them, consistent with their field-level visibility restrictions.

### 13.2 PagerDuty

- On SEV creation, automatically look up the current on-call person for the affected service and populate the on-call field
- Link to the PagerDuty incident if one exists
- Sevitout reads from PagerDuty only; triggering pages remains a manual action outside the system

### 13.3 Task Trackers

**v1 — GitHub Issues only:**
- Link existing GitHub Issues to a SEV (paste URL or search by repo + issue number)
- Create a new GitHub Issue from within Sevitout (pre-filled with SEV context)
- Display issue title and current status (open/closed) inline on the SEV record

**v2 (fast follow) — Jira, Linear:**
- Same link/create/display capabilities extended to Jira and Linear

### 13.4 Monitoring (Datadog / Prometheus / CloudWatch)

- Link a dashboard URL or saved query to a SEV
- Store alert name and monitoring tool as detection metadata
- Future: embed chart snapshots from monitoring tools into the SEV record

---

## 14. Authentication & Authorization

- **Authentication**: Email and password. Passwords are stored as bcrypt hashes — no external OAuth provider required.
- **Registration**: Open self-registration. The first user to register is automatically granted the Admin role for bootstrapping; all subsequent users receive the Viewer role by default and must be promoted by an Admin.
- **Authorization roles**:

| Role | Capabilities |
|---|---|
| **Viewer** | Read all SEV data |
| **Responder** | Create SEVs, add updates, link tasks, capture chat |
| **Incident Commander** | All responder actions + manage roles, transition status |
| **Admin** | Full access: configure integrations, AI plugins, manage users |

- Sensitive SEVs (e.g., security incidents) may have restricted visibility — only explicitly added users can view
- All permission changes are logged

### 14.1 Public Shareable Links

- Any SEV may have a public shareable link generated on demand (opt-in, not on by default)
- The link grants read-only access to a curated view of the SEV — no login required
- Shareable view exposes: title, severity, status, lifecycle timestamps, announcements marked `external`, and business impact; internal-only fields (chat log, audit log, sensitive notes) are hidden
- Links are revocable by any IC or Admin on the SEV
- Sensitive SEVs cannot have shareable links generated

---

## 15. Audit Log

- Immutable, append-only log of all changes to every SEV record
- Each entry: timestamp, user, action (field changed, value before/after)
- Accessible to Admins and the IC for a given SEV
- Critical for postmortem accuracy, compliance, and dispute resolution
- Immutability enforced at both the database level (INSERT-only DB role) and the application level (append-only store interface — no update or delete path exists)

---

## 16. Notifications & Alerting

- Configurable notification channels: Slack, email
- Notification triggers: SEV opened, status change, new announcement, postmortem due, postmortem approved
- Role-based routing: IC notified of all changes; management notified of SEV-1/SEV-2 opens only
- Escalation: alert if a SEV-1 has been open for > N minutes without an IC assigned (configurable threshold)

---

## 17. Reporting & Analytics

- **Dashboard**: active SEV count, MTTR trend, SEV frequency by service and level, postmortem completion rate
- **Trend detection**: highlight services or failure patterns that appear across multiple SEVs
- **Export**: CSV export of SEV records for a date range
- **Recurring incident flag**: automatically link a new SEV to prior SEVs with same service + root cause category

---

## 18. Configuration API & Admin

All configuration is managed through a dedicated Configuration API and a corresponding admin UI page. Admins are the only role with write access to configuration resources.

### 18.1 Service Registry

Sevitout maintains its own lightweight service registry:

- **Service record**: name, description, owning team, PagerDuty service ID (for on-call lookup), tags
- CRUD operations via API and admin UI
- Services are referenced by name/ID throughout the system (affected services, SLI records, PagerDuty lookup)
- Deactivating a service removes it from active selection but preserves historical SEV references

### 18.2 User Management

- View all registered users
- Assign and revoke organization roles (Viewer, Responder, Incident Commander, Admin)
- Deactivate a user (revokes access without deleting historical attribution)
- User directory is searchable by name and email

### 18.3 On-Call Configuration

- Define on-call rotations as named entries: rotation name, linked service(s), PagerDuty schedule ID
- On-call records can be managed manually (override who is on-call for a service) or synced from PagerDuty
- Manual on-call entries support a time window (start/end) for planned overrides
- On-call history is preserved so SEV records can always show who was on-call at the time of the incident

### 18.4 Integration Configuration

- Per-integration settings managed via admin UI and API:
  - **Slack**: workspace token, default notification channel, incident channel naming convention
  - **PagerDuty**: API key, default escalation policy
  - **Task trackers**: per-system credentials and default project/board
  - **Monitoring**: tool type and base URL for dashboard link generation
- Integration health status visible on the admin page (connected / error / not configured)

### 18.5 Notification & Escalation Settings

- Configure escalation thresholds (e.g., SEV-1 without IC after N minutes)
- Configure which roles receive which notification events
- Manage notification channel defaults (Slack channel, email list)

### 18.6 AI Plugin Configuration

- Register, enable, and disable AI plugins
- Configure provider, model, and encrypted API key per plugin
- Set rate limits and per-severity-level defaults for proactive triggers

### 18.7 Data Retention

- **Default**: SEVs are retained forever — no automatic deletion unless configured
- Retention policy is configurable **per severity level**:

| Level | Default Retention |
|---|---|
| SEV-1 | Forever |
| SEV-2 | Forever |
| SEV-3 | Forever |
| SEV-4 | Forever |

- Admins may set a custom retention period (in days) for any severity level; `0` means retain forever
- On expiry: SEVs are archived (soft-deleted, not purged) by default; hard-delete requires explicit configuration
- Archived SEVs are excluded from search and dashboards but remain accessible to Admins
- Export is available before archival for compliance purposes

---

## 19. Non-Functional Requirements

| Requirement | Target |
|---|---|
| **Availability** | The SEV system must stay up during an active incident (high-availability deployment) |
| **API-first** | All features accessible via REST API; web UI and Slack bot are consumers |
| **Latency** | Sub-200ms read latency for open SEV views |
| **Single organization** | Single-tenant data model (multi-tenancy is out of scope for v1) |
| **Auditability** | All mutations logged with user and timestamp |
| **Security** | OAuth tokens, AI API keys, and integration credentials encrypted at rest using AES-256-GCM at the application layer; database never holds plaintext |
| **Extensibility** | Plugin architecture for AI agents and future integrations |

---

## 20. Out of Scope (v1)

- Automated incident detection / alert origination (Sevitout does not replace Datadog/PagerDuty)
- On-call scheduling and rotation management
- Customer-facing status page hosting
- Full monitoring/observability platform
- Multi-tenant / multi-organization support
- Mobile-native application (mobile browser is acceptable)

---

## 21. Open Questions

*To be resolved before the architecture phase:*

1. ~~What does the service registry look like — is there an existing list of services, or do we build a lightweight registry into Sevitout?~~
   - **Answered**: Sevitout will maintain its own lightweight service registry, managed via a Configuration API and admin UI page. See §18.
2. ~~For task trackers (Jira/GitHub/Linear) — do we need all three at launch, or prioritize one?~~
   - **Answered**: GitHub Issues is the only task tracker integration for v1. Jira and Linear are fast-follow / v2. See §8 and §13.3.
3. ~~Should the Slack incident channel be created automatically by the bot when a SEV-1/SEV-2 is opened?~~
   - **Answered**: Yes — and extended beyond the original SEV-1/2 scope: the Slack bot automatically creates a dedicated incident channel for *every* opened SEV, regardless of severity, so unrelated SEVs never share one channel's discussion. See §13.1.
4. ~~Are there data retention requirements — how long should resolved SEVs be kept?~~
   - **Answered**: Default is retain forever; configurable per severity level with different retention periods per SEV level. See §18.7.
5. ~~Should opening a SEV-1/SEV-2 automatically trigger a PagerDuty page, or always remain a manual action?~~
   - **Answered**: Always manual. Sevitout reads from PagerDuty (on-call lookup) but never triggers pages. See §13.2.
6. ~~Is there a need for a read-only external shareable link for a SEV (viewable without login)?~~
   - **Answered**: Yes — SEVs support a public shareable link. See §14.
