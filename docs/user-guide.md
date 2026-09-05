# Sevitout — User Guide

This guide is for people **using** Sevitout day to day: opening and running a
SEV, writing a postmortem, and — for Admins — wiring up Slack, PagerDuty,
GitHub, Jira, and AI plugins. It describes the web app and the Slack bot.

If you're integrating against the API directly, start with
[`docs/architecture.md`](architecture.md) and the per-milestone runbooks in
[`demo/`](../demo/), which give exact `curl` examples for every endpoint.

---

## 1. What is a SEV

A SEV (Severity Event) is Sevitout's central record for an incident. Every
SEV has a severity level:

| Level | Name | Description |
|---|---|---|
| **SEV-1** | Critical | Total outage or data loss; immediate all-hands response required |
| **SEV-2** | Major | Significant degradation; substantial customer/business impact |
| **SEV-3** | Minor | Partial degradation; limited or isolated customer impact |
| **SEV-4** | Low | Minor issue; no or negligible customer impact |

A **postmortem is required for every SEV**, regardless of severity — see
[§11](#11-postmortems).

---

## 2. Creating a SEV

### Via the web app

**SEV List** → **New SEV** opens the create form, with three sections:

- **Details** — title (required), severity level, description, and affected
  services (pick from the service registry, or type a new one).
- **Detection** — *Started at* / *Detected at* timestamps, detection method,
  monitoring tool (a closed list: Datadog, Prometheus, CloudWatch, or Other),
  alert name, and optional links (alert link, dashboard link, a saved
  query/expression, snapshot image URL).
- **Tags** — arbitrary key/value pairs for later filtering.

Two checkboxes at the bottom matter for how the SEV is handled downstream:

- **Sensitive** — restricts visibility and excludes the SEV from search,
  reports, and AI dispatch (see [§14.5](#145-ai-plugins) and
  [§13](#13-reporting-dashboards-public-share-links)). Use this for
  security incidents or anything with restricted-visibility requirements.
- **Disable AI plugin dispatch for this SEV** — opts this one SEV out of AI
  suggestions even if AI plugins are otherwise enabled organization-wide.

### Via Slack

```
/sev open [severity 1-4] <title>
```

Severity defaults to `3` if omitted. The bot replies with the new SEV's ID.

### Via the API

See [`demo/M02-sev-api.md`](../demo/M02-sev-api.md) for `POST /v1/sevs` and
the full CRUD surface.

### What happens automatically on create

- If the SEV's primary affected service has a PagerDuty service ID
  configured in the [service registry](#151-service-registry), the current
  on-call engineer is auto-assigned the **on-call** role.
- The Slack bot creates a dedicated incident channel for the SEV,
  **regardless of severity** — see [§14.1](#141-slack).
- SEV-1 and SEV-2 SEVs automatically get an AI-suggested list of responders
  (if an AI plugin is enabled) — see [§14.5](#145-ai-plugins).
- If the SEV's affected service + root cause category matches a prior SEV,
  it's auto-linked to it as `recurrence-of` (only once the root cause
  category is set — see [§13](#13-reporting-dashboards-public-share-links)).

---

## 3. SEV lifecycle & statuses

A SEV moves through these statuses:

```
        ┌────────────────────────────┐
        ▼                            │
      Open ──────► Investigating ────┤
        ▲                │           │
        │                ▼           │
        │            Mitigated ◄─────┘
        │                │
        │                ▼
        │            Resolved
        │                │
        │                ▼
        │   Postmortem In Progress
        │                │
        │                ▼
        └──── Postmortem Complete
```

The exact legal moves (enforced server-side — an invalid request is
rejected before anything is written):

| From | Can move to |
|---|---|
| Open | Investigating, Mitigated |
| Investigating | Mitigated, **Open** (re-open) |
| Mitigated | Resolved, **Investigating** (step back) |
| Resolved | Postmortem In Progress |
| Postmortem In Progress | Postmortem Complete |
| Postmortem Complete | **Open** (re-open after postmortem) |

Notice `Resolved` is only reachable from `Mitigated` — you can't jump
straight from `Open` to `Resolved`.

**Via the web app**: the SEV detail page shows the current status and the
valid next-status buttons only — you can't request an invalid transition
from the UI.

**Via Slack**:

```
/sev transition <sev-id> <status>
/sev resolve <sev-id>          # shorthand for "transition ... resolved"
```

Valid status values: `open`, `investigating`, `mitigated`, `resolved`,
`postmortem_in_progress`, `postmortem_complete`.

Every transition is recorded with the user and timestamp in
`sev_status_history` and is visible in the SEV's audit log.

---

## 4. Understanding the metrics: MTTD, MTTM, MTTR, DTTM

Sevitout tracks five lifecycle timestamps (all UTC) and derives four metrics
from them automatically — you never enter a metric directly:

| Timestamp | Description |
|---|---|
| `started_at` | When impact actually began (may be estimated) |
| `detected_at` | When the team first became aware |
| `mitigated_at` | When impact was reduced (root cause may still be open) |
| `resolved_at` | Incident fully closed |
| `postmortem_completed_at` | Postmortem approved and finalized |

| Metric | Meaning | Formula |
|---|---|---|
| **MTTD** | Mean Time to Detect | `detected_at − started_at` |
| **MTTM** | Mean Time to Mitigate | `mitigated_at − started_at` |
| **MTTR** | Mean Time to Resolve | `resolved_at − started_at` |
| **DTTM** | Detection to Mitigation | `mitigated_at − detected_at` |

**A metric only appears once both of its inputs are set.** An open SEV with
no `mitigated_at` yet will show a blank MTTM and DTTM — that's expected, not
a bug. As soon as you set the corresponding timestamp (usually by
transitioning status, which prompts for the timestamp), the metric appears.

Where to see them:

- The SEV detail page's lifecycle panel, for a single SEV.
- The **Reports** page's dashboard (MTTR trend over 7/30/90-day windows,
  frequency by service and severity level) — see [§13](#13-reporting-dashboards-public-share-links).
- CSV export.

---

## 5. Roles on a SEV

| Role | Description |
|---|---|
| **On-call** | Person/team on rotation when the SEV occurred; auto-populated from PagerDuty if configured |
| **Detected by** | Person or system (e.g. an alert name) that first identified the issue |
| **Incident Commander (IC)** | Leads the response and coordinates action |
| **Communications Lead** | Manages stakeholder and customer-facing communication |
| **Recorder / Scribe** | Captures real-time notes and timeline |
| **Responders** | All other active participants |

Multiple people can hold the same role. Assign/remove roles from the SEV
detail page's Roles panel, or via `RoleService` directly (see
[`demo/M04-roles-oncall.md`](../demo/M04-roles-oncall.md)).

Assigning an Incident Commander matters beyond coordination: **IC (or
Admin) is required to unlock a completed SEV or generate a public share
link** — see [§11.1](#111-post-postmortem-lock) and
[§13](#13-reporting-dashboards-public-share-links).

---

## 6. Organization roles (RBAC)

Separate from roles *on a SEV*, every user has one organization-wide role:

| Role | Capabilities |
|---|---|
| **Viewer** | Read all SEV data |
| **Responder** | Everything Viewer can, plus: create SEVs, add updates, link tasks, capture chat |
| **Incident Commander** | Everything Responder can, plus: manage SEV roles, transition status |
| **Admin** | Full access: configure integrations, AI plugins, manage users |

**Registration is open** — anyone can register with an email/password.
**The first person to ever register becomes Admin automatically**, which
bootstraps the system; everyone after that starts as Viewer and must be
promoted by an Admin from **Admin → Users**. There is no OAuth login —
authentication is email + bcrypt-hashed password.

---

## 7. Announcements & updates

Ordered status updates on a SEV, each tagged with an audience:

| Audience | Meaning |
|---|---|
| `internal` | Visible only inside Sevitout / to your team |
| `external` | Customer-facing; pushed to Slack automatically |
| `status-page` | Meant for a public status page; also pushed to Slack automatically |

Announcements can be flagged as lifecycle milestones (e.g. "SEV opened",
"mitigation complete"). Post one from the SEV detail page's Announcements
feed, or from Slack:

```
/sev update <sev-id> <message>
```

`/sev update` always posts as `internal` — it is **not** pushed to Slack
again (it would be redundant, since you're already posting from Slack).
Only `external`/`status-page` announcements created elsewhere (web UI, API)
get pushed out to the SEV's incident channel.

---

## 8. Chat & communication log

A searchable log of relevant chat excerpts from incident response. Add
entries manually (copy/paste) from the SEV detail page, or capture them from
the current Slack channel:

```
/sev capture <sev-id> [limit]
```

Pulls the last `limit` messages (default 20) from whichever channel you run
the command in, oldest first. Run it from the SEV's own incident channel —
the bot doesn't verify the channel matches the SEV, so running it elsewhere
captures the wrong conversation.

---

## 9. Linked tasks (GitHub & Jira Issues) & SLA due dates

Link action items and follow-up work to a SEV. **GitHub Issues and Jira
Issues are both supported** (Linear is still planned as a fast-follow). Each
linked task shows a colored badge for its tracker (GitHub, Jira, or a plain
manually-linked URL) so the three are easy to tell apart at a glance. Each
link has a relationship type (`action-item`, `contributing-factor`,
`follow-up`, `dependency`) and a priority:

| Priority | SLA due date |
|---|---|
| `critical` | SEV's `resolved_at` + 30 days |
| `non-critical` | SEV's `resolved_at` + 90 days |

The due date is calculated once the SEV resolves (if you link a task before
resolution, the due date fills in automatically once `resolved_at` is set).
You can always override the due date manually. A task is **overdue** once
its due date has passed — overdue tasks surface on the SEV record and on
the Reports dashboard's overdue count.

From the SEV detail page you can **link an existing** issue (paste a URL, or
for GitHub search by repo + issue number) or **create a new one** — a
separate form per tracker — pre-filled with SEV context; the created issue
is automatically labeled with the SEV ID and its priority. Creating/linking
a GitHub Issue requires GitHub credentials configured (a PAT with `repo`
scope — see [§14.3](#143-github)); creating/linking a Jira Issue requires
Jira credentials (see [§14.4](#144-jira)). Without either configured, that
tracker's actions are unavailable, but everything else about linked tasks —
including the other tracker — still works.

---

## 10. Linked SEVs

Relate a SEV to others with a typed relationship:

| Type | Meaning |
|---|---|
| **Related** | Loosely related, shared context |
| **Caused by** | Triggered by an upstream SEV |
| **Duplicate** | Same root issue, separate ticket |
| **Recurrence of** | Same failure pattern as a prior SEV |

Links are **bidirectional** — linking A → B also shows up as B → A
automatically. `recurrence-of` links can also be created automatically (see
[§2](#2-creating-a-sev)).

---

## 11. Postmortems

Every SEV gets an empty **Draft** postmortem the moment it's created — there
is no separate "start a postmortem" step. Open it from the **Postmortem**
button on the SEV detail page.

The editor is rich text (backed by Markdown storage, so the document stays
portable and readable outside Sevitout). The first time you edit a blank
postmortem, it's pre-filled from the SEV's own recorded facts — Summary,
Lifecycle (a timestamp table with deltas), Root Cause, Business Impact,
Services Affected, and Mitigation — with explanatory placeholders (e.g.
"_Not yet determined._") anywhere data hasn't been filled in yet on the SEV
itself. Missing facts don't disappear the section — fill in the SEV's
Details panel and re-open the postmortem to see them populate.

**Status workflow**: `Draft → In Review → Approved`. Transitioning requires
Incident Commander or Admin.

An **AI draft suggestion** panel shows the most recent AI-generated
postmortem draft (see [§14.5](#145-ai-plugins)), always clearly marked
"AI-generated — not authoritative, review before use." You can regenerate
it or **Apply to editor** to load it in for editing — it never saves
automatically.

You can **download the document as Markdown or print/save as PDF** at any
time, whether it's saved or mid-edit.

### 11.1 Post-postmortem lock

Once a SEV reaches **Postmortem Complete**, the *entire* SEV record —
including the postmortem — becomes read-only. This is a common point of
confusion, so to be explicit:

1. The record shows a locked/read-only state; only Incident Commander or
   Admin see an **Unlock to edit** option at all (a Viewer or Responder sees
   no way to edit it).
2. Unlocking requires typing a **written reason** in a modal.
3. That reason, the user, and the timestamp are written to the SEV's audit
   log.
4. You get one edit window: the next save uses up the unlock, and any
   further edit needs a fresh reason — there's no persistent "unlocked"
   state to leave open by accident.

---

## 12. Search & filtering

The SEV List page and `SearchService` support:

- **Full-text search** across title, description, root cause description,
  business impact, and announcement text.
- **Filters**: severity level, status, affected service(s), on-call
  person, detected-by, date range, tags, root cause category — combine any
  number of these.
- **Quick views**: `open` (status is Open/Investigating/Mitigated),
  `awaiting_postmortem` (Resolved or Postmortem In Progress), `my_sevs`
  (you hold any role on it).
- **Sort**: by start time, severity, MTTR, or last updated, either
  direction. SEVs missing the sorted-on value (e.g. sorting by MTTR before a
  SEV has resolved) always sort last.

---

## 13. Reporting, dashboards & public share links

The **Dashboard** and **Reports** pages surface: active SEV count by
severity, MTTR trend (7/30/90-day windows), SEV frequency by service and
severity level, postmortem completion rate, and overdue task count.
**Recurring incidents** — SEVs sharing the same affected service and root
cause category — are automatically flagged and linked (see
[§10](#10-linked-sevs)).

**CSV export** — from the SEV List page, export the currently filtered list
of SEVs to CSV for offline analysis or compliance records.

**Public shareable links** — any non-sensitive SEV can get a public,
read-only, no-login-required link:

- Generated by an Incident Commander or Admin, opt-in only (never on by
  default).
- The shared view exposes: title, severity, status, lifecycle timestamps,
  `external`-audience announcements, and business impact. Everything else —
  root cause detail, tags, chat log, audit log, internal announcements — is
  **absent**, not just hidden.
- **Sensitive SEVs cannot get a share link** — the request is rejected
  outright.
- Revocable at any time by an IC or Admin on the SEV; a revoked or expired
  link returns "gone," not the SEV data.

---

## 14. Configuring integrations

All integration configuration lives under **Admin → Integrations** (and
**Admin → AI Plugins** for the AI system specifically). Only Admins can
configure integrations.

**Admin → Integrations** is a sidebar of the six fixed integration types
(PagerDuty, GitHub, Slack, Jira, Email, Monitoring). Each row shows whether
it's configured and a live health badge (green **Connected**, red **Error**
with the underlying error message and a short troubleshooting hint shown
right in the form, gray **Not configured**/**No health check**). Selecting a
row opens a form built from that integration's own schema: credential fields
are password-masked and labeled in plain language (e.g. "Bot Token", not
`bot_token`); leaving a credential field blank on save keeps whatever's
already stored, so rotating one secret never requires re-entering the
others. Monitoring has no credential fields at all — see
[§14.6](#146-monitoring-datadog--prometheus--cloudwatch); Email has no
environment-variable fallback at all (unlike the other five) — it's
configured exclusively through this form, see
[§14.7](#147-email-for-notifications).

**Every credential below can also be set via an environment variable
instead of (or as a fallback for) the admin form** — PagerDuty, GitHub, and
Jira all prefer whatever's configured in **Admin → Integrations** over their
env var, falling back to the env var only when nothing's saved there yet;
Slack does the same for the parts of the bot that make outbound Slack API
calls (channel creation, messages, invites), though its Socket Mode
connection (slash commands, `@mentions`) still only picks up a *rotated*
credential on restart — see [§14.1](#141-slack). A change made in the admin
form reaches a running process within a few minutes, no restart needed,
except where noted otherwise below.

### 14.1 Slack

**What it needs from you, once:**

1. **Create a Slack app** at <https://api.slack.com/apps> → **Create New
   App** → **From an app manifest**, using a manifest with Socket Mode
   enabled, the `/sev` slash command, and an `app_mention` event
   subscription. The full manifest and exact scopes are in
   [`demo/M11-slack-bot.md`](../demo/M11-slack-bot.md#configuring-slack-one-time-setup) —
   copy it as-is unless you need to customize the slash command.
2. **Generate an app-level token** (Basic Information → App-Level Tokens,
   scope `connections:write`).
3. **Install the app to your workspace** → copy the Bot User OAuth Token.
4. **Invite the bot** (`/invite @sevbot`) to whichever channel you'll run
   `/sev open` from.
5. **Give the bot a service account**: register a normal user (e.g.
   `slackbot@sevitout.local`) and promote it to Admin from **Admin →
   Users** (Admin is required for the bot to read integration config at
   startup). Put that email/password in `.env` as
   `SLACKBOT_SERVICE_EMAIL`/`SLACKBOT_SERVICE_PASSWORD` — the bot logs
   itself in and keeps its own token fresh; there's nothing to rotate by
   hand.
6. Set `API_GRPC_ADDR` (e.g. `api:8080` in Docker Compose).
7. Put the two tokens from steps 2–3 **either** in `.env` as
   `SLACK_APP_TOKEN`/`SLACK_BOT_TOKEN`, **or** under **Admin →
   Integrations → Slack** as `Bot Token`/`App Token` — the bot prefers
   whatever's saved there over the env vars. For a from-scratch setup,
   the env vars are the simpler path (there's no Slack config to save to
   yet on first boot); once running, you can switch to managing them from
   the admin form instead.

**From Admin → Integrations → Slack**, once running, you can set/rotate:

- **Bot Token** / **App Token** — the pair above. A **rotated** value here
  reaches the bot's outbound Slack API calls (channel creation, messages,
  invites, history, user lookup) within a few minutes, no restart needed —
  but slash commands and `@mentions` (Socket Mode) keep using whichever
  pair was active when the bot process last started, so rotating a token
  still needs a `slackbot` restart to take effect there.
- `default_channel` — a Slack **channel ID** (not `#name`) used only as a
  fallback before a SEV's own incident channel exists.
- `channel_naming_convention` — default `inc-{id}-{title}`.

**What happens automatically once Slack is configured:**

- Every SEV opened — any severity, any origin (Slack, web UI, API, an
  integration) — gets its own auto-created incident channel; on-call and
  (if opened via `/sev open`) the opener are invited, and the SEV link is
  posted. Sensitive SEVs never get an auto-created channel.
- SEV open / status change events post to the SEV's incident channel.
- `external`/`status-page` announcements push to Slack.

### 14.2 PagerDuty

Either set `PAGERDUTY_API_KEY` in the environment, **or** enter the API Key
under **Admin → Integrations → PagerDuty** (preferred when both are
present, and the only option that can be changed without a restart). Then,
in **Admin → Services**, set the PagerDuty service ID on each service
record you want on-call lookups for.

Once both are set, creating a SEV against that service auto-populates the
**on-call** role with the current on-call engineer. Sevitout only *reads*
from PagerDuty — it never triggers a page; paging stays a manual action
outside the system.

### 14.3 GitHub

Either set `GITHUB_TOKEN` (a Personal Access Token with `repo` scope) in
the environment, **or** enter the token under **Admin → Integrations →
GitHub** (preferred when both are present).

Without a token configured (either way), linking an *existing* issue by
URL still works, but creating a new GitHub Issue from Sevitout, and any
other GitHub-API-backed action, returns "unavailable" rather than failing
the whole task-linking feature.

### 14.4 Jira

Either set `JIRA_CLOUD_ID`/`JIRA_API_TOKEN` (and optionally `JIRA_SITE_URL`)
in the environment, **or** enter them under **Admin → Integrations →
Jira** (preferred when both are present):

- **API Token** — a Jira Cloud API token, sent as a Bearer token.
- **Cloud ID** (required) — the Jira Cloud tenant's Cloud ID, a UUID — find
  it under `admin.atlassian.com`, **not** your site's `https://*.atlassian.net`
  name.
- **Site URL** (optional) — e.g. `https://acme.atlassian.net`. Purely
  cosmetic: used to build a clickable `.../browse/{key}` link on created/
  linked issues. Left unset, issue links fall back to the API's own
  non-browsable resource URL.

Without both the token and Cloud ID configured, linking an *existing* Jira
issue by URL still works, but creating a new Jira Issue, and any other
Jira-API-backed action, returns "unavailable" — the rest of linked tasks
(including GitHub) is unaffected.

### 14.5 AI plugins

From **Admin → AI Plugins**, register a plugin:

- **Handler type**: `builtin` (Anthropic's Messages API — needs an API
  key) or `http` (POSTs to your own externally hosted endpoint — no
  Anthropic key needed, useful for a different provider or an internal
  service).
- **Provider / model / API key** — the API key is encrypted at rest
  (`ENCRYPTION_KEY` must be set to register one with a key); no RPC ever
  returns it back, only whether one is configured.
- **Per-trigger enable flags** — separately toggle which lifecycle events
  fire this plugin (see the trigger table below) and a **requests/minute
  rate limit**.

**Proactive triggers** (fire automatically, no user action):

| Event | AI action |
|---|---|
| SEV opened (SEV-1 or SEV-2 only) | Suggest responders |
| SEV → Mitigated | Draft a mitigation summary + suggest root cause categories |
| SEV → Resolved | Draft a postmortem skeleton |
| Postmortem → In Review | Suggest action items |

**On-demand actions** (trigger any time from the SEV detail or postmortem
page): Summarize, Find similar SEVs, Draft announcement, Suggest prevention
tasks.

**Exclusions**: Sensitive SEVs are *always* excluded from proactive
dispatch, regardless of any other setting — their content is never sent to
a configured AI plugin. A SEV's own **"Disable AI plugin dispatch"**
checkbox (set at creation or later) opts it out of both proactive and
on-demand dispatch.

Every AI output is stored separately from the SEV record, clearly marked as
AI-generated, and never mutates SEV fields on its own — a human must
explicitly apply a suggestion.

### 14.6 Monitoring (Datadog / Prometheus / CloudWatch)

Two independent things are both called "monitoring config," worth telling
apart:

- **Per-SEV detection metadata** (no admin setup needed) — when creating or
  editing a SEV, record which tool fired (a closed choice: Datadog,
  Prometheus, CloudWatch, or Other), the alert name, and optionally a
  dashboard link and a saved query/expression. See [§2](#2-creating-a-sev).
- **Admin → Integrations → Monitoring** (a 5th configurable integration,
  alongside PagerDuty/GitHub/Slack/Jira) — has **no credential fields at
  all**; it's settings-only: a **Tool** dropdown (Datadog/Prometheus/
  CloudWatch — no "Other," since there's no base-URL shape to assume for an
  unnamed tool) and a **Base URL**. This exists purely as a durable place to
  record which monitoring tool your org uses and where, for reference — it
  has no live API integration and no health check of its own (its sidebar
  row always shows "No health check," since there's nothing to poll), and
  doesn't currently drive any other part of the app.

There's no embedded chart-snapshot rendering yet — that's a possible future
direction, not current behavior.

### 14.7 Email (for notifications)

Under **Admin → Integrations → Email**, enter your SMTP server's host, port,
username, and password, plus a **From address**. There's no environment
variable fallback for any of these — unlike the other five integrations,
email is configured exclusively through this form. There's also no live
health check (the sidebar row always shows "No health check") — the only
way to confirm it works is to configure a notification rule (see
[§17](#17-notifications--alerting)) and trigger the event it routes on.

This exists purely to back the **email** channel type in notification
routing rules — it has no other purpose in Sevitout.

---

## 15. Admin configuration reference

All of the following live under **Admin →** and require the Admin role.

### 15.1 Service registry

Sevitout keeps its own lightweight list of services: name, description,
owning team, PagerDuty service ID, tags. Services are referenced throughout
the system (affected services on a SEV, SLI records, PagerDuty lookup).
Deactivating a service removes it from new selections but keeps historical
SEV references intact.

### 15.2 Users

View all registered users, promote/demote organization roles, and
deactivate a user (revokes access without deleting their historical
attribution on past SEVs). Searchable by name and email.

### 15.3 On-call

Define on-call rotations (name, linked service(s), PagerDuty schedule ID),
or manage on-call manually with time-windowed overrides. History is
preserved so a SEV always shows who was actually on-call at the time.

### 15.4 Integrations

A sidebar of the five fixed integrations (PagerDuty, GitHub, Slack, Jira,
Monitoring), each with a schema-driven credential/settings form — see
[§14](#14-configuring-integrations) above for what to put in each one. Every
row also shows a live health badge: green **Connected**, red **Error**
(clicking through shows the underlying error message plus a short
troubleshooting hint), or gray **Not configured**/**No health check**
(Monitoring always shows this last one — it has no live check by design).

### 15.5 AI plugins

Register/enable/disable plugins and set provider, model, encrypted API key,
and rate limits — see [§14.5](#145-ai-plugins).

### 15.6 Data retention

See [§16](#16-data-retention) below.

### 15.7 Notifications & Alerting

See [§17](#17-notifications--alerting) below.

---

## 16. Data retention

By default, **SEVs are retained forever** — nothing is auto-deleted. From
**Admin → Retention**, set a custom retention period (in days) per severity
level; `0` means forever.

On expiry, SEVs are **archived** (soft-deleted), not purged, unless
hard-delete is explicitly configured. Archived SEVs are excluded from
search and dashboards but remain accessible to Admins. Export a SEV to CSV
before it archives if you need a durable copy for compliance.

---

## 17. Notifications & Alerting

From **Admin → Notifications**, configure routing rules and escalation
thresholds. Two independent things live on this page:

### 17.1 Routing rules

Each rule is: **for this org role, on any of these events, post to this
Slack channel or email address** — optionally restricted to a severity level
or more critical. A single rule can cover several events (e.g. one Slack
rule for both "SLA at risk" and "SLA breached"), so you don't need a
separate rule per event just to reuse the same target. Add a rule with
**Add rule**, choose:

- **Role** — Viewer, Responder, Incident Commander, or Admin. This labels
  who the rule is for; it does **not** look up individual users holding
  that role and message them personally (see the limitation below).
- **Events** — check every event this rule should cover: SEV opened,
  updated, or status changed; an announcement posted; a postmortem becoming
  due (fires the moment a SEV resolves) or approved; a SEV escalating for
  having no Incident Commander (see [§17.2](#172-escalation)); or a SEV's
  SLA becoming at-risk or breached (see
  [§17.3](#173-sla-risk--breach-alerts)). At least one event is required.
- **Channel type** — Slack or Email (email requires
  [§14.7](#147-email-for-notifications) configured first).
- **Channel target** — a Slack channel name (e.g. `#incidents`) or an
  email address.
- **Max severity** (optional) — e.g. set to SEV-2 to express "only for
  SEV-1 and SEV-2," since severity 1 is the most critical. Leave unset to
  match every severity.

Delete a rule with the trash icon in its row. There's no edit-in-place in
the UI yet — delete and re-add to change a rule's events, target, or
severity filter.

**Send test**: once role, events, channel type, and channel target are
filled in, the **Send test** button (paper-plane icon) sends one real test
message per selected event straight to that channel or address — without
saving the rule first. Every already-saved rule has the same button in its
table row, for testing a rule you set up earlier without waiting for a real
SEV to trigger it. Results appear per event right below: "sent" on success,
or the actual delivery error (e.g. "slack integration unavailable", "email
integration is missing smtp_host or from_address") when something's wrong —
this is the fastest way to debug a rule that isn't delivering. A test send
never gets routed through any other rule's matching logic and never counts
toward a real event.

**Important limitation**: a rule is a fixed broadcast — it always posts to
the same channel or address, for anyone matching that role. It is **not**
a personal notification to whichever specific person is filling that role
on a given SEV (e.g. it won't DM *this SEV's* actual assigned Incident
Commander). If you need that, route the "SEV status changed" event to
that SEV's own incident channel instead (everyone in it, including its
IC, already sees it) — see [§14.1](#141-slack).

The message you receive includes the SEV's real ID (e.g.
`SEV-2026-0042`), title, severity, and status, and — once the SEV's
incident channel exists (see [§14.1](#141-slack)) — a link to it. That
channel link is normally missing from a SEV's very first "opened"
notification, since the channel is created by a separate process reacting
to that same event; it appears starting with the SEV's next notification
(a status change, for example).

### 17.2 Escalation

Also on **Admin → Notifications**, a 4-row table (one per severity level)
sets: **if a SEV at this level has been open longer than N minutes with no
Incident Commander assigned, alert.** Each row has a minutes threshold and
an enabled checkbox — all four start disabled with no threshold set. This
is checked once a minute in the background; assigning an Incident
Commander to a SEV clears its alert state, so a resolved gap doesn't
re-alert once the threshold refires.

To actually receive this alert, add a routing rule for the "no Incident
Commander" event (§17.1) — the threshold table alone only decides *when*
to alert, not *where*.

### 17.3 SLA risk & breach alerts

If you've configured per-service SLA targets (**Admin → Services → SLAs**
icon, or see `docs/roadmap.md` Phase 12 for the underlying design), Sevitout
checks once a minute whether any SEV's live SLA status has crossed into
**at risk** or **breached**, and fires the corresponding event —
route it to a channel via §17.1 the same way as any other event. Each SEV
gets at most one "at risk" alert and one "breached" alert over its
lifetime, not a repeat every minute.

### 17.4 What else already happens, independent of the page above

- The web app updates **live** via WebSocket while you have a SEV open —
  status changes, new announcements, role/task changes all appear without a
  manual refresh. This has nothing to do with the routing rules above and
  can't be configured — it's always on.
- If Slack is configured (see [§14.1](#141-slack)), *every* status change
  and `external`/`status-page` announcement pushes to the SEV's own
  incident channel automatically, regardless of anything configured on this
  page — that's a separate, always-on mechanism, not one of the rules
  above.

---

## 18. Further reading

- [`docs/architecture.md`](architecture.md) — system design, database
  schema, and every API service.
- [`docs/roadmap.md`](roadmap.md) — the current phased plan for engineering,
  observability, and feature work, updated as phases ship.
- [`docs/architecture-evolution.md`](architecture-evolution.md) — the design
  record for the observability/hardening work (request-scoped logging,
  metrics, health checks) now folded into `docs/architecture.md`'s §3.4.
- [`docs/requirements.md`](requirements.md) — the full functional
  specification this system was built against.
- [`demo/`](../demo/) — one runbook per milestone with exact `curl`/API
  walkthroughs and documented known limitations; the most precise reference
  for exact current behavior.
