# Demo — Integration-aware SEV UI (Roadmap Phase 11)

## What was built

The SEV detail page showed every integration-tied action unconditionally:
`TasksPanel.tsx` always offered "Create GitHub issue" and "Create Jira issue"
even when only one (or neither) tracker was configured, and there was no way
for a non-Admin to even ask "is X configured" — every existing status surface
(`GetIntegrationConfig`, `ListIntegrationConfigs`, `/admin/integrations/health`)
is Admin-gated. Separately, Phase 10's Slack-invite work covered assigned
roles but never gave a viewer of the SEV a way to join its incident channel
themselves. This phase closes those gaps. (11d — inviting the SEV creator —
shipped early; see `demo/integration-user-profiles.md`'s "10h" section.)

**11a. Backend: viewer-safe "enabled integrations" signal**

- New RPC `ConfigService.ListEnabledIntegrations` → `GET
  /v1/config/enabled-integrations` (a sibling path, not nested under
  `/v1/config/integrations/{type}`, for the same collision reason
  `GetIntegrationCatalog` already documents), RBAC floor `Viewer` — the one
  integration-config RPC below the Admin floor every sibling RPC in this
  group sits at, since it returns only a list of type strings, nothing an
  Admin-only endpoint wouldn't already consider safe to hand to any Viewer.
- `internal/api/grpc/config_integration.go`'s `integrationConfigured(cfg)`
  reuses the "configured" concept Phase 9's admin UI already applies: a
  non-empty encrypted-credentials blob, or — for a settings-only
  integration_type like Monitoring, which has no credential fields at all —
  a non-empty settings map.
- **Known limitation, stated not solved**: this reflects store-configured
  integrations only. PagerDuty/GitHub/Jira can also activate via their
  static env-var fallback with zero store rows (`cmd/server`'s `*Resolver`
  types) — such a setup reports as "not enabled" here and hides its
  SEV-page action even though the backend would actually serve the request.

**11b. Frontend: gate SEV-page integration actions by enabled status**

- New shared hook `web/src/lib/useEnabledIntegrations.ts` — one React Query
  query (`['config', 'enabled-integrations']`), so every panel on a page
  shares one request/cache entry.
- `TasksPanel.tsx`: "Create GitHub issue" renders only when `github` is
  enabled, "Create Jira issue" only when `jira` is enabled. If only Jira is
  configured, GitHub's option simply isn't offered; if neither is, only
  "Link existing" (which needs no integration) remains.
- `RolesPanel.tsx`: the per-role "Add to chat" button (Phase 10e) and this
  phase's "Join Slack channel" button both render only when `slack` is
  enabled **and** `SEV.slack_channel_id` is set — previously "Add to chat"
  only checked the channel.

**11c. Backend + frontend: self-service "Join Slack channel"**

- New RPC `RoleService.JoinSlackChannel(sev_id)` → `POST
  /v1/sevs/{sev_id}/join-slack-channel`, RBAC floor `Viewer`. Unlike Phase
  10e's `InviteRoleToSlack` (which invites a *named role holder* and needs
  no identity resolution for the caller), this resolves the *caller's own*
  identity: stored `User.SlackUserID` → `LookupUserIDByEmail` against the
  caller's own email → `codes.FailedPrecondition` ("no Slack identity on
  file — set one in your profile") if neither resolves. Also
  `FailedPrecondition` when the SEV has no recorded Slack channel, and
  `codes.Unavailable` when Slack invite support isn't wired up at all
  (mirroring `InviteRoleToSlack`'s posture).
- **Security gate not present in Phase 10's design**: before resolving or
  inviting, the handler checks the caller has full (non
  visibility-restricted) access to the SEV via the same
  `sensitiveSEVVisible` check `loadVisibleSEV` uses elsewhere —
  `codes.PermissionDenied` when the caller lacks access to a Sensitive SEV.
  This is deliberately a `PermissionDenied`, not `loadVisibleSEV`'s usual
  existence-masking `NotFound` — the caller already knows the SEV exists
  (they're looking at its detail page), so there's nothing left to mask.
  Self-service Slack join must not become a side-channel around
  sensitive-SEV restrictions, since Slack channel membership itself isn't
  gated by Sevitout RBAC once granted.
- `RolesPanel.tsx` gained a "Join Slack channel" button in the Roles
  section's header (next to the section title, visually adjacent to the
  per-role "Add to chat" buttons below it), independent of `canManage` — any
  Viewer with real access to the SEV may click it.

## Design notes

**RBAC is per-RPC, not per-service.** `ListEnabledIntegrations` sits at
`Viewer` on `ConfigService` even though every other RPC on that service is
`Admin`-only, the same way `ChatService.ListChatEntries` already sits at
`Viewer` alongside `ChatService`'s higher-gated `AddChatEntry`.
`JoinSlackChannel` sits at `Viewer` on `RoleService` even though
`AssignRole`/`RemoveRole` need Incident Commander and `InviteRoleToSlack`
needs Responder — it's a self-service "add me" action, not role management
or even an invite of someone *else*.

**Why `JoinSlackChannel` needed its own visibility check and
`InviteRoleToSlack` didn't**: `InviteRoleToSlack` sits behind the Responder
RBAC floor, and only a Responder+ who can already see the SEV (via
`ListRoles`'s own `loadVisibleSEV` check, which they'd have had to pass to
even find the role ID to invite) can reach it. `JoinSlackChannel` sits at the
much lower Viewer floor specifically so *any* authenticated user can
self-serve — which means the visibility check has to be explicit in the
handler itself rather than inherited from a higher RBAC gate or a prior call.

## Prerequisites

- `go build ./... && go test ./...` and `npm --prefix web run test` passing
- A running server + web app (`make up`)
- A configured "slack" integration (`demo/datastore-slack-bot-credentials.md`)
  and `cmd/slackbot` running, for the Slack-tied walkthrough steps
- A Sevitout user with a Slack identity set on their profile
  (`demo/integration-user-profiles.md`)

## Walkthrough

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

# Configure only Jira (no GitHub, no Slack) as an Admin.
curl -s -X PUT http://localhost:8080/v1/config/integrations/jira \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"credentials":{"api_token":"..."},"settings":{"cloud_id":"..."}}'

# Any authenticated Viewer can now ask which integrations are enabled.
curl -s http://localhost:8080/v1/config/enabled-integrations -H "Authorization: Bearer $TOKEN"
# {"enabled_types":["jira"]}
```

**In the web app:**

1. With only Jira configured (as above), open any SEV's **Linked tasks**
   panel: **Create Jira issue** is offered, **Create GitHub issue** is not.
2. Configure Slack too (`demo/datastore-slack-bot-credentials.md`) and create
   a new SEV from the web UI — `cmd/slackbot` creates its incident channel as
   usual. Open the SEV's **Roles** section: a **Join Slack channel** button
   appears in the section header.
3. Log in as a *different* user who holds no role on that SEV, with a Slack
   identity set on their own profile, and open the same SEV — **Join Slack
   channel** is visible and enabled for them too (it's self-service, not
   gated by `canManage`). Clicking it invites them into the channel directly.
4. Unconfigure Slack (or use a SEV created before Slack was ever configured,
   so `slack_channel_id` is unset) — both **Join Slack channel** and every
   per-role **Add to chat** button disappear entirely.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./...
npm --prefix web run build && npm --prefix web run test
```

New coverage:
- `internal/auth/rbac_test.go`: `ListEnabledIntegrations` (Viewer) and
  `JoinSlackChannel` (Viewer) RBAC floors.
- `internal/api/grpc/config_test.go`:
  `TestListEnabledIntegrations_CredentialAndSettingsOnlyTypes` (credential
  vs. settings-only vs. unconfigured filtering) and
  `TestListEnabledIntegrations_NoneConfigured`.
- `internal/api/grpc/role_test.go`: ten `TestJoinSlackChannel_*` cases —
  stored-identity resolution, email-lookup fallback, no-resolvable-identity,
  no-channel, not-configured, SEV-not-found, unauthenticated, and both the
  Sensitive-SEV-`PermissionDenied` and granted-access/Incident-Commander-
  bypass paths.
- `web/src/components/sev/TasksPanel.test.tsx`: create-issue button gating
  across Jira-only, GitHub-only, neither, and both-enabled combinations.
- `web/src/components/sev/RolesPanel.test.tsx`: "Join Slack channel"
  visibility across `canManage`/`slackEnabled`/`slackChannelId` combinations,
  plus its success and server-error states.

## Known limitations

- **`ListEnabledIntegrations` reflects store-configured integrations only**
  — an integration active solely via its static env-var fallback (zero store
  rows) reports as not enabled, hiding an action the backend would actually
  serve. See 11a's design notes above.
- **SEVs opened via `/sev open` still don't attribute `created_by` to the
  human opener** (a Phase 10h/11d limitation, restated here since 11d is
  part of this phase) — the creator-invite resolves to the slackbot's own
  service account for that path, a harmless no-op, not a duplicate of the
  human who's separately invited via the existing pending-opener path.
- **No retroactive backfill**: a SEV created before Slack was configured, or
  before `slack_channel_id` started being recorded (Phase 10e), has no join
  target — both "Add to chat" and "Join Slack channel" simply don't render
  for it, same as Phase 10e's existing "Add to chat" limitation.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 11) and
[`docs/architecture-evolution.md`](../docs/architecture-evolution.md) for the
fuller design rationale.
