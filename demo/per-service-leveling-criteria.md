# Demo — Per-service SEV leveling criteria (Roadmap Phase 14)

## What was built

SEVs use the SEV-1 through SEV-4 taxonomy with generic, org-wide descriptions
(`docs/requirements.md` §3), but what those descriptions mean in practice varies by
service — a payments service's SEV-1 bar looks nothing like an internal admin
tool's. Phase 12/13 built SLA targets and compliance reporting that key off
whatever severity level a SEV is assigned, so picking the *right* level matters —
an incident leveled too low silently gets a looser SLA target than it should. This
phase adds per-service, per-severity-level free-text guidance for what qualifies as
each level, surfaced when opening a SEV and again on the postmortem page.
**This is guidance only and is never validated or enforced** — nothing blocks
opening or transitioning a SEV based on it.

- **Schema + store**: a new `service_leveling_criteria` table (migration `000019`),
  one row per `(service_id, severity_level)`, with a single `criteria TEXT NOT NULL`
  column — unlike `service_slas`'s nullable numeric targets, a row here only exists
  when there's guidance to show; "no criteria configured" is "no row," not an empty
  string. `store.ServiceLevelingCriteriaStore` follows the same shape as
  `ServiceSLAStore` (`Upsert`/`Get`/`Delete`/`ListByService` plus a
  `ListForServices` batch lookup), with in-memory and Postgres implementations.
  Deliberately a **separate table** from `service_slas` — free-text guidance
  authored by humans has a different lifecycle than numeric thresholds evaluated by
  `internal/sev/sla.go`, and the two shouldn't be coupled.
- **No domain/evaluation logic**: unlike `ServiceSLA` (which `EvaluateSLA`
  dereferences on every SEV read), nothing reads or evaluates this table — there's
  no `MostStrictSLA`-style reduction, no `SEVResponse` field, and it's not wired
  into `SEVServer` or `ReportServer` at all. Both frontend consumers call
  `ConfigService` directly.
- **API surface**: `ConfigService` gains `GetLevelingCriteria`/
  `UpsertLevelingCriteria`/`DeleteLevelingCriteria`/`ListLevelingCriteria` (per
  service, same URL shape as the SLA RPCs) plus `ListLevelingCriteriaForServices`
  (a top-level batch lookup across multiple services at one severity level, no
  reduction — silently omits any service with no configured row). Reads sit at the
  Viewer floor (matching `GetServiceSLA`/`ListServiceSLAs`), mutations at the Admin
  floor. `UpsertLevelingCriteria` rejects an empty/whitespace-only `criteria` with
  `InvalidArgument` — clearing existing guidance is `DeleteLevelingCriteria`'s job,
  not an empty upsert's.
- **Admin UI**: `AdminServicesPage.tsx` gets a second per-service action (a new
  "Leveling criteria" icon, independent of and alongside the existing "Manage SLAs"
  toggle — both panels can be open at once) that expands a 4-row table, one per
  severity level, each row a `Textarea` instead of `ServiceSLAEditor`'s numeric
  inputs. Built directly on `ServiceSLAEditor.tsx`'s structure
  (`LevelingCriteriaEditor.tsx`).
- **SEV creation form**: a new shared `LevelingCriteriaPanel` renders directly below
  the "Affected services" field on `SevCreatePage.tsx`, re-querying live as the
  reporter changes either the severity dropdown or the affected-services chips —
  showing each selected service's configured guidance for the currently-selected
  level, or a quiet "no criteria configured" note.
- **Postmortem page**: the same `LevelingCriteriaPanel`, wrapped in a read-only
  `Section title="Leveling criteria reference"` above the AI draft panel, using the
  SEV's own recorded severity level and affected services — a side-by-side
  reference for confirming the level chosen actually matched criteria, with no edit
  affordance anywhere in it.

## Prerequisites

- `go build ./... && go vet ./... && go test ./...` and `web`'s `tsc -b && vitest
  run && oxlint` passing.
- No database needed for the walkthrough below — the store and RBAC behavior are
  both visible against the in-memory store (`DATABASE_URL` unset).

## Walkthrough

Live-verified against a real `cmd/server` process (in-memory store):

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 ./server &

TOKEN=$(curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}' | jq -r .token)
# The first registered user is Admin (existing bootstrap behavior).

# 1. register two services and give checkout leveling guidance at SEV-1 and SEV-2.
curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"checkout","name":"Checkout"}'
curl -s -X PUT localhost:8080/v1/config/services/checkout/leveling-criteria/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"severity_level":1,"criteria":">50% of checkout traffic failing, or checkout fully unavailable"}'
# {
#   "service_id": "checkout", "severity_level": 1,
#   "criteria": ">50% of checkout traffic failing, or checkout fully unavailable",
#   "created_at": "...", "updated_at": "..."
# }
curl -s -X PUT localhost:8080/v1/config/services/checkout/leveling-criteria/2 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"severity_level":2,"criteria":"10-50% of checkout traffic failing"}'

curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"payments","name":"Payments"}'
# payments intentionally gets no leveling criteria configured.

# 2. payments has none configured — an empty object, not an error.
curl -s localhost:8080/v1/config/services/payments/leveling-criteria -H "Authorization: Bearer $TOKEN"
# {}

# 3. list checkout's criteria — ascending severity order.
curl -s localhost:8080/v1/config/services/checkout/leveling-criteria -H "Authorization: Bearer $TOKEN"
# {"criteria": [{"severity_level": 1, ...}, {"severity_level": 2, ...}]}

# 4. batch lookup across both services at severity 1 — the shape the SEV creation
#    form and postmortem page actually call. payments has no row at severity 1, so
#    it's silently omitted, not an error.
curl -s "localhost:8080/v1/config/leveling-criteria?service_ids=checkout&service_ids=payments&severity_level=1" \
  -H "Authorization: Bearer $TOKEN"
# {"criteria": [{"service_id": "checkout", "severity_level": 1, ...}]}

# 5. an empty/whitespace criteria submission is rejected outright — it does NOT
#    clear the row (that's what Delete is for).
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/services/checkout/leveling-criteria/3 \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"severity_level":3,"criteria":"   "}'
# {"code":3, "message":"criteria is required"}
# 400

# 6. delete actually clears a row.
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE localhost:8080/v1/config/services/checkout/leveling-criteria/2 \
  -H "Authorization: Bearer $TOKEN"                                                                # 200
curl -s -w '\n%{http_code}\n' localhost:8080/v1/config/services/checkout/leveling-criteria/2 -H "Authorization: Bearer $TOKEN"
# {"code":5, "message":"leveling criteria not configured for this service and severity level"}
# 404

# 7. RBAC: any Viewer can read; only Admin can write.
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/v1/config/services/checkout/leveling-criteria \
  -H "Authorization: Bearer $VIEWER_TOKEN"                                                          # 200
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/services/checkout/leveling-criteria/3 \
  -H "Authorization: Bearer $VIEWER_TOKEN" -H 'Content-Type: application/json' -d '{"severity_level":3,"criteria":"x"}'
# {"code":7, "message":"insufficient permissions for /sevitout.v1.ConfigService/UpsertLevelingCriteria"}
# 403

# 8. an unregistered service, or an out-of-range severity level, is rejected —
#    an orphaned criteria row could never be resolved by ListForServices.
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/services/does-not-exist/leveling-criteria/1 \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"severity_level":1,"criteria":"x"}'
# {"code":5, "message":"service not found"}   / 404
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/services/checkout/leveling-criteria/9 \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"severity_level":9,"criteria":"x"}'
# {"code":3, "message":"severity_level must be between 1 and 4"}   / 400
```

Every response above was run against a live server, not hand-typed — the exact
"empty criteria rejected, delete required to clear," "unconfigured service silently
omitted from the batch lookup," and RBAC behavior described in "What was built" is
what actually came back.

The admin editor, SEV creation form panel, and postmortem reference panel are
covered by `AdminServicesPage.test.tsx`'s new leveling-criteria tests,
`LevelingCriteriaPanel.test.tsx`, `SevCreatePage.test.tsx`'s refetch-on-change
test, and `PostmortemPage.test.tsx`'s read-only render test — see Verify tests
pass below.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage:
- `internal/store/memory/memory_test.go`: `TestServiceLevelingCriteriaStore` —
  insert, get, not-found, update-preserves-ID, per-service listing in ascending
  severity order, the batch `ListForServices` lookup (including the no-nil-elements
  invariant for an unconfigured service ID), delete, and delete-not-found.
- `internal/api/grpc/config_test.go`: `TestUpsertLevelingCriteria_{Valid,
  UnknownService,InvalidSeverityLevel,EmptyCriteriaRejected}`,
  `TestGetLevelingCriteria_NotFound`, `TestDeleteLevelingCriteria_{Valid,NotFound}`,
  `TestListLevelingCriteria_{ReturnsOnlyConfiguredSeverityLevels,MissingServiceID}`,
  `TestListLevelingCriteriaForServices_SkipsUnconfiguredServices`.
- `internal/auth/rbac_test.go`: RBAC floor coverage for all five new RPCs (Viewer
  reads, Admin-only writes, Incident Commander gets no more than Viewer).
- `web/src/pages/admin/AdminServicesPage.test.tsx`: opening the leveling-criteria
  editor and saving guidance text; the Clear button removing a saved row.
- `web/src/components/sev/LevelingCriteriaPanel.test.tsx` (new): renders nothing
  with no services selected (and makes no request); shows per-service guidance text
  when populated, with the correct `service_ids`/`severity_level` query params;
  shows the quiet empty-state note when nothing is configured.
- `web/src/pages/SevCreatePage.test.tsx`: the panel refetches when either the
  severity level or the affected-services selection changes.
- `web/src/pages/PostmortemPage.test.tsx`: the reference panel renders using the
  SEV's own severity/affected-services with no edit control anywhere in it; the
  section is omitted entirely when the SEV has no affected services.

## Known limitations

- An `AffectedServices` entry that doesn't resolve to a real `Service.ID` (loose
  free-text entry, `ServiceChipEditor.tsx`) is silently excluded from
  `ListLevelingCriteriaForServices`, same accepted gap as Phase 12/13's SLA and
  compliance-reporting work.
- This is never validated against the chosen severity, by design — a SEV can be
  opened, transitioned, and resolved at any level regardless of what the criteria
  panel shows. Enforcing or warning on a mismatch was explicitly considered and
  rejected for this phase (see `docs/roadmap.md` Phase 14's "Also considered and
  explicitly deferred").
- No versioning/history of criteria changes — `Upsert` is a destructive
  full-replace with no audit trail beyond the row's own `updated_at`.
- No org-wide default/fallback criteria for a service with nothing configured — the
  panel simply shows its "no criteria configured" note.
