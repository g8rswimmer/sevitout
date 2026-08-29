# Demo — Structured monitoring-tool metadata (Roadmap Phase 6b)

## What was built

Before this change, a SEV's monitoring detection metadata was a mix of
enforced and unenforced free text: `detection_method` was already a closed,
server-validated vocabulary, but `monitoring_tool` was plain text (the
frontend offered a `<Select>` of named tools plus a free-text "Other" escape
hatch, with nothing validating it server-side), and the single `metric_link`
field conflated two different concepts — a dashboard URL and a saved
query/expression — into one column, despite `docs/requirements.md` §13.4
framing them as "a dashboard URL **or** saved query" from the start.

- **`monitoring_tool` is now a closed, server-validated enum** —
  `datadog`/`prometheus`/`cloudwatch`/`other` — the same pattern
  `detection_method` already used:
  - `store.MonitoringTool` + consts (`internal/store/models.go`), mirroring
    `store.DetectionMethod`.
  - `validateMonitoringTool` (`internal/api/grpc/sev.go`), called from both
    `CreateSEV` and `UpdateSEV`, rejecting any non-empty value outside the
    four consts with `codes.InvalidArgument`.
  - `"other"` is itself a valid, closed value — there's no companion
    free-text sub-field for it. A caller that wants to name a specific tool
    outside the list can still say so in `alert_name` or the description;
    that trade-off is called out under Known limitations below.
- **`metric_link` split into `dashboard_url` and `query`** — a URL and a
  saved query/expression string (e.g. PromQL or a Datadog query) are
  genuinely different shapes of data, so they now live in separate columns
  instead of one overloaded link field:
  - DB: `migrations/000011_monitoring_tool_structured.up.sql` renames the
    `metric_link` column to `dashboard_url` (an `ALTER TABLE ... RENAME
    COLUMN`, not a drop+add, so existing links carry forward) and adds a new
    nullable `query TEXT` column.
  - `store.SEV.MetricLink` → `store.SEV.DashboardURL`, plus a new
    `store.SEV.Query` field.
  - `proto/sevitout/v1/sev.proto`: `metric_link` renamed to `dashboard_url`
    on `SEVResponse`/`CreateSEVRequest`/`UpdateSEVRequest`, plus a new
    `query` field on all three.
  - sqlc source (`internal/store/sql/sevs.sql`) and the hand-rolled
    `internal/store/postgres/sev.go` list/filter path (`sevSelectCols`,
    `scanSEVRow`) both updated in lockstep — the sqlc-generated
    `Insert`/`Get`/`Update` path and the hand-written `List` path read the
    same columns, so both needed the rename+addition (see
    `internal/store/postgres/sev.go`'s doc comments for why `List` doesn't go
    through sqlc).
- **Frontend**: `web/src/types/api.ts`'s `MonitoringTool` type gained
  `'other'` and is now the actual type of `SEVResponse.monitoring_tool` (was
  a bare `string`, matching the backend now enforcing it).
  `DetectionFields.tsx`'s monitoring-tool `<Select>` dropped the "Other…"
  free-text companion input — the four options are now the complete set. A
  new "Saved query" text input sits alongside the renamed "Dashboard link"
  field, with its own preview-independent state.

## Design notes

**A rename, not a drop-and-recreate**, for `metric_link` → `dashboard_url` —
existing dashboard links already in the column carry forward automatically;
only the down migration needs to reverse the rename (plus drop the new
`query` column) to restore the pre-Phase-6b shape exactly.

**No DB-level `CHECK` constraint on `monitoring_tool`**, matching
`detection_method`'s existing precedent (only `status` has one — see
`migrations/000002_schema.up.sql`). Validation stays a single application-side
function (`validateMonitoringTool`), not duplicated at the DB layer; this also
avoids a migration-time data problem — rows written before this phase may
hold monitoring-tool text that doesn't match the new closed set (see Known
limitations), and a `CHECK` would need those normalized or the constraint
would fail to apply.

**Dropping the free-text "Other" input was a deliberate scope call**, not an
oversight — closing `monitoring_tool` to a true enum and keeping a
free-text escape hatch for the "other" bucket are in tension: keeping one
would mean either a second column just for the custom label, or the "closed
enum" claim being only partially true. The roadmap phase explicitly asked for
a `datadog`/`prometheus`/`cloudwatch`/`other` enum, not an
enum-plus-free-text hybrid, so `"other"` now stands alone as a value with no
attached label field.

## Prerequisites

- `go build ./... && go test ./...` and `web`'s `tsc -b && vitest run`
  passing.
- A running Postgres with the migrations applied through `000011` (`make
  migrate`, or `docker compose ... run --rm migrate`).

## Walkthrough

Live-verified end-to-end against a real Postgres instance (`make up` /
`make migrate`, then the API server pointed at it) rather than just unit
tests, since this phase touches the DB schema:

```bash
# apply migration 000011
docker compose ... run --rm migrate
# 11/u monitoring_tool_structured

# create a SEV with the new fields
curl -s -X POST localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
    "title": "Checkout errors",
    "severity_level": 3,
    "detection_method": "monitoring-dashboard",
    "monitoring_tool": "datadog",
    "alert_url": "https://pagerduty.example.com/incidents/1",
    "dashboard_url": "https://app.datadoghq.com/dashboard/abc",
    "query": "sum:trace.express.request.errors{service:checkout}",
    "snapshot_url": "https://img.example.com/snapshot.png"
  }'
# {"id":"SEV-2026-0049", ..., "monitoring_tool":"datadog",
#  "dashboard_url":"https://app.datadoghq.com/dashboard/abc",
#  "query":"sum:trace.express.request.errors{service:checkout}", ...}

# GetSEV round-trips the same fields back
curl -s localhost:8080/v1/sevs/SEV-2026-0049 -H "Authorization: Bearer $TOKEN"

# an unknown monitoring_tool is rejected server-side
curl -s -w '\nHTTP %{http_code}\n' -X POST localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"bad tool","severity_level":2,"monitoring_tool":"new-relic"}'
# {"code":3,"message":"unknown monitoring_tool"}
# HTTP 400
```

The down migration was also exercised directly against the live DB
(`migrate ... down 1`) to confirm `dashboard_url` renames back to
`metric_link` and `query` drops cleanly, then re-applied to leave the dev
database on the new schema.

## Verify tests pass

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./...
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage: `internal/api/grpc/sev_test.go`
(`TestCreateSEV_UnknownMonitoringTool_Rejected`,
`TestUpdateSEV_UnknownMonitoringTool_Rejected`, plus the existing detection-
metadata round-trip tests updated for `dashboard_url`/`query`);
`web/src/pages/SevCreatePage.test.tsx` and `SevDetailPage.test.tsx` updated
for the renamed/added fields and the dropped free-text "Other" input.
Backend aggregate coverage after this change: 79.6% (gate: 78.0%, see
`demo/test-coverage-ci-gate.md`).

## Known limitations

- **Pre-existing free-text `monitoring_tool` values are not
  migrated/normalized.** A SEV written before this phase with, say,
  `monitoring_tool = "New Relic"` (typed into the old "Other" input) keeps
  that raw string in the DB — `GetSEV`/`ListSEVs` return it as-is (reads
  aren't validated, only writes are), but the frontend's
  `MONITORING_TOOL_LABELS` lookup won't recognize it and any *new* write to
  that SEV must pick one of the four closed values or leave the field
  unchanged. No migration script back-fills these to `"other"` — see the
  Design notes above for why a DB `CHECK` constraint was deliberately not
  added, which is what would have forced the question.
- **No structured chart-embed / live health-check** — out of scope per the
  roadmap phase and `docs/requirements.md` §13.4's own "Future" marker on
  chart-snapshot embedding.
- **The frontend's "Other" custom tool name is gone.** Previously a user
  could type any tool name into "Other…"; now the choice is limited to the
  four enum values. This is the direct consequence of `monitoring_tool`
  becoming a real closed enum (see Design notes) — flagged here since it's a
  visible behavior change for anyone who relied on the old free-text path.
