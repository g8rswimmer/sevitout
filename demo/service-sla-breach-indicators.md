# Demo — Per-service SLA targets and breach indicators (Roadmap Phase 12)

## What was built

SEVs already compute MTTD/MTTM/MTTR (`internal/sev/metrics.go`), but nothing defined
what those numbers *should* be, and there was no way to tell at a glance whether a
SEV was on track. This phase adds per-service, per-severity-level SLA targets for
the three headline metrics, plus a live breach indicator wired into the SEV response
and UI.

- **Schema + store**: a new `service_slas` table (migration `000016`), one row per
  `(service_id, severity_level)`, with three nullable `*_target_seconds` columns —
  `NULL` means "no target set for that metric", not an instant breach.
  `store.ServiceSLAStore` follows the same shape as every other repository
  interface in this codebase (`Upsert`/`Get`/`Delete`/`ListByService` plus a
  `ListForServices` batch lookup), with in-memory and Postgres implementations.
- **Domain logic (`internal/sev/sla.go`)**:
  - `MostStrictSLA` reduces every attached service's configured row at a SEV's
    severity level to one effective target per metric, taking the **minimum**
    non-nil value — "if a SEV has multiple services, the most strict SLA applies."
  - `EvaluateSLA` derives **live** per-metric status (`not_applicable` / `ok` /
    `at_risk` / `breached`) on every read against `now`, the same derive-on-read
    discipline `internal/api/grpc/task.go`'s `isOverdue` already applies to task
    due dates: a SEV that's still open can show `at_risk` before its terminal
    timestamp is even recorded, and flips cleanly to `ok`/`breached` the moment
    that timestamp lands.
- **API surface**: `SEVResponse` gains a new `sla_status` field, computed in
  `SEVServer.sevToProtoWithSLA` and attached on every handler that returns a SEV
  (Create/Get/Update/List/TransitionStatus). `ConfigService` gains
  `GetServiceSLA`/`UpsertServiceSLA`/`DeleteServiceSLA`/`ListServiceSLAs` — reads at
  the Viewer floor (matching `GetService`/`ListServices`; the resolved numbers are
  already exposed to any Viewer via `sla_status` anyway), mutations at the Admin
  floor (matching `UpdateService`/`DeleteService`).
- **Admin UI**: `AdminServicesPage.tsx` gets a per-service "Manage SLAs" action
  (the new gauge icon) that expands a 4-row table — one per severity level — built
  directly on `AdminRetentionPage.tsx`'s own per-severity-level table pattern.
  Targets are entered in hours and converted to/from seconds at the API boundary
  (see Follow-up #4 below — this started as minutes, then changed).
- **SEV UI indicators**: a new `SLABadge` component (`components/sev/badges.tsx`),
  modeled on `TasksPanel.tsx`'s existing "Overdue" badge — renders nothing for
  `ok`/`not_applicable`, an amber "at risk" badge for `at_risk`, a destructive "SLA
  breached" badge for `breached`. Shown per-metric next to MTTD/MTTM/MTTR in
  `LifecyclePanel.tsx`, and as an overall summary badge next to the severity/status
  badges on both the SEV detail page and the SEV list.

## Follow-up, same phase: metric tooltips + an RTPC (postmortem-tail) SLA

Two gaps surfaced from user feedback right after the above shipped: the acronyms
(MTTD/MTTM/MTTR) were unexplained wherever they appeared, and the SLA targets only
covered incident *response* (detect/mitigate/resolve), not the postmortem tail teams
also want held to a deadline.

- **Metric definitions as hover/focus tooltips.** `METRIC_DEFINITIONS` (previously
  private to `LifecyclePanel.tsx`) moved to a new shared
  `web/src/lib/metricDefinitions.ts`, so both `LifecyclePanel.tsx` (per-SEV values)
  and `ServiceSLAEditor.tsx` (per-service target column headers) render the exact
  same plain-English text via the existing `InfoTooltip` component (hover *and*
  keyboard-focus, per its own doc comment — not mouse-only). The admin SLA editor's
  four target columns ("MTTD target (min)", etc.) each carry the icon next to their
  header now, not just the per-SEV metric fields that already had one.
- **New metric: RTPC — Resolution to Postmortem Complete**
  (`postmortem_completed_at − resolved_at`). This is the same point-A-to-point-B
  shape as the existing `DTTMSeconds` (detection→mitigation), deliberately **not**
  "from `started_at`" like MTTD/MTTM/MTTR — a SEV open for days before resolution
  shouldn't count against a fast postmortem turnaround. `ComputeMetrics` computes it
  alongside the other three; `EvaluateSLA` evaluates it against `ResolvedAt` as its
  baseline (not `StartedAt`); `MostStrictSLA` reduces it across services the same
  way as the other three. A 4th target column (`rtpc_target_seconds`) was added to
  `service_slas` (migration `000017`) and threaded through the same
  store/proto/handler/UI surface as the original three — `ServiceSLAEditor.tsx`'s
  table is now 4 target columns instead of 3, and `LifecyclePanel.tsx` shows a 5th
  metric field (RTPC, with its own `SLABadge`) alongside MTTD/MTTM/MTTR/DTTM.
  **Corrected mid-phase**: this metric first shipped as "MTTPC," measured from
  `MitigatedAt` — the postmortem clock is conventionally understood to start once
  the incident is *resolved*, not merely mitigated, so both the name and the
  baseline were changed to RTPC/`ResolvedAt` before this reached anyone outside
  this branch. Noted here rather than silently reconciled, per this project's usual
  practice for mid-phase corrections.
- **Live-verified**, continuing a real `cmd/server` session:

  ```bash
  # Configure a 24h RTPC target alongside the existing 5-minute MTTD target.
  curl -s -X PUT localhost:8080/v1/config/services/checkout/sla/1 -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' -d '{"severity_level":1,"mttd_target_seconds":300,"rtpc_target_seconds":86400}'

  # Resolve a SEV 10 minutes ago — well within the 24h target, even though
  # started_at was never set (proving RTPC's baseline really is resolved_at).
  curl -s -X POST localhost:8080/v1/sevs/SEV-2026-0001/transition -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' -d '{"to_status":"resolved","resolved_at":"<10 min ago>"}'
  # {"sla_status": {"rtpc": "ok", "rtpc_target_seconds": "86400", "overall": "ok", ...}}

  # Complete the postmortem ~26 hours after resolution — over the 24h target.
  curl -s -X POST localhost:8080/v1/sevs/SEV-2026-0001/transition -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"to_status":"postmortem_complete","postmortem_completed_at":"<~26h after resolved_at>"}'
  # {"rtpc_seconds": "94209", "sla_status": {"rtpc": "breached", "overall": "breached", ...}}
  ```

## Follow-up #2, same phase: tooltip UX fix + SLA targets at service-creation time

Two more rounds of user feedback:

- **Tooltip UX fix.** `InfoTooltip` (`components/ui/tooltip.tsx`) previously set both
  a `title` attribute *and* rendered its own styled `role="tooltip"` span — the
  native browser tooltip (an unstyled box, after the OS hover delay) was appearing
  **in addition to** the custom one, and `cursor-help` was swapping the pointer for
  a question-mark cursor on every hover. `title` is now removed entirely (the
  `sr-only` span already covers screen readers, and keyboard-focus already reveals
  the styled tooltip via `group-focus-visible`, so it added nothing but the
  duplicate box), and the cursor override is gone — hovering now shows exactly one
  tooltip, with the ordinary pointer.
- **SLA targets at service-creation time.** `AdminServicesPage.tsx`'s "New service"
  form previously only let an admin set SLAs *after* creating a service, via the
  separate "Manage SLAs" action. It now embeds the same 4-column,
  per-severity-level target table directly in the creation form (optional — a blank
  row is skipped). On submit, the service is created first, then
  `UpsertServiceSLA` is called once per severity level that has at least one
  non-blank field, in parallel. Shared logic (`SEVERITY_LEVELS`, the per-row form
  shape, minutes→seconds conversion) moved to a new `web/src/lib/slaTargets.ts` so
  the creation form and `ServiceSLAEditor.tsx` (still used for editing after the
  fact) can't drift apart; `ServiceSLAEditor.tsx`'s `ColumnHeader` (label + info
  tooltip) is exported and reused by both tables rather than duplicated.
  **Known limitation, stated explicitly**: if the service itself is created but one
  of its SLA upserts then fails, the service is not rolled back (and is made visible
  in the list immediately, specifically so this partial-failure state isn't hidden)
  — the admin sees the error and can retry via "Manage SLAs" on the now-existing
  service, rather than the form silently succeeding or the created service vanishing
  from view.

## Prerequisites

- `go build ./... && go vet ./... && go test ./...` and `web`'s `tsc -b && vitest
  run && oxlint` passing.
- No database needed for the walkthrough below — the store, evaluation, and RBAC
  behavior are all visible against the in-memory store (`DATABASE_URL` unset).

## Walkthrough

Live-verified against a real `cmd/server` process (in-memory store):

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 ./server &

curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}'
TOKEN=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1"}' | jq -r .token)
# The first registered user is Admin (existing bootstrap behavior).

# 1. register two services and give each a different SLA at SEV-1 — checkout's
#    MTTD target (5m) is stricter than payments' (10m); only payments sets an
#    MTTR target.
curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"checkout","name":"Checkout"}'
curl -s -X PUT localhost:8080/v1/config/services/checkout/sla/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"severity_level":1,"mttd_target_seconds":300}'

curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"payments","name":"Payments"}'
curl -s -X PUT localhost:8080/v1/config/services/payments/sla/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"severity_level":1,"mttd_target_seconds":600,"mttr_target_seconds":3600}'

# 2. open a SEV-1 attached to both services, started 10 minutes ago and not yet
#    detected — already past checkout's 5-minute MTTD target while still open.
curl -s -X POST localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout errors spiking","severity_level":1,
       "affected_services":["checkout","payments"],"started_at":"<10 min ago>"}'
# {
#   ..., "affected_services": ["checkout", "payments"],
#   "sla_status": {
#     "mttd": "at_risk",              <- live: elapsed 600s > checkout's 300s target
#     "mttm": "not_applicable",       <- neither service sets an MTTM target
#     "mttr": "ok",                   <- elapsed 600s < payments' 3600s target
#     "overall": "at_risk",
#     "mttd_target_seconds": "300",   <- the stricter of checkout's 300 / payments' 600
#     "mttr_target_seconds": "3600"
#   }
# }

# 3. mark it detected 8 minutes after it started — over the 300s target — and
#    watch mttd flip from a live "at_risk" to a finalized "breached".
curl -s -X PATCH localhost:8080/v1/sevs/SEV-2026-0001 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"detected_at":"<8 min after started_at>"}'
# {"mttd_seconds": "489", "sla_status": {"mttd": "breached", "overall": "breached", ...}}

# 4. GetSEV and ListSEVs both carry the same live sla_status on every read —
#    it's never trusted from storage.
curl -s localhost:8080/v1/sevs/SEV-2026-0001 -H "Authorization: Bearer $TOKEN" | jq .sla_status
curl -s localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" | jq '.sevs[0].sla_status'

# 5. RBAC: any Viewer can read a service's configured SLAs; only Admin can change
#    them.
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/v1/config/services/checkout/sla \
  -H "Authorization: Bearer $VIEWER_TOKEN"                      # 200
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/services/checkout/sla/2 \
  -H "Authorization: Bearer $VIEWER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"severity_level":2,"mttd_target_seconds":60}'
# {"code":7,"message":"insufficient permissions for /sevitout.v1.ConfigService/UpsertServiceSLA"}
# 403
```

Every line above was run against a live server, not hand-typed — the exact
"most-strict target wins," "live vs. finalized," and RBAC behavior described in
"What was built" is what actually came back.

The admin editor and SEV-page badges are covered by
`AdminServicesPage.test.tsx`'s new SLA test and `badges.test.tsx`'s `SLABadge`
suite — see Verify tests pass below.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage:
- `internal/sev/metrics_test.go`: `RTPCSeconds` computed from
  `resolved_at`/`postmortem_completed_at` (added to the all-timestamps case, plus a
  dedicated RTPC-only case and the no-timestamps nil case).
- `internal/sev/sla_test.go` (new): `MostStrictSLA`'s multi-service min-reduction
  (now across all four metrics) and no-rows case; `EvaluateSLA`'s four status
  outcomes per metric, the at-risk→breached/ok transition once the final timestamp
  lands, overall-is-worst-of-four, and RTPC specifically: measured from
  `ResolvedAt` (not `StartedAt`) via a case where those two baselines would give
  opposite answers, not-applicable before resolution, and breached once finalized.
- `internal/store/memory/memory_test.go`: `TestServiceSLAStore` — insert, get,
  not-found, update-preserves-ID, per-service listing in ascending severity order,
  batch `ListForServices` across multiple service IDs, delete/delete-not-found.
- `internal/api/grpc/config_test.go`: `ServiceSLA` RPC tests — valid upsert +
  round-trip (now asserting `rtpc_target_seconds` too), unknown service rejected,
  invalid severity level rejected, a zero-valued field clears its target
  (full-replace semantics), not-found on get/delete, list returns only configured
  severity levels in order, missing `service_id` rejected.
- `internal/api/grpc/sev_test.go`: `SLAStatus` tests on `GetSEV`/`ListSEVs` — the
  most-strict target resolves correctly across two attached services, a still-open
  SEV shows `at_risk` before any target is finalized, a late-detected SEV shows
  `breached` once `MTTDSeconds` is computed, a SEV with no attached service gets no
  `sla_status` at all; plus RTPC-specific: `RtpcSeconds` computed on the
  `PostmortemComplete` transition, and `sla_status.rtpc` correctly using
  `resolved_at` as its baseline via `GetSEV`.
- `web/src/components/sev/badges.test.tsx` (new): `SLABadge` renders nothing for
  `ok`/`not_applicable`/undefined, an "at risk" badge, a "breached" badge, and
  defaults its label to "SLA".
- `web/src/pages/admin/AdminServicesPage.test.tsx`: opening the SLA editor and
  saving a target converts hours to seconds correctly in the request body; every
  target column's info icon exposes the right metric definition text (present
  whether or not it's currently hovered — no `title` attribute involved anymore);
  an RTPC target saves correctly; and a new service can have an SLA target set
  inline at creation time, with a blank severity-level row triggering no
  `UpsertServiceSLA` call at all.

## Follow-up #3, same phase: a real bug from the MTTPC→RTPC rename

The Follow-up #1 rename (migration `000017` edited in place, from `mttpc_*` to
`rtpc_*` columns) was based on an assumption that turned out to be wrong: that
because this branch had never been pushed, no real database could have already
applied it. In practice, `golang-migrate` (the tool `deploy/docker-compose.yml`'s
`migrate` service and `make migrate` both use) tracks applied migrations **by
version number only** — it never diffs file content — so a local dev database
that had run `make migrate` against the pre-rename code (creating `mttpc_seconds`/
`mttpc_target_seconds`) recorded version 17 as applied and silently kept those
column names forever once the file's content changed underneath it. Every SEV
read or write then failed: `column "rtpc_seconds" does not exist (SQLSTATE
42703)` — surfacing in the UI as "the dashboard and SEV panels are not able to
list the SEVs."

**Fix**: a new migration, `000018_rtpc_column_rename_repair.up.sql`, does a
guarded `ALTER TABLE ... RENAME COLUMN` — `sevs.mttpc_seconds` →
`sevs.rtpc_seconds`, `service_slas.mttpc_target_seconds` →
`service_slas.rtpc_target_seconds` — wrapped in an `information_schema.columns`
existence check, so it repairs a drifted database and is a verified no-op on a
fresh one (000017 already creates the `rtpc_*` names directly there).
**Live-verified** both directions with a throwaway Postgres container: applying
the pre-rename migration set (reconstructed from this branch's own git history)
to reproduce the drifted state, confirming the exact `column "rtpc_seconds" does
not exist` failure on `CreateSEV`/`ListSEVs`/`GetDashboardMetrics`, then
confirming migration 18 repairs it — and separately, that a completely fresh
database migrates cleanly through 18 with no behavior change.

**Anyone who ran `make migrate` (or the `migrate` compose service) against this
branch before this fix needs to run it again** — `docker compose up migrate` or
`make migrate` — to pick up migration 18 and repair their database; no manual
SQL is required.

## Follow-up #4, same phase: the tooltip fix from Follow-up #2 didn't actually work, plus hours instead of minutes

Two more rounds of feedback:

- **The Follow-up #2 tooltip fix was incomplete.** Removing `title` did kill the
  duplicate native tooltip, but the styled tooltip itself was still invisible on
  every SLA-target column header — the real bug. `InfoTooltip` positions its popup
  with `position: absolute; bottom-full` (opening *upward*, above the icon), and
  every column-header caller sits inside `ServiceSLAEditor.tsx`/
  `AdminServicesPage.tsx`'s `overflow-x-auto` table wrapper. **Verified directly in
  headless Chrome** (screenshotted several isolated repros, not just reasoned
  about): a popup opening upward out of a `<table>` inside such a wrapper gets
  clipped at the wrapper's top edge — and, surprisingly, adding an explicit
  `overflow-y: visible` override on the wrapper (the textbook fix for the
  "overflow-x-auto implicitly computes overflow-y: auto" CSS gotcha) **did not
  help**; the clipping is specific to a `<table>` descendant of a scrollable
  wrapper, not the general case (a plain, non-table element with the same
  `overflow-x-auto` wrapper and the same override rendered its popup correctly).
  The actual fix: `InfoTooltip` now takes a `side?: 'top' | 'bottom'` prop
  (default `'top'`, unchanged for `LifecyclePanel.tsx`'s metric labels, which
  have their value directly underneath — opening downward there would cover it).
  `ServiceSLAEditor.tsx`'s `ColumnHeader` passes `side="bottom"`: opening
  downward never needs to escape the scrollable wrapper's box in either
  direction, so the table-specific clipping quirk never applies. Re-verified in
  headless Chrome with the exact real component markup and classes.
- **SLA targets are entered and displayed in hours, not minutes.** "48 hours" is
  far easier to reason about than "2880 minutes," and no SLA in practice needs
  finer-than-hour precision. `web/src/lib/slaTargets.ts`'s `minutesToSeconds` is
  now `hoursToSeconds` (`n * 3600`, was `n * 60`); `ServiceSLAEditor.tsx`'s
  `toForm` converts stored seconds back to hours the same way. Both SLA tables'
  column headers read "MTTD target (hrs)" (was "(min)"), and every input's
  `aria-label` says "hours" instead of "minutes." **No backend or wire-format
  change** — `*_target_seconds` stays the API/DB representation in both
  directions; only the admin UI's input/display unit changed, so this is
  backward-compatible with any target already stored under the old minutes-entry
  UI (which was always just seconds under the hood).

## Known limitations

- **No org-wide default/fallback SLA.** A SEV with no attached service, or whose
  attached services have no configured row at its severity level, simply shows no
  SLA indicator at all — this phase doesn't invent an implicit target.
- **No automated breach notifications.** This phase is indicators-only (a badge on
  read); pinging Slack or paging someone when a SEV crosses `at_risk` is a
  separate, unscoped follow-up.
- **No SLA compliance reporting/analytics.** This is per-SEV live status only —
  there's no "% of SEVs within SLA this quarter" rollup anywhere in this phase.
- **An `affected_services` entry that doesn't resolve to a real `Service.ID`**
  (loose free-text entry, per `ServiceChipEditor.tsx`'s doc comment) is silently
  excluded from the most-strict computation, the same way it's already excluded
  from `ServiceStore.Get` lookups elsewhere in the codebase.
- **`ListSEVs`'s SLA lookup isn't batched** — one small indexed `ListForServices`
  query per returned SEV, not a single batched query across the whole page. Matches
  this codebase's existing lack of batching elsewhere (e.g. `CreateSEV`'s on-call
  lookup) and is bounded by page size; a batched variant is a named follow-up if
  list-page latency becomes a real problem at larger scale.
