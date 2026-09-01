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
  Targets are entered in minutes and converted to/from seconds at the API boundary.
- **SEV UI indicators**: a new `SLABadge` component (`components/sev/badges.tsx`),
  modeled on `TasksPanel.tsx`'s existing "Overdue" badge — renders nothing for
  `ok`/`not_applicable`, an amber "at risk" badge for `at_risk`, a destructive "SLA
  breached" badge for `breached`. Shown per-metric next to MTTD/MTTM/MTTR in
  `LifecyclePanel.tsx`, and as an overall summary badge next to the severity/status
  badges on both the SEV detail page and the SEV list.

## Follow-up, same phase: metric tooltips + an MTTPC (postmortem-tail) SLA

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
- **New metric: MTTPC — Mitigation to Postmortem Complete**
  (`postmortem_completed_at − mitigated_at`). This is the same point-A-to-point-B
  shape as the existing `DTTMSeconds` (detection→mitigation), deliberately **not**
  "from `started_at`" like MTTD/MTTM/MTTR — a SEV open for days before mitigation
  shouldn't count against a fast postmortem turnaround. `ComputeMetrics` computes it
  alongside the other three; `EvaluateSLA` evaluates it against `MitigatedAt` as its
  baseline (not `StartedAt`); `MostStrictSLA` reduces it across services the same
  way as the other three. A 4th target column (`mttpc_target_seconds`) was added to
  `service_slas` (migration `000017`) and threaded through the same
  store/proto/handler/UI surface as the original three — `ServiceSLAEditor.tsx`'s
  table is now 4 target columns instead of 3, and `LifecyclePanel.tsx` shows a 5th
  metric field (MTTPC, with its own `SLABadge`) alongside MTTD/MTTM/MTTR/DTTM.
- **Live-verified**, continuing a real `cmd/server` session:

  ```bash
  # Configure a 24h MTTPC target alongside the existing 5-minute MTTD target.
  curl -s -X PUT localhost:8080/v1/config/services/checkout/sla/1 -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' -d '{"severity_level":1,"mttd_target_seconds":300,"mttpc_target_seconds":86400}'

  # Mitigate a SEV 10 minutes ago — well within the 24h target, even though
  # started_at was never set (proving MTTPC's baseline really is mitigated_at).
  curl -s -X POST localhost:8080/v1/sevs/SEV-2026-0001/transition -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' -d '{"to_status":"mitigated","mitigated_at":"<10 min ago>"}'
  # {"sla_status": {"mttpc": "ok", "mttpc_target_seconds": "86400", "overall": "ok", ...}}

  # Complete the postmortem ~26 hours after mitigation — over the 24h target.
  curl -s -X POST localhost:8080/v1/sevs/SEV-2026-0001/transition -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"to_status":"postmortem_complete","postmortem_completed_at":"<~26h after mitigated_at>"}'
  # {"mttpc_seconds": "94209", "sla_status": {"mttpc": "breached", "overall": "breached", ...}}
  ```

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
- `internal/sev/metrics_test.go`: `MTTPCSeconds` computed from
  `mitigated_at`/`postmortem_completed_at` (added to the all-timestamps case, plus
  a dedicated MTTPC-only case and the no-timestamps nil case).
- `internal/sev/sla_test.go` (new): `MostStrictSLA`'s multi-service min-reduction
  (now across all four metrics) and no-rows case; `EvaluateSLA`'s four status
  outcomes per metric, the at-risk→breached/ok transition once the final timestamp
  lands, overall-is-worst-of-four, and MTTPC specifically: measured from
  `MitigatedAt` (not `StartedAt`) via a case where those two baselines would give
  opposite answers, not-applicable before mitigation, and breached once finalized.
- `internal/store/memory/memory_test.go`: `TestServiceSLAStore` — insert, get,
  not-found, update-preserves-ID, per-service listing in ascending severity order,
  batch `ListForServices` across multiple service IDs, delete/delete-not-found.
- `internal/api/grpc/config_test.go`: `ServiceSLA` RPC tests — valid upsert +
  round-trip (now asserting `mttpc_target_seconds` too), unknown service rejected,
  invalid severity level rejected, a zero-valued field clears its target
  (full-replace semantics), not-found on get/delete, list returns only configured
  severity levels in order, missing `service_id` rejected.
- `internal/api/grpc/sev_test.go`: `SLAStatus` tests on `GetSEV`/`ListSEVs` — the
  most-strict target resolves correctly across two attached services, a still-open
  SEV shows `at_risk` before any target is finalized, a late-detected SEV shows
  `breached` once `MTTDSeconds` is computed, a SEV with no attached service gets no
  `sla_status` at all; plus MTTPC-specific: `MttpcSeconds` computed on the
  `PostmortemComplete` transition, and `sla_status.mttpc` correctly using
  `mitigated_at` as its baseline via `GetSEV`.
- `web/src/components/sev/badges.test.tsx` (new): `SLABadge` renders nothing for
  `ok`/`not_applicable`/undefined, an "at risk" badge, a "breached" badge, and
  defaults its label to "SLA".
- `web/src/pages/admin/AdminServicesPage.test.tsx`: opening the SLA editor and
  saving a target converts minutes to seconds correctly in the request body; every
  target column's info icon exposes the right metric definition via its `title`
  attribute, and an MTTPC target saves correctly.

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
