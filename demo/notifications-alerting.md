# Demo — Notifications & Alerting (Roadmap Phase 15)

## What was built

`docs/requirements.md` §16/§18.5 was the last major unimplemented section of the
original functional spec: no `NotificationConfig` RPC/service, no admin page, and no
email delivery path existed anywhere in the codebase — only a hard-coded Slack
substitute (status-change and `external`/`status-page` announcement pushes,
`cmd/slackbot/notify.go`). This phase builds the real thing: an admin-configurable
routing table across Slack and (new) email, a SEV-1-without-IC escalation
scanner, and — closing the exact gap Phase 12 and Phase 13 each explicitly
deferred pending "no notification layer exists yet" — an SLA risk scanner that
fires once when a SEV's overall SLA status becomes at-risk, and again if it's
later confirmed breached.

- **Schema**: migration `000020` extends the already-present-but-unused
  `notification_config` table (`role`, `event`, `channel_type`, `channel_target`)
  with a nullable `max_severity_level` (unset = every severity; a value expresses
  "SEV-1/SEV-2 opens only" as one row rather than a new event type per severity
  band); adds a new `escalation_config` table (per-severity-level threshold,
  pre-seeded disabled for SEV-1..4, mirroring `retention_config`'s seed
  precedent); and adds `sevs.escalated_at`, a marker so the escalation scanner
  notifies once per incident rather than every scan cycle. Migration `000021`
  adds `sevs.sla_notified_status` for the same reason, on the SLA risk scanner.
  **Follow-up, migration `000022`**: widens `notification_config` from one
  `event` column to an `events TEXT[]` column (`= ANY(...)` matching, same
  idiom `sevs.affected_services`/`service_slas.service_id` already use in
  this schema) so a single rule can cover several events — e.g. one Slack
  rule for both "SLA at risk" and "SLA breached" — instead of requiring a
  separate row per event. `(role, event, channel_type)` stopped being a
  usable natural key once `events` could hold more than one value (two rules
  can share a role and channel while differing only in which events they
  cover), so rules moved from that composite key to plain `id`-addressing —
  see the API bullet below.
- **Store**: `store.NotificationConfigStore` (`Create`/`Update`/`Delete`/
  `List`/`ListForEvent`, id-addressed as of the `000022` follow-up above) and
  `store.EscalationConfigStore` (`Get`/`Upsert`/`List`, unchanged), both
  with in-memory and Postgres implementations, following
  `ServiceLevelingCriteriaStore`'s/`AIPluginStore`'s exact shape. `SEVStore`
  gains two narrow mutators, `UpdateEscalatedAt` and `UpdateSLANotifiedStatus`,
  same shape as the existing `UpdateLocked`.
- **Dispatch**: `internal/api/grpc/notify.go`'s `Notifier` — best-effort, same
  contract as `auditAppendBestEffort`: looks up every `NotificationConfig` row
  matching an event (filtered by severity), builds a Slack or email client from
  the datastore's current integration config, and delivers. A missing/
  unconfigured integration, or a delivery error, is logged and skipped — it never
  fails the mutation the event is attached to. The rendered message includes
  the SEV's real case ID (e.g. `SEV-2026-0042`, not just its 1-4 severity
  level — an earlier version of this conflated the two), title, severity, and
  status, plus — once `cmd/slackbot` has created the incident channel for
  that SEV (§13.1) — a Slack channel mention (`<#CHANNELID>`, or a
  `slack.com/app_redirect` deep link for email). That channel reference is
  necessarily absent from the very first `sev.created` notification, since
  it fires from the same event that triggers channel creation in a separate
  process; it appears starting with the next event for that SEV
  (`sev.updated`/`sev.status_changed`/etc.) once the channel exists. Wired
  into `SEVServer`
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
- **SLA risk scanner**: `cmd/server`'s `startSLARiskScanner`/`scanSLARisk`,
  same ticker shape, but a wider status filter (includes Resolved and
  Postmortem In Progress, since RTPC is still live post-resolution). Batches
  the `ServiceSLAStore.ListForServices` lookup by severity level (≤4 round
  trips per scan, mirroring `report.go`'s `serviceLevelMetrics`), reduces
  each SEV's attached services via `sev.MostStrictSLA`, evaluates via
  `sev.EvaluateSLA`, and fires `sev.sla_at_risk` the first time a SEV's
  `Overall` reads `at_risk`, or `sev.sla_breached` the first time it reads
  `breached` — tracked via a new `sevs.sla_notified_status` column
  (migration `000021`) so neither event re-fires for the same SEV once
  notified at that level. "Breached" always wins the notify decision, so a
  SEV that jumps straight from on-track to breached still gets exactly one
  notification rather than a skipped one.
- **API + RBAC**: `ConfigService` gains `ListNotificationConfigs` and
  `UpsertEscalationConfig`/`ListEscalationConfigs`. Reads sit at the Viewer floor
  (matching `ServiceSLA`/`LevelingCriteria`); mutations are Admin-only.
  **Follow-up**: `UpsertNotificationConfig` (a single PUT keyed by
  role/event/channel_type) split into `CreateNotificationConfig` (POST) and
  `UpdateNotificationConfig` (PATCH `/v1/config/notifications/{id}`), and
  `DeleteNotificationConfig` moved from query-param identification
  (`?role=&event=&channel_type=`) to `DELETE /v1/config/notifications/{id}`
  — the same Create/Update/Delete-by-id shape `AIPlugin` already uses,
  adopted here because a multi-event rule can no longer identify itself by
  its field values. **Second follow-up**: `TestNotificationConfig` (POST
  `/v1/config/notifications/test`) sends one real test message per event in
  the request straight to its own `channel_type`/`channel_target`,
  completely bypassing `ListForEvent`'s event/severity matching — it takes
  the same fields as `CreateNotificationConfigRequest` (no `id`), so it
  works equally for an already-saved rule (pass its current values) or one
  still being drafted in the Add-rule form. `Notifier` gained a parallel
  `Test` method for this — `Notify`'s delivery path logs and swallows
  errors (best-effort, never blocks the mutation it's attached to); `Test`
  returns each event's error to the caller instead, since surfacing exactly
  why a channel isn't working is the entire point of a manual test button.
  Admin-only, like every other mutation here.
- **Admin UI**: a new `/admin/notifications` page/tab — a routing-rules table
  (add/delete/test; role, a multi-select checkbox group of events, channel
  type, target, optional max-severity) built on `AdminOnCallPage.tsx`'s
  create-form-plus-list pattern, and a 4-row escalation threshold table built
  directly on `AdminRetentionPage.tsx`'s structure. Each rule row renders its
  covered events as a wrapped list of chips rather than a single value.
  **Follow-up**: a **Send test** button, enabled once role/events/channel
  type/target are filled in, appears both in the Add-rule form (tests the
  draft without saving it) and on every already-saved rule's row; results
  render per event right below — "sent" or the actual delivery error — so
  a misconfigured channel or missing integration is obvious immediately
  instead of only surfacing whenever the next real SEV event happens to fire.

## Prerequisites

- `go build ./... && go vet ./... && go test ./...` and `web`'s `tsc -b && vitest
  run && oxlint` passing.
- The walkthrough below runs against the in-memory store (`DATABASE_URL` unset).
  A real Postgres was also used, out-of-band, to apply migrations `000020`,
  `000021`, and `000022` and run `go test -tags integration ./internal/store/...`
  against it — see Verify tests pass below for what that covered.

## Walkthrough

Live-verified against a real `cmd/server` process (in-memory store):

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 ./server &

TOKEN=$(curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}' | jq -r .token)
# The first registered user is Admin (existing bootstrap behavior).

# 1. add an admin → Slack rule covering *both* SLA events in one rule —
#    the whole point of the multi-event follow-up.
curl -s -X POST localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","events":["sev.sla_at_risk","sev.sla_breached"],"channel_type":"slack","channel_target":"#sla-alerts"}'
# {"id":"1","role":"admin","events":["sev.sla_at_risk","sev.sla_breached"],
#  "channel_type":"slack","channel_target":"#sla-alerts","created_at":"...","updated_at":"..."}

# 2. add an IC → Slack rule for every SEV creation.
curl -s -X POST localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"incident-commander","events":["sev.created"],"channel_type":"slack","channel_target":"#incidents"}'
# {"id":"2","role":"incident-commander","events":["sev.created"],"channel_type":"slack",
#  "channel_target":"#incidents","created_at":"...","updated_at":"..."}

# 3. list both rules.
curl -s localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN"
# {"configs":[{"id":"1","role":"admin","events":["sev.sla_at_risk","sev.sla_breached"],...},
#             {"id":"2","role":"incident-commander","events":["sev.created"],...}]}

# 4. update rule 1 to also cover sev.created — one rule, three events now,
#    same id, same channel_target, updated_at advances.
curl -s -X PATCH localhost:8080/v1/config/notifications/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","events":["sev.sla_at_risk","sev.sla_breached","sev.created"],"channel_type":"slack","channel_target":"#sla-alerts"}'
# {"id":"1","role":"admin","events":["sev.sla_at_risk","sev.sla_breached","sev.created"],
#  "channel_type":"slack","channel_target":"#sla-alerts","created_at":"...","updated_at":"..."}
#  — created_at unchanged, updated_at advances.

# 5. validation: an empty events list and a duplicate event within one list
#    are both rejected outright.
curl -s -w '\n%{http_code}\n' -X POST localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","channel_type":"slack","channel_target":"#x"}'
# {"code":3,"message":"events must contain at least one event type"}   / 400
curl -s -w '\n%{http_code}\n' -X POST localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","events":["sev.created","sev.created"],"channel_type":"slack","channel_target":"#x"}'
# {"code":3,"message":"duplicate event type: sev.created"}   / 400

# 6. delete rule 2, confirm it's gone, then double-delete -> not found.
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE localhost:8080/v1/config/notifications/2 \
  -H "Authorization: Bearer $TOKEN"                                                                     # 200
curl -s localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN"
# {"configs":[{"id":"1", ...}]}   — only rule 1 remains
curl -s -w '\n%{http_code}\n' -X DELETE localhost:8080/v1/config/notifications/2 -H "Authorization: Bearer $TOKEN"
# {"code":5,"message":"notification config not found"}   / 404

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
curl -s -w '\n%{http_code}\n' -X POST localhost:8080/v1/config/notifications -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","events":["sev.created"],"channel_type":"slack","channel_target":"#x"}'
# {"code":7,"message":"insufficient permissions for /sevitout.v1.ConfigService/CreateNotificationConfig"}   / 403

# 11. add an email rule (SEV-1/SEV-2 opens only), then create a SEV-1. It
#     matches, but no "email" integration is actually configured, so delivery
#     is skipped gracefully (logged, not an error) rather than failing the
#     request.
curl -s -X POST localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","events":["sev.created"],"channel_type":"email","channel_target":"mgmt@example.com","max_severity_level":2}'
curl -s -X POST localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"Checkout down","severity_level":1}'
# {"id":"SEV-2026-0001", "title":"Checkout down", "severity_level":1, "status":"open", ...}   — succeeds normally

# the server log shows the graceful skip:
# {"level":"ERROR","source":{"function":"...(*Notifier).deliverEmail"},
#  "msg":"notify: email integration unavailable","err":"store: not found"}

# 12. register a service with a strict SLA target and open a SEV already
#     backdated past that target — rule 1 from step 1/4 already covers
#     sev.sla_at_risk, so no new notification rule is needed here.
curl -s -X POST localhost:8080/v1/config/services -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"checkout","name":"Checkout"}'
curl -s -X PUT localhost:8080/v1/config/services/checkout/sla/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"severity_level":1,"mttd_target_seconds":60}'
# a 60-second MTTD target
STARTED=$(date -u -v-5M +"%Y-%m-%dT%H:%M:%SZ")   # 5 minutes ago
curl -s -X POST localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"title\":\"Checkout down\",\"severity_level\":1,\"affected_services\":[\"checkout\"],\"started_at\":\"$STARTED\"}"
# {"id":"SEV-2026-0002", ..., "sla_status":{"mttd":"at_risk", "overall":"at_risk",
#  "mttd_target_seconds":"60", ...}}   — already over target on read, per Phase 12

# within the next minute, the scanner picks it up and attempts delivery — no
# real Slack integration is configured, so it skips gracefully, same as step 11:
# {"level":"ERROR","source":{"function":"...(*Notifier).deliverSlack"},
#  "msg":"notify: slack integration unavailable","err":"store: not found"}

# 13. TestNotificationConfig: send a real test for a draft rule (never
#     saved) covering two events — no "slack" integration is configured, so
#     both fail with a clear reason instead of the request itself erroring.
curl -s -X POST localhost:8080/v1/config/notifications/test -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","events":["sev.sla_at_risk","sev.sla_breached"],"channel_type":"slack","channel_target":"#sla-alerts"}'
# {"results":[{"event":"sev.sla_at_risk","error":"slack integration unavailable: store: not found"},
#             {"event":"sev.sla_breached","error":"slack integration unavailable: store: not found"}]}

# now configure a "slack" integration (a real bot token would deliver for
# real; this one is intentionally invalid, to show a *delivery* failure
# rather than a *missing-integration* one) and test again.
curl -s -X PUT localhost:8080/v1/config/integrations/slack -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"credentials":{"bot_token":"xoxb-fake-invalid-token"}}'
curl -s -X POST localhost:8080/v1/config/notifications/test -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin","events":["sev.created"],"channel_type":"slack","channel_target":"#incidents"}'
# {"results":[{"event":"sev.created","error":"slack delivery failed: slack: post message to #incidents: invalid_auth"}]}
# — a real round trip to Slack's API, failing on the invalid token, exactly
# what an admin debugging a misconfigured rule needs to see.

# confirm nothing above was ever persisted as a rule (testing is read-only
# with respect to notification_config).
curl -s localhost:8080/v1/config/notifications -H "Authorization: Bearer $TOKEN"
# {}   — still empty; only rule 1 exists at this point in the walkthrough,
# and it isn't shown here since it was listed already in step 3/6.

# validation and RBAC match Create's.
curl -s -w '\n%{http_code}\n' -X POST localhost:8080/v1/config/notifications/test -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","channel_type":"slack","channel_target":"#x"}'
# {"code":3,"message":"events must contain at least one event type"}   / 400
curl -s -w '\n%{http_code}\n' -X POST localhost:8080/v1/config/notifications/test -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin","events":["sev.created"],"channel_type":"slack","channel_target":"#x"}'
# {"code":7,"message":"insufficient permissions for /sevitout.v1.ConfigService/TestNotificationConfig"}   / 403
```

Every response above was run against a live server, not hand-typed — including
step 12's SLA-risk scanner, which really did fire and log the graceful-skip line
shown (immediately, in this run, since rule 1 already existed before the SEV was
created — the scanner's own 1-minute tick, not rule lookup, is what's periodic),
and step 13's second `TestNotificationConfig` call, which really did reach
Slack's real `chat.postMessage` API over HTTPS and got back a real
`invalid_auth` response for the intentionally-bogus bot token — the one place
in this walkthrough that makes an actual external network call, since that's
the entire point of a manual test button. Delivering to a *real, working*
Slack workspace or SMTP server (a valid bot token, a real mailbox) isn't
demoed here (that would need real external credentials this walkthrough
doesn't have) — it's covered instead by
`internal/api/grpc/notify_test.go` (`TestNotifier_Notify_SlackDelivery`/
`_EmailDelivery`, against fake `SlackSender`/`EmailSender`s), `TestCreateSEV_NotifiesOnSevCreated`,
`TestCreateAnnouncement_Notifies`, `TestTransitionPostmortemStatus_NotifiesOnApproved`,
`cmd/server/sla_risk_scanner_test.go` (all asserting the right code path calls
`Notifier.Notify` with the right event type, against a real in-memory
`Notifier`/fake Slack sender), `Notifier.Test`'s own tests (`TestNotifier_Test_*`,
same file, a successful multi-event send against the fakes), and
`internal/integrations/email/client_test.go` (a real SMTP conversation against
a local fake server, exercising `Client.Send` end-to-end at the protocol level).

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
  `max_severity_level` filter (both sides), a rule covering several events
  matches on any of them and not on an event outside its list, a rule
  routed to an unconfigured integration skips gracefully, a nil-SEV event
  still matches an unfiltered rule, the rendered message includes the real
  SEV case ID (not just its severity level), the Slack channel mention
  appears when `SlackChannelID` is set and is omitted when it isn't, and the
  email body includes the equivalent Slack deep link when set. Also
  `Notifier.Test` — nil receiver/nil cfg/empty-events all no-op safely, one
  result per event delivered to cfg's own channel (ignoring `ListForEvent`
  and `MaxSeverityLevel` entirely — it works with zero saved rules, i.e. a
  never-persisted draft), and an unconfigured integration's error is
  returned to the caller rather than only logged.
- `internal/api/grpc/config_notification_test.go`: Create/Update/Delete/List
  for `NotificationConfig` (id-based, not the original composite-key
  Upsert/Delete) and Upsert/List for `EscalationConfig` — validation of
  role/events (non-empty, no unknown or duplicate event)/channel_type/
  severity/threshold, not-found mapping on update and delete, a regression
  test that `Create`'s response carries non-zero `created_at`/`updated_at`,
  and the pre-seeded 4-row default for escalation config. Also
  `TestNotificationConfig` — `Unavailable` with no `Notifier` wired, one
  result per event on success, an unconfigured integration reported as a
  failed (not erroring) result, works for a never-saved draft rule and
  confirms nothing gets persisted, and the same role/events/channel_target
  validation as `Create`.
- `internal/auth/rbac_test.go`: RBAC floor coverage for all seven
  `NotificationConfig`/`EscalationConfig` RPCs.
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
- `cmd/server/sla_risk_scanner_test.go`: `scanSLARisk` fires `sla_at_risk` and
  marks `SLANotifiedStatus` (no re-fire on a second scan); escalates a
  previously-notified `at_risk` SEV to `sla_breached` once its final value
  confirms it, and never re-fires past that; a SEV whose final value already
  exceeds target on read gets exactly one `sla_breached` notification, not an
  `sla_at_risk` one first; no notification with no `ServiceSLA` configured;
  doesn't panic with no candidate SEVs; and `startSLARiskScanner` stops
  cleanly on context cancellation.
- `internal/integrations/email/client_test.go`: a full SMTP conversation against
  a local fake server (successful send, a rejected `RCPT TO`, and a dial failure
  against a closed port).
- `internal/integrations/catalog/catalog_test.go`: the existing structural
  invariants (unique types/keys, credential fields are all `secret`, settings
  fields never are) still hold with the 6th ("email") entry added.
- `internal/store/memory/memory_test.go`: `TestNotificationConfigStore` —
  create/update (not-found included)/list/delete (not-found included), and
  `ListForEvent` matching a rule on either its severity filter or any event
  in its list, including the second event of a multi-event rule;
  `TestEscalationConfigStore` — unchanged shape, list/upsert/pre-seeded
  defaults; `TestSEVStore`'s new `UpdateEscalatedAt`/`UpdateSLANotifiedStatus`
  subtests (set, clear, not-found).
- `internal/store/postgres/notificationconfig_test.go` /
  `escalationconfig_test.go` (integration-tagged, run against real Postgres):
  the same coverage against the actual schema and generated sqlc queries,
  including the `events TEXT[]`/`ANY(...)` matching.
- `web/src/pages/admin/AdminNotificationsPage.test.tsx`: renders existing
  rules including a multi-event rule's full chip list, adds a rule covering
  multiple events (asserting the exact `events` array sent), disables **Add
  rule** when every event checkbox is unchecked, deletes a rule by id, and
  saves an escalation threshold row. Also **Send test**: sends a test for an
  already-saved rule and renders one "sent"/error line per event, shows the
  actual delivery error text on failure, sends a test straight from the
  draft Add-rule form without ever calling Create, and disables **Send
  test** in the draft form when no events are selected.

## Known limitations

- Routing is role/events → a fixed broadcast channel or address, **not**
  per-user or per-incident-assignee personal delivery — there is no `user_id`/
  `sev_id` on a `NotificationConfig` row, matching the table's original schema
  design. A DM to *this specific* SEV's actual assigned IC/on-call (rather than
  "whoever holds this org role, always") would reuse Phase 10's per-user Slack/
  email identity fields but is a materially different targeting model; scoped
  as a deferred follow-up, not built here.
- No edit-in-place in the **Admin → Notifications** UI — the
  `UpdateNotificationConfig` RPC and `api.config.notifications.update`
  client method both exist and are covered by tests/this walkthrough, but no
  button in the page calls them; it only offers Add and Delete. Changing an
  existing rule's events, target, or severity filter means deleting and
  re-adding it by hand today.
- No per-user notification preferences (e.g. opt in/out of email) — `ProfilePage.tsx`
  gains no such field in this phase.
- No in-app toast/badge delivery — the existing WebSocket hub is per-SEV-client
  scoped with no per-user filtering; Slack/email cover the requirement instead.
- Admin config mutations here (`CreateNotificationConfig`/`UpdateNotificationConfig`/
  their deletes, `UpsertEscalationConfig`) are **not audited** — consistent with
  every sibling `ConfigService` RPC (`config_*.go` never calls
  `auditAppendBestEffort`; that helper is SEV-scoped only), not a regression
  specific to this phase.
- Escalation only checks for a missing Incident Commander — no other role, and no
  configurable "which role must be present" beyond IC.
- `sev.sla_at_risk`/`sev.sla_breached` fire on the SEV's `Overall` SLA status
  only (worst of MTTD/MTTM/MTTR/RTPC), not per-metric — a rule can't route
  specifically on "MTTR at risk" versus "MTTD at risk." `SLANotifiedStatus`
  is also a one-way marker: an admin loosening a service's SLA target (or
  its affected-services list) after a SEV already read `at_risk` won't
  un-notify it or reset the marker.
- `ListEnabledIntegrations`-style blind spots aside, the Slack/email delivery
  clients are built fresh per notification from the datastore's current
  integration config (no caching) — matching every other per-call Slack-client
  construction in this codebase (`role.go`'s `InviteRoleToSlack`/
  `JoinSlackChannel`), not a new inefficiency introduced here.
- **Send test** is a real send, not a dry-run — it posts an actual message
  to the real Slack channel or mails the real address, visible to anyone
  else already in that channel/inbox; there's no simulate-only mode. It also
  completely bypasses `MaxSeverityLevel` and every other rule's
  `ListForEvent` matching, so a successful test only proves the channel and
  integration work — it does *not* prove a real SEV at some given severity
  would actually match this rule (or that some *other* higher-priority rule
  wouldn't shadow it; there's no such precedence today, every matching rule
  always fires). `TestNotificationConfig` calls are Admin-only, like every
  other mutation-adjacent RPC here, and are **not audited** either, for the
  same reason Create/Update/Delete aren't (§ above).
