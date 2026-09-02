# Demo — Per-service SLA compliance reporting (Roadmap Phase 13)

## What was built

Phase 12 added per-service SLA targets and a live, per-SEV breach indicator, but
explicitly deferred the aggregate view: *"SLA compliance reporting/analytics (e.g.
'% of SEVs within SLA this quarter') — out of scope; this phase is per-SEV live
status only, not an aggregate rollup"* (`docs/roadmap.md` Phase 12). This phase
closes that gap: for each service and severity level, over a selectable trailing
window (30/60/90/180 days), a report shows how many SEVs occurred, how many were
within SLA, and the average MTTD/MTTM/MTTR.

- **13a — UX mock, reviewed before backend work started.**
  `web/src/components/reports/ServiceSLAComplianceTable.tsx` was built first
  against a hardcoded fixture, wired into `ReportsPage.tsx`, and iterated on live
  in the running app across three rounds of feedback: a Service/Severity filter
  was added, then collapsed into dropdown popovers (`MultiSelectDropdown`, no
  Radix — a plain positioned `<div>`, matching `select.tsx`/`checkbox.tsx`/
  `dialog.tsx`/`tooltip.tsx`'s existing convention), then each popover gained a
  "Select all"/"Clear" pair. The approved mock's exact shape became the template
  for the backend contract below, per `docs/roadmap.md`'s own stated sequencing.
- **13b — Backend: proto + aggregation.** New RPC
  `ReportService.GetServiceMetrics` (`GET /v1/reports/service-metrics`) —
  `GetServiceMetricsRequest{window_days, service_ids}` →
  `ServiceMetricsResponse{service_level_metrics[], window_days}`.
  `internal/api/grpc/report.go`'s `serviceLevelMetrics` groups the window's SEVs
  by `(service, severity level)` — the same grouping `frequencyByServiceAndLevel`
  already uses — accumulating nil-safe MTTD/MTTM/MTTR averages (only SEVs with
  that metric already computed contribute, same discipline as `mttrTrends`) and a
  compliance breakdown via `sev.EvaluateSLA`'s `Overall` status per SEV.
  `buildSLALookup` batches the `ServiceSLAStore` reads to one call per distinct
  severity level present (at most 4), not one per SEV — an improvement on
  `sevToProtoWithSLA`'s already-accepted per-SEV tradeoff from Phase 12, not a
  repeat of it. An unrecognized `window_days` defaults to 30 rather than erroring.
- **13c — RBAC + wiring.** `GetServiceMetrics` → `OrgRoleViewer`, alongside the
  other read-only `ReportService` RPCs; `ReportServer` gains a `serviceSLAs`
  dependency, already constructed in `cmd/server/main.go` since Phase 12.
- **13d/13e — Frontend: real types + wiring.** `ServiceLevelMetrics`/
  `ServiceMetricsResponse` added to `types/api.ts`, `api.reports.serviceMetrics`
  added to `lib/api.ts`. `ServiceSLAComplianceTable.tsx` swapped its fixture
  `useState` for a `useQuery` — the **Service** filter now drives the real
  `service_ids` request field (checking a service narrows what's fetched, so it's
  part of the query key), while **Severity** stays a client-side filter over the
  fetched rows, since a window's response already contains every severity level
  in one call. The columns/empty-states approved in 13a carried over unchanged.

### Correction, mid-phase: two spots where the plan didn't match the code

Both surfaced during implementation/live-verification, not left silently
reconciled, per this project's usual practice:

- **`docs/roadmap.md`'s own 13b sketch was wrong about nil handling.** It
  described building each SEV's SLA row as
  `sev.MostStrictSLA([]*store.ServiceSLA{slaLookup(service, level)})`, claiming "a
  nil row is handled by `MostStrictSLA`'s existing nil-tolerant reduction."
  `internal/sev/sla.go`'s `MostStrictSLA` actually **dereferences every row in the
  slice it's given** — passing a nil element would panic, not degrade gracefully.
  The real implementation (`buildSLALookup`/`serviceLevelMetrics` in
  `report.go`) only appends a row to the slice when the lookup map actually has
  one for that `(service, level)` key, leaving the slice empty otherwise — which
  is what `MostStrictSLA`'s empty-slice case actually tolerates. `docs/roadmap.md`
  is corrected to match.
- **A wire-format gap, caught by live-verifying against a real server (see the
  walkthrough below), not just fixture data.** `cmd/server/main.go`'s grpc-gateway
  marshaler has no `EmitUnpopulated`, so protojson omits a proto3 scalar field
  from the JSON body entirely when it's the zero value — confirmed directly: a
  group with zero breached SEVs has no `"sla_breached_count"` key in the response
  at all, not a present `0`. `ServiceLevelMetrics`' TypeScript type originally
  marked every count and `compliance_pct` as required `number`, so a fully
  `not_applicable` group (real `sla_ok_count`/`sla_at_risk_count`/
  `sla_breached_count` all 0, hence all omitted) rendered **"NaN%"** instead of
  "No SLA configured" — `undefined + undefined + undefined` is `NaN`, not `0`, so
  the existing `measured === 0` check never matched. This is the exact same
  proto3-omission behavior `MTTRTrend.sample_size`/`average_mttr_seconds` already
  handle as optional fields elsewhere in `types/api.ts` — Phase 13's new type just
  hadn't followed that convention yet. Fixed by marking
  `sla_ok_count`/`sla_at_risk_count`/`sla_breached_count`/
  `sla_not_applicable_count`/`compliance_pct` optional and coalescing with `?? 0`
  in `ComplianceCell`, matching `MTTRTrendChart.tsx`'s existing
  `Number(t.average_mttr_seconds ?? 0)` pattern. A dedicated regression test
  (`ReportsPage.test.tsx`) mocks the real omitted-key JSON shape directly and
  asserts "No SLA configured" renders, not "NaN%" — verified to fail against the
  pre-fix code before confirming the fix passes it.

## Prerequisites

- `go build ./... && go vet ./... && gofmt -l . && go test ./...` and `web`'s
  `npx tsc -b && npx vitest run && npx oxlint` all passing.
- No database needed for the walkthrough below — the store, aggregation, and RBAC
  behavior are all visible against the in-memory store (`DATABASE_URL` unset).

## Walkthrough

Live-verified against a real `cmd/server` process (in-memory store) — every
response below is what actually came back, not hand-typed:

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 ./server &

curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}'
TOKEN=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1"}' | jq -r .token)
# The first registered user is Admin (existing bootstrap behavior).

# 1. Two services; only checkout gets an SLA target at SEV-1 (payments is left
#    deliberately unconfigured, to exercise sla_not_applicable below).
curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"checkout","name":"Checkout"}'
curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"payments","name":"Payments"}'
curl -s -X PUT localhost:8080/v1/config/services/checkout/sla/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"severity_level":1,"mttr_target_seconds":3600}'

# 2. Two checkout SEV-1s: one resolves in 10 minutes (under the 1h target — on
#    track), one takes 80 minutes (over target — breached). A third, on
#    payments, has no SLA configured at all.
#    (Each: create -> transition to mitigated -> transition to resolved, per
#    the SEV state machine — Open can't jump straight to Resolved.)
curl -s -X POST localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout errors — recovered fast","severity_level":1,"affected_services":["checkout"],"started_at":"<40 min ago>"}'
# ... transition to mitigated, then resolved 10 minutes after started_at:
# {"id":"SEV-2026-0001","mttr_seconds":"600","sla_status":{"mttr":"ok","overall":"ok","mttr_target_seconds":"3600",...}}

curl -s -X POST localhost:8080/v1/sevs ... -d '{"title":"Checkout errors — slow recovery","severity_level":1,"affected_services":["checkout"],"started_at":"<90 min ago>"}'
# ... resolved 80 minutes after started_at:
# {"id":"SEV-2026-0002","mttr_seconds":"4800","sla_status":{"mttr":"breached","overall":"breached","mttr_target_seconds":"3600",...}}

curl -s -X POST localhost:8080/v1/sevs ... -d '{"title":"Payments checkout drop","severity_level":1,"affected_services":["payments"],"started_at":"<5 min ago>"}'
# {"id":"SEV-2026-0003","sla_status":{"overall":"not_applicable",...}}   <- no SLA row for payments/SEV-1

# 3. GetServiceMetrics, default window (30 days, window_days omitted):
curl -s localhost:8080/v1/reports/service-metrics -H "Authorization: Bearer $TOKEN" | jq .
# {
#   "service_level_metrics": [
#     {
#       "service_id": "checkout", "severity_level": 1, "sev_count": 2,
#       "avg_mttm_seconds": "2640", "avg_mttr_seconds": "2700",
#       "sla_ok_count": 1, "sla_breached_count": 1, "compliance_pct": 0.5
#     },
#     { "service_id": "payments", "severity_level": 1, "sev_count": 1, "sla_not_applicable_count": 1 }
#   ],
#   "window_days": 30
# }
# Note what's *absent*: checkout has no "avg_mttd_seconds" key (neither SEV was
# ever marked detected — a nil-safe average, not a 0). payments has no
# "sla_ok_count"/"sla_at_risk_count"/"sla_breached_count"/"compliance_pct" keys
# at all — the exact zero-value-omission behavior the frontend fix above exists
# for. avg_mttm_seconds=2640 is real, not a placeholder: both SEVs were marked
# mitigated (540s and 4740s from their own started_at), and (540+4740)/2 = 2640.

# 4. Same window, filtered to one service (service_ids is a repeated query param):
curl -s "localhost:8080/v1/reports/service-metrics?service_ids=checkout" -H "Authorization: Bearer $TOKEN" | jq -c '.service_level_metrics[]'
# {"service_id":"checkout","severity_level":1,"sev_count":2,"avg_mttm_seconds":"2640","avg_mttr_seconds":"2700","sla_ok_count":1,"sla_breached_count":1,"compliance_pct":0.5}
# (payments dropped — not in the filter)

# 5. An unrecognized window_days degrades to the default rather than erroring:
curl -s "localhost:8080/v1/reports/service-metrics?window_days=45" -H "Authorization: Bearer $TOKEN" | jq -c '{window_days}'
# {"window_days":30}

# 6. RBAC: any Viewer can read this — same floor as every other ReportService RPC.
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/v1/reports/service-metrics -H "Authorization: Bearer $VIEWER_TOKEN"
# 200
```

The frontend side (window/service/severity filter interaction, the fetched-vs-
client-filtered distinction, and the omitted-field regression) is covered by
`ReportsPage.test.tsx`'s "SLA Compliance by Service" suite — see Verify tests
pass below.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage:
- `internal/api/grpc/report_test.go`: `serviceLevelMetrics` grouping across
  multiple services/severities; partial-metric SEVs (a SEV missing MTTM doesn't
  skew the group's MTTD/MTTR averages, and isn't pulled into the MTTM average
  either); a service+severity with no configured `ServiceSLA` row landing every
  SEV in `sla_not_applicable_count`; a mixed ok/breached group's
  `compliance_pct`; the window cutoff boundary (a SEV just outside `window_days`
  excluded); an unrecognized `window_days` defaulting to 30.
- `internal/auth/rbac_test.go`: `GetServiceMetrics` at the Viewer floor.
- `web/src/pages/ReportsPage.test.tsx` ("SLA Compliance by Service" suite):
  rendering fetched rows with resolved service names and formatted compliance;
  a window switch refetching with the new `window_days`; a service-filter
  selection refetching with `service_ids` set; a severity-filter selection
  narrowing rendered rows *without* a refetch; the omitted-zero-field wire shape
  rendering "No SLA configured" rather than "NaN%" (verified to fail against the
  pre-fix code, per the Correction section above).

## Known limitations

- **Compliance is a point-in-time snapshot over the selected window, not a
  historical trend.** A compliance-over-time chart (mirroring `mttrTrends`'s
  rolling-window shape) is a natural follow-up once this snapshot view is
  validated, not built here.
- **No CSV/JSON export of the aggregated rows.** `ExportSEVs` already covers
  raw per-SEV record export; a dedicated aggregate export can be added later if
  requested.
- **No per-user or per-team breach leaderboards.** This phase aggregates by
  `(service, severity)` only, matching `frequencyByServiceAndLevel`'s existing
  grouping.
- **No automated alerts when compliance drops below a threshold** — no
  notification layer exists yet, the same reasoning Phase 12 gave for deferring
  automated breach notifications.
- **An `affected_services` entry that doesn't resolve to a real `Service.ID`**
  (loose free-text entry, per `ServiceChipEditor.tsx`'s doc comment) is silently
  excluded from this report, the same accepted gap as Phase 12 — including from
  the Service filter itself, which only lists services present in the registry
  (`ConfigService.ListServices`).
