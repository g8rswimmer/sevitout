# M14d Demo — Admin Pages

## What was built

The fourth frontend sub-milestone (`docs/project-plan.md`'s M14d): a six-page admin
console at `/admin/*`, wired up against the M10 `ConfigService` backend — no backend
changes were needed for this milestone.

- **Service registry** (`/admin/services`) — full CRUD table for the internal
  service registry: create (id/name/description/owning team/PagerDuty service ID/
  tags), inline edit, deactivate/reactivate via the Active checkbox, and delete.
  `ServiceChipEditor`'s existing affected-services picker (M14b) reads from the same
  `GET /v1/config/services` list, so anything created here immediately shows up
  there.
- **User management** (`/admin/users`) — search the user directory by name/email,
  change a user's org role via a dropdown (applies immediately — `UpdateUserRole`),
  and deactivate/reactivate a user. This is the first UI for promoting a Viewer to
  a higher role; previously it was `curl`-only (M14a's demo doc's own "Known
  limitations" called this out).
- **On-call rotations** (`/admin/oncall`) — CRUD for rotations: name, an optional
  service (from the same registry above), an optional PagerDuty schedule ID, and
  a manual override (user ID, display name, and a start/end window using the same
  `DateTimeField` picker M14b introduced for SEV lifecycle timestamps).
- **Integrations** (`/admin/integrations`) — a table of every configured
  integration with its credential status and live health (via `GET
  /admin/integrations/health`, a **Refresh health** button re-runs the checks), plus
  an add/update form. PagerDuty, GitHub, and Slack are offered as known types with
  their well-known credential key pre-filled (`api_key`/`token`/`bot_token`
  respectively, matching `cmd/server/main.go`'s registered `HealthChecker`s); an
  "Other" option accepts any future integration type by name. Credentials are
  write-only — the form never shows or re-fetches a stored secret, only whether one
  is configured, matching `UpsertIntegrationConfigRequest`'s API contract.
- **AI plugins** (`/admin/ai`) — register/edit/delete plugins: name, version,
  description, handler type (built-in vs. HTTP endpoint, with the endpoint field
  only shown for HTTP), provider/model, a write-only API key field, an Enabled
  toggle, the four proactive-trigger checkboxes (open/mitigated/resolved/postmortem
  review — §11.1), and a per-minute rate limit.
- **Data retention** (`/admin/retention`) — one row per severity level (SEV-1
  through SEV-4) with an editable retention-days field and a hard-delete checkbox,
  matching `docs/requirements.md` §18.7 exactly (0 days = retain forever).
- **Admin nav & layout**: an **Admin** link appears in the top nav only for users
  with the Admin role (`hasRole`-gated, same pattern `SevCreatePage`'s Responder
  gating already used), leading to `AdminLayout`'s tab strip over the six pages
  above. The whole `/admin/*` subtree is wrapped in one `ProtectedRoute
  minRole="admin"` — a non-Admin who navigates there directly sees the same "you
  don't have permission" message every other role-gated route already shows, not a
  hidden/broken page.
- **Code-split**: the entire `/admin/*` subtree is one `React.lazy` chunk
  (`pages/admin/AdminRoutes.tsx`), the same "route lazy-loads its own nested
  `<Routes>`" pattern used for `PostmortemPage`'s TipTap dependency — six admin
  pages is enough weight that non-Admins shouldn't pay for it in the main bundle.

No confirmation dialogs on delete/deactivate actions — same direct
"click-triggers-the-mutation" convention `RolesPanel`/`LinkedSevsPanel`/
`TasksPanel`'s Remove/Unlink buttons already established in M14b.

---

## Prerequisites

- M14a–M14c complete
- Backend milestone M10 (Configuration API) — no backend changes were needed for
  this milestone, but its RPCs are what every page here calls
- Node.js 22+, `npm`
- `JWT_SECRET` set (or accept the dev default)
- `ENCRYPTION_KEY` set (base64-encoded 32 bytes, `openssl rand -base64 32`) if you
  want to actually save integration credentials or an AI plugin API key — without
  it, `UpsertIntegrationConfig`/`CreateAIPlugin`/`UpdateAIPlugin` reject any request
  that supplies one (M10's own behavior, unchanged here)

---

## Start the stack

Same two options as prior M14 sub-milestones:

```bash
# Option A — Docker Compose
cp .env.example .env   # if you don't already have one
make up

# Option B — local dev servers
JWT_SECRET=dev-secret-please-change JWT_TTL_HOURS=24 ENCRYPTION_KEY=$(openssl rand -base64 32) go run ./cmd/server   # terminal 1
cd web && npm install && npm run dev                                                                                # terminal 2
```

Open **http://localhost:3000** (Option A) or **http://localhost:5173** (Option B).

---

## Walkthrough

1. **Log in as the bootstrap Admin.** If this is a fresh database, register the
   first account (`docs/requirements.md` §14: the first user ever registered gets
   the Admin role) — `/register`, e.g. `admin@example.com` / `password123`. Confirm
   you see an **Admin** link in the top nav (a non-Admin session never shows it).
2. **Service registry.** Go to **Admin → Services**, click **New service**, fill in
   an ID (`checkout`), a name, an owning team, and a tag (`tier=1`), then **Create**.
   It appears in the table immediately. Click **Edit** on it, change the owning
   team, **Save** — confirm the change persists. Uncheck **Active** and save; the
   badge switches to "Inactive". Open `/sevs/new`'s Detection section's affected-
   services picker (M14b) and confirm `checkout` shows up there too, since both
   read the same registry.
3. **User management.** Go to **Admin → Users**, register a second account from
   another browser/incognito window (or `curl -X POST /auth/register`) so there's
   someone to manage — it lands as Viewer by default. Search for them by email,
   change their role to Responder via the dropdown (applies immediately, no
   separate Save), then click **Deactivate** — the badge flips to "Deactivated" and
   the button becomes **Reactivate**.
4. **On-call rotations.** Go to **Admin → On-Call**, click **New rotation**, name
   it, pick the `checkout` service from step 2, and leave the PagerDuty/manual
   fields blank for a normal rotation — **Create**. Click **New rotation** again for
   a manual override: fill in a display name and use the **Override start**/**end**
   date pickers, leave PagerDuty blank — **Create**. Edit either one and confirm the
   change round-trips.
5. **Integrations.** Go to **Admin → Integrations**. In "Add / update integration",
   the Type dropdown defaults to PagerDuty with `api_key` pre-filled as the
   credential key — type a (fake, for this demo) value and **Save integration**. It
   appears in the table above as "Configured" with a health badge — click
   **Refresh health** to re-run the live check (a fake key reports "Error" with
   PagerDuty's rejection reason in a tooltip; a real key reports "Connected").
   Switch Type to **Other…**, name it `datadog`, save — it appears with health
   "No health check" (no `HealthChecker` is registered for it yet, matching
   `cmd/server/main.go`'s `healthCheckers` map).
6. **AI plugins.** Go to **Admin → AI Plugins**, click **New plugin**, name it,
   leave Handler type as "Built-in", set a provider/model, optionally an API key,
   check a couple of proactive triggers, **Create**. Edit it and toggle **Enabled**
   off — **Save** — confirm the Status column flips to "Disabled". Switch Handler
   type to "HTTP endpoint" on a new plugin and confirm the endpoint field appears
   only then.
7. **Data retention.** Go to **Admin → Retention** — all four severity levels
   already show a row (seeded by M01's migration with `retention_days: 0`, retain
   forever). Set SEV-4's retention to `365` days, leave "Hard delete" unchecked
   (archive, not purge), and **Save**.
8. **Confirm the gate.** Log out and log back in as the Viewer/Responder account
   from step 3, then navigate directly to `/admin/services` — you see "You don't
   have permission to view this page," not the admin UI or a crash, and there's no
   **Admin** link in the nav to find it from.

---

## Verify tests pass

```bash
cd web
npm run build
npx oxlint
npm test
```

The Go side is untouched by this sub-milestone (confirmed by re-running its full
suite unchanged): `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...`,
`go test -race ./...`, and `golangci-lint run ./...` all still pass.

84 Vitest/RTL tests (23 new):

- `pages/admin/AdminServicesPage.tsx` — renders the registry with its tags,
  creates a service with the exact expected request body, edits and saves a
  field, deletes directly (no confirmation step).
- `pages/admin/AdminUsersPage.tsx` — renders the directory, searches by query,
  changes a role via the select (fires immediately), deactivates then reactivates.
- `pages/admin/AdminOnCallPage.tsx` — renders rotations with their resolved
  service name (not a bare ID), creates, edits, deletes.
- `pages/admin/AdminIntegrationsPage.tsx` — renders configured integrations with
  their health badge, saves a known type with its well-known credential key
  pre-filled, supports a custom "Other" type.
- `pages/admin/AdminAIPluginsPage.tsx` — renders registered plugins, creates one
  while always sending the boolean/rate-limit fields explicitly (never omitting a
  meaningful `false`/`0` as if it were unset — see the wrapper-type note below),
  shows the HTTP endpoint field only for the HTTP handler type, edits and deletes.
- `pages/admin/AdminRetentionPage.tsx` — renders all four severity levels (filling
  in defaults for any the backend hasn't returned yet), saves one level's policy.
- `components/layout/AppLayout.tsx` — the Admin nav link is hidden for a
  non-Admin and shown (linking to `/admin`) for an Admin.

Manually verified end-to-end against a live server before writing any frontend
code, and again after: full CRUD for services/on-call/AI plugins, user role
change/deactivate/reactivate, `UpsertIntegrationConfig` + `GET
/admin/integrations/health`, and `GetRetentionConfig`'s defaults — confirming
along the way that `retention_days`, every AI-plugin boolean
(`enabled`/`trigger_on_*`), and `rate_limit_per_minute` are all **omitted from the
wire entirely when false/0** (protojson's zero-value-omission rule, `types/api.ts`'s
header comment) rather than sent as an explicit falsy value — which is why every
admin form here always sends its *entire* current state on save rather than only
changed fields, so an intentional "set this back to false/0" is never silently
dropped as if it were "unchanged."

---

## Bug fix: five of the six admin tabs lost everything on restart

Reported after this milestone shipped: a service created through **Admin →
Services** disappeared after the API restarted. Asked to check whether the other
tabs had the same problem — they did, for everything except **Users**.
`cmd/server/main.go`'s `buildStores` wired `ServiceStore`, `OnCallStore`,
`IntegrationConfigStore`, `RetentionConfigStore`, and `AIPluginStore` to their
in-memory implementations even when `DATABASE_URL` was set and a real Postgres
connection existed — the exact same class of bug M14c's postmortem-persistence fix
found and fixed for `PostmortemStore`, and the server even said so out loud on
every startup: `"...oncall, integration-config, retention-config, ai-plugin...
stores are in-memory — data will not persist across restarts (postgres
implementations deferred)"` (this repo's own M10 "Known limitations" had flagged
exactly this, unaddressed since M01). `UserStore` was never affected — it already
had a Postgres implementation, which is why role changes/deactivation in **Admin →
Users** were never in question.

Fixed by adding `internal/store/postgres/{service,oncall,integrationconfig,
retentionconfig,aiplugin}.go`. As with the postmortem fix, none of this needed new
SQL: `internal/store/sql/{services,oncall,integrationconfig,retentionconfig,
aiplugins}.sql` and their sqlc-generated `internal/store/queries/*.sql.go` code had
existed unused since M01 — only the `store.XStore` wrapper implementing each
interface against that generated code was missing. No migration was needed either;
all five tables (including `retention_config`'s pre-seeded severity 1-4 rows) have
existed since M01's schema.

Verified directly against the real Docker Compose stack: created a service, an
on-call rotation, a PagerDuty integration, an AI plugin, and a retention policy
change, ran `docker restart sevitout-api-1`, and confirmed every one of them was
still there afterward (this exact sequence returned an empty list for each before
the fix) — then exercised update and delete on each through the running API to
confirm the full CRUD path, not just create, round-trips through Postgres
correctly.

---

## Known limitations

- **No confirmation dialogs** on Delete (services, on-call rotations, AI plugins)
  or Deactivate (users) — a misclick isn't undoable from the UI. This matches the
  rest of the app's existing Remove/Unlink convention (M14b), not a gap unique to
  this milestone, but it's worth calling out here given how much more consequential
  deleting a service or deactivating a user is than unlinking one task.
- **The integration health check only has built-in checkers for PagerDuty, GitHub,
  and Slack** (`cmd/server/main.go`'s `healthCheckers` map) — any other
  `integration_type` (Datadog, Prometheus, CloudWatch, or a typo) always reports
  "No health check", which is a legitimately different state from "Error" and is
  labeled as such, not a bug.
- **No audit-log surface for admin actions** — role changes, deactivations, and
  integration/plugin/retention edits are written to the audit log server-side
  (`slog.InfoContext` calls in `internal/api/grpc/config*.go`) but there's still no
  frontend view of the audit log at all (same gap `demo/M14c-postmortem-editor.md`
  already noted for the unlock reason).
- **No guard against locking yourself out** — nothing stops an Admin from
  demoting or deactivating their own account (or the last remaining Admin) from
  this UI, matching M10's own documented limitation.
