# M14b Demo — SEV List & SEV Detail

## What was built

The second frontend sub-milestone (`docs/project-plan.md`'s M14b): a searchable SEV
list, a create form, and the full SEV detail page with every M06/M07/M09 section
wired up live.

- **`/sevs`** — search bar (`SearchService.SearchSEVs`'s full-text `query`), a filter
  sidebar (severity checkboxes, status checkboxes, disabled while a quick view is
  active), quick-view tabs (`All` / `Open` / `My SEVs` / `Awaiting Postmortem`, mapping
  to `quick_view`), sort controls (field + asc/desc), and pagination. Every SEV row
  shows its severity and status badges, affected services, start time, and MTTR.
- **`/sevs/new`** — a Responder+-gated create form covering every `CreateSEVRequest`
  field: title, severity, description, affected services (a chip editor that both
  free-types and suggests from the service registry), started/detected-at, detection
  metadata (see below), tags, and the sensitive/AI-disabled flags.
- **`/sevs/:id`** — replaces M14a's placeholder with the full detail view:
  - **Header** — severity/status/sensitive/locked badges, title, and a status
    transition control (Incident-Commander+) showing only the state machine's actual
    valid next statuses (`internal/sev/statemachine.go`, mirrored client-side for UX
    only — the server is still the authority). Transitioning to `mitigated` or
    `resolved` prompts for a timestamp first, because `TransitionStatus` only
    auto-defaults `postmortem_completed_at`, not `mitigated_at`/`resolved_at` — without
    one, MTTM/MTTR would never compute.
  - **Lifecycle** — all five lifecycle timestamps plus MTTD/MTTM/MTTR/DTTM, with
    edit-in-place (Responder+, unlocked only) for `started_at`/`detected_at`.
  - **Details** — description, root cause, mitigation, prevention, business impact,
    affected services, detection metadata, tags, and the "right people in the room"
    flag/notes, all edit-in-place as one form (see "Known limitations" for what
    "edit-in-place" means here). **Detection metadata** (`components/sev/
    DetectionFields.tsx`, shared by this panel and the create form) is: detection
    method as a dropdown constrained to `docs/requirements.md` §4.2's fixed
    vocabulary (enforced server-side too — `internal/api/grpc/sev.go`'s
    `validateDetectionMethod`); monitoring tool as a dropdown (Datadog/Prometheus/
    CloudWatch/None/Other, the last with a free-text companion field, since
    monitoring_tool itself stays unrestricted text on the wire); and three optional
    links — alert name directly above its link, then the monitoring metric/
    dashboard link and a snapshot image URL, the last rendered as an inline
    `<img>` preview with a graceful fallback if it fails to load. `started_at`/
    `detected_at` (Lifecycle panel, and the create form; `components/sev/
    DateTimeField.tsx`) pair an `<input type="datetime-local">` (a native
    browser date+time picker) with an explicit calendar button that calls the
    input's own `showPicker()` — an unambiguous "open the picker" affordance
    rather than a passive icon. Left blank, `started_at` is no longer defaulted
    to "now" on create: the caller sets it explicitly or it stays unset.
  - **Roles** — list + assign/remove (Incident-Commander+), matching every
    `internal/store.SEVRoleType`.
  - **Announcements** — feed + post form with audience (`internal`/`external`/
    `status-page`) and milestone flag (Responder+).
  - **Chat log** — feed + manual entry form (Responder+).
  - **Linked tasks** — list with an overdue badge, "link existing" and "create GitHub
    issue" forms (Responder+; the latter 503s cleanly if `GITHUB_TOKEN` isn't
    configured, same as M07's curl demo).
  - **Linked SEVs** — list + link/unlink form (Responder+); each target is a live link
    to its own `/sevs/:id`.
  - **Live updates** — `lib/ws.ts`'s `useSevSocket` subscribes to `/ws?sev_id=<id>` for
    as long as the page is open and invalidates the affected TanStack Query cache
    entries on any matching event, per `docs/architecture.md` §3.2's "WS event →
    cache invalidation" design — a second browser tab open on the same SEV sees
    another user's announcement/chat/role/task change appear without a manual reload.

### A real integration problem, solved: browser WebSocket auth

A native browser `WebSocket` cannot set an `Authorization` header — there's no API for
it — so M14a's Bearer-token-in-`localStorage` design (chosen because `/auth/login`
never sets a cookie) had no way to authenticate the `/ws` handshake. Rather than
change the backend, `lib/api.ts`'s `tokenStorage` now also mirrors the token into a
plain (necessarily non-httpOnly) `token` cookie on login/logout, alongside the
existing localStorage copy. The browser then sends that cookie automatically on the
WebSocket upgrade request, which lands on `internal/auth.ExtractBearerToken`'s
existing cookie fallback — the same fallback the REST gateway's `WithMetadata` already
had, seemingly built for exactly this. Verified directly with a raw HTTP/1.1 upgrade
request carrying only the cookie (no `Authorization` header) — see the walkthrough.
This cookie is exactly as exposed to script-injection as the localStorage copy
already is; mirroring the same secret into it doesn't create a new exposure.

**Also caught and fixed**: `protojson` encodes every `int64` field as a JSON string,
not a number — this was already known for `*_seconds` duration fields (M14a), but
turns out to also apply to every sub-resource's database `id` (roles, announcements,
chat entries, tasks, SEV links). Confirmed against a live server; `types/api.ts`'s
`id` fields are typed `string` for this reason, not `number`.

---

## Prerequisites

- M14a complete
- Backend milestones through M09 (WebSocket), M07 (linked tasks) — for the sections
  this page renders
- Node.js 22+, `npm`
- `JWT_SECRET` set (or accept the dev default)

---

## Start the stack

Same two options as M14a:

```bash
# Option A — Docker Compose
cp .env.example .env   # if you don't already have one
make up

# Option B — local dev servers
JWT_SECRET=dev-secret-please-change JWT_TTL_HOURS=24 go run ./cmd/server   # terminal 1
cd web && npm install && npm run dev                                      # terminal 2
```

Open **http://localhost:3000** (Option A) or **http://localhost:5173** (Option B).

---

## Walkthrough

1. **Register and open a SEV.** Register (first user → Admin, per M14a). Click **New
   SEV** from `/sevs` (or the dashboard). Fill in a title, pick SEV-2, type a service
   name and press Enter to chip it, leave the rest default, submit. You land on
   `/sevs/:id`.
2. **Confirm the detail page renders every section**: header badges (SEV-2, Open),
   Lifecycle (Started populated, everything else `—`), Details (your description/
   services), and empty states for Roles/Announcements/Chat log/Linked tasks/Linked
   SEVs ("No … yet").
3. **Assign a role.** In Roles, pick "Incident Commander", type a name, **Assign**.
   It appears immediately (no reload) — this is a plain refetch-on-mutation, not a WS
   round trip yet.
4. **Post an announcement and add a chat entry** — confirm both appear in their feeds,
   newest announcement first, chat entries in chronological order.
5. **Link a task**: paste any URL and a title under "Link existing" → **Link task**.
   Try **Create GitHub issue** instead (owner/repo/title) — expect a clean error
   ("GitHub integration is not configured") unless `GITHUB_TOKEN` is set, matching
   `demo/M07-linked-tasks.md`.
6. **Edit Details in place**: click **Edit** on the Details panel, change the
   description, toggle "Right people were in the room", **Save** — the read view
   updates immediately.
7. **Transition status**: as Admin (Incident-Commander+), the header shows
   **Transition to: Investigating, Mitigated** (Open's valid next states). Click
   **Mitigated** — a timestamp field appears defaulted to now; confirm. The status
   badge updates and MTTM appears in Lifecycle.
8. **Watch a live update land from a second tab.** Open the same `/sevs/:id` URL in a
   second browser tab. In tab 1, post another announcement. In tab 2, without
   reloading, it appears within a couple seconds — the WebSocket event invalidated
   tab 2's announcements query.
9. **Verify the WS-over-cookie fix directly**, no browser needed:
   ```bash
   TOKEN=<a valid token, e.g. from /auth/login's response>
   curl --http1.1 -i -N --max-time 2 \
     -H "Connection: Upgrade" -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     -H "Cookie: token=$TOKEN" \
     "http://localhost:3000/ws?sev_id=<your-sev-id>"
   ```
   Expected: `HTTP/1.1 101 Switching Protocols` — note there's no `Authorization`
   header anywhere in that request; only the cookie.
10. **Try the list page**: go to `/sevs`, search for part of your SEV's title, switch
    between quick-view tabs, toggle a severity/status filter, change the sort field
    and direction — confirm the result set and count update each time.
11. **Try it as a Viewer**: register a second account (a non-first user, so it's a
    Viewer by default). Open the same SEV — confirm every "Edit"/"Assign"/"Post"/
    "Link"/transition control is gone; only read views remain.

---

## Verify tests pass

```bash
cd web
npm run build
npx oxlint
npm test
```

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

The Go side did later gain the detection-links backend (migration `000005`, three new
`SEVResponse`/`CreateSEVRequest`/`UpdateSEVRequest` proto fields, and
`validateDetectionMethod`) and the repository field (migration `000006`,
`github_repo`) — see "Richer detection metadata" and "SEV page layout and reference
fields" below; all the above still passes with both included.

46 Vitest/RTL tests:

- `lib/ws.ts` — connects to `/ws?sev_id=`, invalidates the matching query key on a
  same-SEV event, ignores events for other SEVs and malformed frames, reconnects
  after a close, and closes cleanly (no reconnect) on unmount — all against a mocked
  `WebSocket` global (jsdom has none).
- `pages/SevListPage.tsx` — renders results, requests the right `quick_view` param on
  a tab click, requests the typed `query` on search submit.
- `pages/SevCreatePage.tsx` — submits the exact expected `CreateSEVRequest` body
  (including a chip-added affected service and, separately, a selected detection
  method, a custom "Other" monitoring tool, an alert name, and all three detection
  links — asserting `started_at`/`detected_at` are omitted, not defaulted, when
  left untouched, and that alert name sits before alert link in DOM order),
  surfaces a server validation error, and confirms the date/time picker button
  calls the input's own `showPicker()`.
- `pages/SevDetailPage.tsx` — renders every section read-only for a Viewer with no
  write affordances present, including detection metadata as human labels, the
  three links/snapshot preview, and the repository link, with Details positioned
  before Lifecycle in the DOM; a Responder can post an announcement end-to-end, edit
  the root cause category via "Other" plus business impact plus repository and see
  the exact `UpdateSEVRequest` body, pre-fill a GitHub issue's owner/repo from the
  SEV's repository, and pick a Linked SEVs autocomplete suggestion by title to link
  it; an Incident Commander sees the correct valid-next-status transition buttons
  for the SEV's current status.

---

## Richer detection metadata (follow-up)

A follow-up request tightened up the Detection section of the create form and
Details panel:

- **Detection method** is now a dropdown constrained to `docs/requirements.md`
  §4.2's fixed vocabulary (alert, monitoring dashboard, customer report, synthetic
  test, manual discovery, Slack escalation) — enforced server-side too, via
  `internal/api/grpc/sev.go`'s `validateDetectionMethod` (same "closed switch,
  reject unknown" pattern `role.go` already used for `role_type`), backed by a new
  `store.DetectionMethod` type.
- **Monitoring tool** is a dropdown (Datadog / Prometheus / CloudWatch / None /
  Other), but stays free text on the wire — "Other" reveals a companion text input
  rather than losing whatever the caller actually names, so the backend never
  rejects a legitimate tool the three named ones don't cover.
- **Three new optional links**, added as columns on `sevs` (migration `000005`) and
  fields on all three SEV proto messages: `alert_url` (the alert itself),
  `metric_link` (the monitoring dashboard/metric/query), and `snapshot_url` — a URL
  to an already-hosted image, not a file upload (no blob storage exists in this
  system), rendered as an inline `<img>` preview with a graceful fallback if the
  link doesn't load. **Alert name sits directly above alert link** — the two
  describe the same alert — rather than being separated by the detection-method/
  monitoring-tool dropdowns.
- `started_at`/`detected_at` (`web/src/components/sev/DateTimeField.tsx`) pair the
  existing `<input type="datetime-local">` with a calendar button that calls the
  input's own `showPicker()`, so opening the native date+time picker is an
  explicit, clickable action rather than an undiscoverable side effect of clicking
  the text field. **`started_at` is no longer defaulted to "now" when omitted on
  create** — `docs/requirements.md` §2.1 already said this timestamp "may be
  estimated"; leaving it unset until the caller sets it explicitly reads that more
  literally than assuming "now" ever did.

New shared components: `web/src/components/sev/DetectionFields.tsx` and
`DateTimeField.tsx`, used by both `SevCreatePage` and `DetailsPanel`'s (plus
`LifecyclePanel`'s) edit mode.

---

## SEV page layout and reference fields (second follow-up)

- **Details now renders above Lifecycle** on `/sevs/:id` — reads better as "what
  happened" before "when it happened."
- **Lifecycle lists the full six-stage sequence** (`docs/requirements.md` §2.3: Open
  → Investigating → Mitigated → Resolved → Postmortem In Progress → Postmortem
  Complete), current stage highlighted, above the existing timestamp/metric facts —
  the state machine allows stepping back/re-opening, so this is a display ordering,
  not a claim a SEV only moves forward through it.
- **MTTD/MTTM/MTTR/DTTM each have an info icon** (`components/ui/tooltip.tsx`, a
  small CSS-only tooltip — hover *or* keyboard focus, plus a native `title`
  fallback; no new dependency) spelling out the acronym and formula.
- **Details**: business impact is now its own line (previously paired with root
  cause category in a two-column row); **root cause category is a dropdown**
  (deployment/configuration/hardware/dependency — `docs/requirements.md` §4.2's
  literal examples — plus "Other" with a free-text companion, same pattern
  `DetectionFields` already uses for monitoring tool; stays free text server-side,
  no new validation); and a new **Repository** field (`github_repo`, "owner/repo",
  new migration `000006`) renders as a `github.com` link.
- **Linked tasks' "Create GitHub issue" pre-fills owner/repo** from that same
  `github_repo` field (split on `/`) when you switch to that mode with both fields
  still empty — one less thing to retype.
- **Linked SEVs now has an autocomplete** (`components/sev/SevAutocomplete.tsx`):
  typing 2+ characters searches `SearchService.SearchSEVs` (debounced 250ms) and
  shows up to 6 matches with their severity badge and title, not just a bare ID
  list. The field's value is still literally the `target_sev_id` being submitted,
  so typing an exact known ID directly — no suggestion click — works exactly as
  before; picking a suggestion just fills the ID in for you.

---

## Known limitations

- **No SLI panel.** `docs/requirements.md` §4.3 and this milestone's plan both call
  for one, but there is no `SLIService` (or any SLI fields on `SEVResponse`) anywhere
  in `proto/`, even though `store.SLIStore` has existed since M01 — it was never wired
  to a gRPC service in any backend milestone. There's no API to build a panel against;
  adding one is a backend gap, not a frontend one.
- **"Edit-in-place" is one toggle per panel, not per field.** Lifecycle and Details
  each have a single Edit → form → Save/Cancel flow rather than independently
  editable fields — simpler to build and use, and still satisfies "edit without
  leaving the page," but isn't literally inline-per-field editing.
- **Linked SEVs don't live-update.** `SEVLinkService` never publishes a WebSocket
  event (confirmed by inspecting `internal/api/grpc/sev_link.go`), unlike every other
  panel — a link/unlink is only reflected after your own mutation's refetch, not
  pushed to other open tabs/users.
- **No user directory search for role assignment.** Roles are assigned by free-form
  display name (`docs/requirements.md` §5 explicitly allows this), not looked up
  against the user directory — `ConfigService.ListUsers` is Admin-only, so a Responder
  assigning an IC couldn't search it anyway; full user management is M14d.
- **The affected-services chip editor doesn't validate against the registry** — it
  accepts any typed value, consistent with §4.2's "linked to a service registry or
  free-form."
- **No optimistic UI.** Every write waits for its response before updating; combined
  with the in-memory store's near-instant responses this isn't very noticeable
  locally, but a slow/real network would show it.
- **List page pagination is offset-based**, matching `SearchSEVsRequest` directly —
  fine at this data scale, not cursor-based.
