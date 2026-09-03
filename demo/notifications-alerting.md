# Demo — Notifications & Alerting (Roadmap Phase 15)

## What was built

`docs/requirements.md` §16/§18.5 was the last major unimplemented section of the
original functional spec: no `NotificationConfig` RPC/service, no admin page, and no
email delivery path existed anywhere in the codebase — only a hard-coded Slack
substitute (status-change and `external`/`status-page` announcement pushes,
`cmd/slackbot/notify.go`). This phase builds the real thing: an admin-configurable
routing table across Slack and (new) email, plus a SEV-1-without-IC escalation
scanner — and, by existing, unblocks the "automated breach notifications" /
"automated compliance-drop alerts" Phases 12 and 13 each explicitly deferred
pending exactly this layer.

- **Schema**: migration `000020` extends the already-present-but-unused
  `notification_config` table (`role`, `event`, `channel_type`, `channel_target`)
  with a nullable `max_severity_level` (unset = every severity; a value expresses
  "SEV-1/SEV-2 opens only" as one row rather than a new event type per severity
  band); adds a new `escalation_config` table (per-severity-level threshold,
  pre-seeded disabled for SEV-1..4, mirroring `retention_config`'s seed
  precedent); and adds `sevs.escalated_at`, a marker so the escalation scanner
  notifies once per incident rather than every scan cycle.
- **Store**: `store.NotificationConfigStore` (`Upsert`/`Delete`/`List`/
  `ListForEvent`) and `store.EscalationConfigStore` (`Get`/`Upsert`/`List`), both
  with in-memory and Postgres implementations, following
  `ServiceLevelingCriteriaStore`'s exact shape. `SEVStore` gains a narrow
  `UpdateEscalatedAt` mutator, same pattern as the existing `UpdateLocked`.
- **Dispatch**: `internal/api/grpc/notify.go`'s `Notifier` — best-effort, same
  contract as `auditAppendBestEffort`: looks up every `NotificationConfig` row
  matching an event (filtered by severity), builds a Slack or email client from
  the datastore's current integration config, and delivers. A missing/
  unconfigured integration, or a delivery error, is logged and skipped — it never
  fails the mutation the event is attached to. Wired into `SEVServer`
  (`sev.created`, `sev.updated`, `sev.status_changed`, and `postmortem.due` on the
  move to Resolved), `AnnouncementServer` (`announcement.created`), and
  `PostmortemServer` (`postmortem.approved`) — right alongside each handler's
  existing WebSocket `publishProto` call, and gated by the same
  `if !record.Sensitive` check.
- **Email — greenfield**: `internal/integrations/email` (stdlib `net/smtp` +
  `crypto/tls` STARTTLS, no new dependency), added as the catalog's 6th
  integration (`internal/integrations/catalog`) — `smtp_username`/`smtp_password`
  as encrypted credentials, `smtp_host`/`smtp_port`/`from_address` as plain
  settings.
- **Escalation scanner**: `cmd/server`'s `startEscalationScanner`, a 1-minute
  ticker (same `ticker`/`select`/`ctx.Done()` shape as the existing
  `startMetricsRefresher`) that evaluates every open, non-sensitive SEV against
  its severity level's `EscalationConfig` via the new pure, table-tested
  `internal/sev.EvaluateEscalations`, fires `sev.escalation_no_ic` through the
  same `Notifier`, and marks `EscalatedAt`. `RoleServer.AssignRole` clears
  `EscalatedAt` back to nil when the newly-assigned role is Incident Commander.
- **API + RBAC**: `ConfigService` gains `UpsertNotificationConfig`/
  `DeleteNotificationConfig`/`ListNotificationConfigs` and
  `UpsertEscalationConfig`/`ListEscalationConfigs`. Reads sit at the Viewer floor
  (matching `ServiceSLA`/`LevelingCriteria`); mutations are Admin-only.
- **Admin UI**: a new `/admin/notifications` page/tab — a routing-rules table
  (add/delete; role, event, channel type, target, optional max-severity) built on
  `AdminOnCallPage.tsx`'s create-form-plus-list pattern, and a 4-row escalation
  threshold table built directly on `AdminRetentionPage.tsx`'s structure.

## Prerequisites

- `go build ./... && go vet ./... && go test ./...` and `web`'s `tsc -b && vitest
  run && oxlint` passing.
- The walkthrough below runs against the in-memory store (`DATABASE_URL` unset).
  A real Postgres was also used once, out-of-band, to apply migration `000020` and
  run `go test -tags integration ./internal/store/...` against it — see Verify
  tests pass below for what that covered.

## Walkthrough

Live-verified against a real `cmd/server` process (in-memory store):

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 ./server &

TOKEN=$(curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}' | jq -r .token)
# The first registered user is Admin (existing bootstrap behavior).

# 1. add an IC → Slack rule for every SEV creation.
curl -s -X PUT localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"incident-commander","event":"sev.created","channel_type":"slack","channel_target":"#incidents"}'
# {"role":"incident-commander","event":"sev.created","channel_type":"slack",
#  "channel_target":"#incidents","created_at":"...","updated_at":"..."}

# 2. add a management → email rule, SEV-1/SEV-2 opens only.
curl -s -X PUT localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","event":"sev.created","channel_type":"email","channel_target":"mgmt@example.com","max_severity_level":2}'

# 3. list both rules.
curl -s localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN"
# {"configs":[{"role":"incident-commander", ...}, {"role":"admin", ..., "max_severity_level":2}]}

# 4. delete the IC rule, confirm it's gone.
curl -s -o /dev/null -w '%{http_code}\n' \
  -X DELETE "localhost:8080/v1/config/notifications?role=incident-commander&event=sev.created&channel_type=slack" \
  -H "Authorization: Bearer $TOKEN"                                                                     # 200
curl -s localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN"
# {"configs":[{"role":"admin", ...}]}   — only the management rule remains

# 5. double-delete -> not found.
curl -s -w '\n%{http_code}\n' -X DELETE \
  "localhost:8080/v1/config/notifications?role=incident-commander&event=sev.created&channel_type=slack" \
  -H "Authorization: Bearer $TOKEN"
# {"code":5, "message":"notification config not found"}   / 404

# 6. validation: an unknown event type is rejected outright.
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","event":"sev.deleted","channel_type":"slack","channel_target":"#x"}'
# {"code":3, "message":"unknown event type"}   / 400

# 7. escalation config: all four severity levels pre-seeded, disabled.
curl -s localhost:8080/v1/config/escalation -H "Authorization: Bearer $TOKEN"
# {"configs":[{"severity_level":1,...}, {"severity_level":2,...}, {"severity_level":3,...}, {"severity_level":4,...}]}
# (threshold_minutes/enabled are omitted here — proto3 zero-value omission, same
# convention ServiceSLAResponse's target-seconds fields already use)

# 8. enable escalation for SEV-1 at a 30-minute threshold.
curl -s -X PUT localhost:8080/v1/config/escalation/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"severity_level":1,"threshold_minutes":30,"enabled":true}'
# {"severity_level":1,"threshold_minutes":30,"enabled":true,"updated_at":"..."}

# 9. the integration catalog now lists "email" as a 6th configurable integration.
curl -s localhost:8080/v1/config/integration-catalog -H "Authorization: Bearer $TOKEN" | jq -c '.integrations[].type'
# "pagerduty" "github" "slack" "jira" "email" "monitoring"

# 10. RBAC: any Viewer can read; only Admin can write.
VIEWER_TOKEN=$(curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","password":"correct-horse-battery-2","name":"Bob Viewer"}' | jq -r .token)
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/v1/config/notifications -H "Authorization: Bearer $VIEWER_TOKEN"  # 200
curl -s -w '\n%{http_code}\n' -X PUT localhost:8080/v1/config/notifications -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","event":"sev.created","channel_type":"slack","channel_target":"#x"}'
# {"code":7, "message":"insufficient permissions for /sevitout.v1.ConfigService/UpsertNotificationConfig"}   / 403

# 11. create a SEV-1 — matches the mgmt email rule (max_severity_level=2). No
#     "email" integration is actually configured, so delivery is skipped
#     gracefully (logged, not an error) rather than failing the request.
curl -s -X POST localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"Checkout down","severity_level":1}'
# {"id":"SEV-2026-0001", "title":"Checkout down", "severity_level":1, "status":"open", ...}   — succeeds normally

# the server log shows the graceful skip:
# {"level":"ERROR","msg":"notify: email integration unavailable","err":"store: not found", ...}
```

Every response above was run against a live server, not hand-typed. Actually
delivering to a real Slack workspace or SMTP server isn't demoed here (that would
need real external credentials and a live network call) — it's covered instead by
`internal/api/grpc/notify_test.go` (`TestNotifier_Notify_SlackDelivery`/
`_EmailDelivery`, against fake `SlackSender`/`EmailSender`s), `TestCreateSEV_NotifiesOnSevCreated`,
`TestCreateAnnouncement_Notifies`, and `TestTransitionPostmortemStatus_NotifiesOnApproved`
(all asserting the right handler calls `Notifier.Notify` with the right event
type), and `internal/integrations/email/client_test.go` (a real SMTP conversation
against a local fake server, exercising `Client.Send` end-to-end at the protocol
level).

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...
go test -tags integration ./internal/store/...   # needs DATABASE_URL + `make migrate`
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage:
- `internal/sev/escalation_test.go`: `EvaluateEscalations` — fires when over
  threshold with no IC; skips when an IC is assigned, when under threshold, when
  already escalated, when the severity level's config is disabled or missing, and
  when there's no `StartedAt` baseline yet.
- `internal/api/grpc/notify_test.go`: `Notifier.Notify` — nil-receiver no-op, no
  matching rule delivers nothing, Slack delivery, email delivery, the
  `max_severity_level` filter (both sides), a rule routed to an unconfigured
  integration skips gracefully, and a nil-SEV event still matches an unfiltered
  rule.
- `internal/api/grpc/config_notification_test.go`: Upsert/Delete/List for both
  `NotificationConfig` and `EscalationConfig` — validation of role/event/
  channel_type/severity/threshold, not-found mapping, and the pre-seeded 4-row
  default for escalation config.
- `internal/auth/rbac_test.go`: RBAC floor coverage for all five new RPCs.
- `internal/api/grpc/sev_test.go`, `announcement_test.go`, `postmortem_test.go`:
  the notifier fires with the right event type at each of the six call sites
  (`sev.created`, `sev.updated`, `sev.status_changed`, `postmortem.due`,
  `announcement.created`, `postmortem.approved`), and does not fire for a
  Sensitive SEV.
- `internal/api/grpc/role_test.go`: `AssignRole` clears `EscalatedAt` when the
  assigned role is Incident Commander, and leaves it untouched for any other role.
- `cmd/server/escalation_scanner_test.go`: `scanEscalations` fires and marks
  `EscalatedAt` (and doesn't re-fire on a second scan), skips a SEV with an IC
  already assigned, doesn't panic with no open SEVs, and `startEscalationScanner`
  stops cleanly on context cancellation.
- `internal/integrations/email/client_test.go`: a full SMTP conversation against
  a local fake server (successful send, a rejected `RCPT TO`, and a dial failure
  against a closed port).
- `internal/integrations/catalog/catalog_test.go`: the existing structural
  invariants (unique types/keys, credential fields are all `secret`, settings
  fields never are) still hold with the 6th ("email") entry added.
- `internal/store/memory/memory_test.go`: `TestNotificationConfigStore` /
  `TestEscalationConfigStore` — insert/update/list/delete, the `ListForEvent`
  severity filter, and the pre-seeded-disabled defaults.
- `internal/store/postgres/notificationconfig_test.go` /
  `escalationconfig_test.go` (integration-tagged, run against real Postgres):
  the same coverage against the actual schema and generated sqlc queries.
- `web/src/pages/admin/AdminNotificationsPage.test.tsx`: renders existing rules,
  adds a rule, deletes a rule, and saves an escalation threshold row.

## Known limitations

- Routing is role/event → a fixed broadcast channel or address, **not**
  per-user or per-incident-assignee personal delivery — there is no `user_id`/
  `sev_id` on a `NotificationConfig` row, matching the table's original schema
  design. A DM to *this specific* SEV's actual assigned IC/on-call (rather than
  "whoever holds this org role, always") would reuse Phase 10's per-user Slack/
  email identity fields but is a materially different targeting model; scoped
  as a deferred follow-up, not built here.
- No per-user notification preferences (e.g. opt in/out of email) — `ProfilePage.tsx`
  gains no such field in this phase.
- No in-app toast/badge delivery — the existing WebSocket hub is per-SEV-client
  scoped with no per-user filtering; Slack/email cover the requirement instead.
- Admin config mutations here (`UpsertNotificationConfig`/`UpsertEscalationConfig`/
  their deletes) are **not audited** — consistent with every sibling `ConfigService`
  RPC (`config_*.go` never calls `auditAppendBestEffort`; that helper is SEV-scoped
  only), not a regression specific to this phase.
- Escalation only checks for a missing Incident Commander — no other role, and no
  configurable "which role must be present" beyond IC.
- `ListEnabledIntegrations`-style blind spots aside, the Slack/email delivery
  clients are built fresh per notification from the datastore's current
  integration config (no caching) — matching every other per-call Slack-client
  construction in this codebase (`role.go`'s `InviteRoleToSlack`/
  `JoinSlackChannel`), not a new inefficiency introduced here.
