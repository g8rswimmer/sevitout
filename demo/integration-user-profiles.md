# Demo — Per-User Integration Profiles (Roadmap Phase 10)

## What was built

Today nothing links a Sevitout user to their Slack account, GitHub username, or
Jira account beyond an email-address coincidence: Slack auto-invite only covered
the **on-call** role, resolved by regex-scraping an email out of
`SEVRole.DisplayName` — a format that happens to hold only because PagerDuty's
auto-assign writes it that way, and silently fails for a manually-typed name or
any other role type. GitHub/Jira issues created from a SEV never got an assignee
at all. This phase adds a self-service "integration identity" (Slack user ID,
GitHub username, Jira account ID) that each user manages for themselves, and uses
it to widen Slack auto-invite to every assigned role, add a manual "add to chat"
action, and default new tracker issues to the creating user.

**10a. Backend: integration-identity data model + self-service API**

- `internal/store.User` gains `SlackUserID`/`GitHubUsername`/`JiraAccountID`
  (all nullable, no uniqueness constraint — a stale/duplicate value just
  resolves to the wrong/no invite or assignee, not an integrity risk).
  Migration `migrations/000012_integration_identities.up/down.sql`.
- `UserStore` gains a **dedicated** `UpdateIntegrationIdentities` method rather
  than widening the existing `Update` (which already deliberately excludes
  `Email`/`PasswordHash`) — mirrors how `ConfigService` already prefers narrow
  methods (`UpdateUserRole`, `Deactivate/ReactivateUser`) over one generic
  mutator.
- `AuthService.UpdateMyIntegrationIdentities` (`PATCH /v1/auth/me`) — a
  write-side sibling of `WhoAmI`, acting purely on the caller via
  `auth.UserFromContext(ctx)`. **Full-replace semantics**: all three fields
  are sent on every call, and an empty string clears that field — a
  deliberate deviation from `UpdateSEV`'s sparse-patch convention, since this
  endpoint only ever touches these three fields and "empty = leave alone"
  would make clearing one impossible. `WhoAmIResponse` gains the three fields
  so the profile page pre-fills off the same call it already makes today.
  RBAC floor: `Viewer` (same as `WhoAmI`).
- `AuthService.ListUserDirectory` (`GET /v1/auth/directory`) — a minimal
  `{id, name, email, slack_user_id}` lookup, `Viewer` floor, deliberately
  narrower than the Admin-gated `ConfigService.ListUsers`. Supports a `query`
  substring filter (the role-picker's search box) and a batch `ids` filter
  (Slack auto-invite's one-call resolve of every assigned role's user).

**10b. Frontend: My Profile page**

- `web/src/pages/ProfilePage.tsx`, route `/profile`, linked from a new
  `UserCog` icon button in `AppLayout.tsx`'s header (next to Logout).
  Read-only Name/Email, three editable inputs with help text, Save calling
  `UpdateMyIntegrationIdentities`, inline success/error feedback.
  `AuthContext` gains `refreshUser()` so other open components (e.g.
  `TasksPanel`'s assignee pre-fill) see a saved change without a reload.
  Out of scope: name/avatar/password editing.

**10c. `RolesPanel.tsx`: user picker**

- The assign form gains an optional user-search combobox (built on
  `ListUserDirectory`'s `query` filter) alongside the existing free-text
  `display_name` input. Picking a real user sets `user_id` on the
  `AssignRole` call (the field already existed end-to-end) and pre-fills the
  display name; the free-text-only path stays fully supported.

**10d. Slack: widen SEV-creation auto-invite to every assigned role**

- `cmd/slackbot/channel.go`: `inviteOnCall` → `inviteRoleHolders`, dropping
  the on-call-only role-type filter. Resolution order per role: (1) a stored
  `SEVRole.UserID` batch-resolved via one `ListUserDirectory` call → a stored
  `SlackUserID` used directly; (2) that user has no stored Slack ID →
  `LookupUserIDByEmail` against their directory-listed email; (3) no
  `UserID` at all (an older or free-text-only assignment) → the original
  `<email@example.com>`-in-`DisplayName` regex scrape; (4) no match → skipped
  silently, same as today.

**10e. Slack: manual "add to chat" action**

- `sevs.slack_channel_id` (migration `000013`) persists the SEV → Slack
  channel mapping server-side, closing the in-memory-only limitation noted in
  `demo/M11-slack-bot.md`: `cmd/slackbot`'s `createIncidentChannel` now calls
  `UpdateSEV{slack_channel_id: ...}` right after creating the channel. Older
  SEVs (created before this shipped) simply have no value here.
- `RoleService.InviteRoleToSlack` (`POST
  /v1/sevs/{sev_id}/roles/{id}/invite-to-slack`), RBAC floor `Responder`
  (an auxiliary invite action, not role *management*, so lower than
  `AssignRole`/`RemoveRole`'s IC floor). Reuses 10d's identity-resolution
  order server-side, building its own `slack.Client` from the config-store's
  decrypted `bot_token` — no round trip through the live bot process.
  `FailedPrecondition` when the SEV has no channel recorded, or the role
  holder has no resolvable Slack identity; `Unavailable` when Slack isn't
  configured at all.
- `RolesPanel.tsx` gets a per-role-row "Add to chat" icon button, disabled
  (not hidden) when the SEV has no `slack_channel_id`.

**10f. GitHub/Jira: default assignee on issue creation**

- `CreateGitHubIssueRequest` gains `assignee` (a GitHub login, wrapped into
  GitHub's `assignees: []string`); `CreateJiraIssueRequest` gains
  `assignee_account_id` (sent as Jira's `assignee: {accountId: "..."}`).
  `TaskResponse` gains `assignee`, persisted on the linked task
  (`sev_linked_tasks.assignee`, migration `000014`) so the list can show it
  without a live re-fetch.
- **`assignee_name` (follow-up fix)**: the *linked-tasks list* originally
  showed `assignee`'s raw tracker-native value (a GitHub login or opaque
  Jira account ID) even for a task assigned via the picker — reported
  immediately after the picker shipped. `CreateGitHubIssueRequest` and
  `CreateJiraIssueRequest` now also accept `assignee_user_id` (the picked
  user's Sevitout ID, sent by `AssigneePicker.tsx` alongside the
  tracker-native value it already sent); `TaskServer` resolves it
  server-side to that user's current display name and stores it as
  `sev_linked_tasks.assignee_name` (migration `000015`) — a snapshot, not a
  live lookup, the same trade-off already accepted for
  `SEVRoleResponse.display_name`. `TaskResponse.assignee_name` is preferred
  over `assignee` everywhere it's displayed, falling back to the raw value
  only for a task whose assignee wasn't picked from Sevitout's own
  directory (e.g. a raw API call). A lookup failure at creation time is
  non-fatal — the tracker issue has already been created for real by that
  point, so a name-resolution problem never blocks the create.
- `TasksPanel.tsx`: both create-issue forms gain an "Assignee" field, backed
  by a new `AssigneePicker.tsx` — a searchable dropdown of directory users
  who have the relevant identity set (`ListUserDirectory`, filtered
  client-side to non-empty `github_username`/`jira_account_id`), showing
  each candidate's **name**, never the raw GitHub login or opaque Jira
  account ID. Picking one submits their tracker-native identifier but
  displays their name in its place; the field pre-fills the same way (the
  caller's own name, from `WhoAmIResponse` via `useAuth()`) the first time
  that mode is opened, clearable, omitted from the request when empty.
  `DirectoryUser` gains `github_username`/`jira_account_id` to support this
  (not sensitive — the same trust level as the `slack_user_id` it already
  exposed for Phase 10d's Slack auto-invite).

**10g. `AdminUsersPage.tsx`**

- Read-only "Integrations" column — a small badge per identity the user has
  configured for themselves (`Slack`/`GitHub`/`Jira`), no edit control (each
  user manages their own via `/profile`).

**10h. Slack: invite the SEV creator when the channel is created (pulled
forward from Roadmap Phase 11d)**

Reported after the rest of this phase shipped: a user with a Slack identity
set on their profile, who opens a SEV from the web UI but isn't assigned any
role on it, was never invited to its incident channel — `inviteRoleHolders`
only ever looked at role assignments, and nothing invited the creator
specifically. This is exactly the gap `docs/roadmap.md`'s Phase 11d already
named ("invite the SEV creator when the channel is created"); it's pulled
forward here as a scoped fix rather than left open, since it depends on
nothing from the rest of Phase 11 (11a-c's UI-gating, 11e's self-service
join) and directly extends 10d's just-shipped resolution logic.

- `cmd/slackbot/notify.go`'s `sevPayload` gains `CreatedBy string
  \`json:"created_by"\`` — previously silently dropped by `json.Unmarshal`
  since the field wasn't declared, even though a real `sev.created` payload
  always carries it (protojson from `SEVResponse.created_by`).
- `cmd/slackbot/channel.go`: `inviteRoleHolders` is split into
  `resolveRoleHolderSlackIDs` (resolution only, no invite call) and a new
  `resolveCreatorSlackUserID` (the same stored-Slack-ID → email-lookup order,
  for one `ListUserDirectory([createdBy])` call). `createIncidentChannel`
  combines role-holder IDs + the creator + the pending `/sev open` opener
  into **one** deduplicated `InviteUsers` call, instead of one call per
  source — closing the loop so a role holder who's also the creator (or the
  slash-command opener) is invited exactly once.
- **Known limitation, not fixed here** (already named by Phase 11d): for a
  SEV opened via `/sev open`, `created_by` is not the human's Sevitout
  identity — the slash-command handler never sets it on `CreateSEVRequest`,
  so the server fills it in from the caller's own auth context, which for
  the bot's gRPC calls is the bot's *service account*. The creator-invite
  resolves to that service account (typically no Slack identity → a
  harmless no-op) rather than duplicating the human, who's already invited
  via the separate pending-opener path. Threading the real identity through
  `/sev open` remains a named follow-up.

## Setting up your own integration profile

Each identity is something *you* find and enter yourself — there is no admin
step and no OAuth flow. Open **Sevitout → the user-cog icon in the header →
My Profile**, or `curl` `PATCH /v1/auth/me` directly (see Walkthrough below).

**Slack User ID** (e.g. `U0123ABCDEF`) — in Slack: click your profile photo →
**Profile** → the "**⋯**" (more) menu → **Copy member ID**. It is *not* your
`@handle` or display name. Leaving it blank is fine — Sevitout falls back to
resolving you by the email address on file (Slack `Lookup by email`), which
works whenever your Slack account uses the same email as Sevitout.

**GitHub Username** (e.g. `octocat`) — your ordinary GitHub login, visible in
your GitHub profile URL (`github.com/<username>`). Used verbatim as an
assignee login; GitHub itself will reject an issue-creation call naming a
login that doesn't have access to the target repo, so make sure you're a
collaborator on whatever repo you plan to file against.

**Jira Account ID** (e.g. `5b10a2844c20165700ede21g`) — an opaque Jira Cloud
ID, **not** your email or display name, and not visible anywhere in Jira's
normal UI. Two ways to find it:
1. Open your own Jira profile page (click your avatar → **Profile**) and copy
   the ID out of the page's URL — it's the segment after `/people/`.
2. Ask a Jira admin to look it up via `GET
   /rest/api/3/user/search?query=<your-email>` (the Jira client this project
   ships has no such lookup built in yet — see Known limitations).

## Prerequisites

- `go build ./... && go test ./...` and `npm --prefix web run test` passing
- A running server + web app (`make up`), or the individual dev servers
- For 10d/10e's Slack behavior: a configured "slack" integration
  (`demo/datastore-slack-bot-credentials.md` or `SLACK_BOT_TOKEN`/
  `SLACK_APP_TOKEN` env vars) and `cmd/slackbot` running
- For 10f: `GITHUB_TOKEN` and/or `JIRA_CLOUD_ID`/`JIRA_API_TOKEN` configured
  (`demo/jira-integration.md`)

## Walkthrough

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

# Set your own identities (full-replace — send all three every call).
curl -s -X PATCH http://localhost:8080/v1/auth/me \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"slack_user_id":"U0123ABCDEF","github_username":"octocat","jira_account_id":"5b10a2844c20165700ede21g"}'
# {"id":"...", "email":"admin@example.com", ..., "slack_user_id":"U0123ABCDEF",
#  "github_username":"octocat", "jira_account_id":"5b10a2844c20165700ede21g"}

# A minimal directory lookup — same call the role picker and Slack
# auto-invite use, open to any authenticated Viewer.
curl -s "http://localhost:8080/v1/auth/directory?query=ada" -H "Authorization: Bearer $TOKEN"
# {"users":[{"id":"...", "name":"Ada Lovelace", "email":"ada@example.com", "slack_user_id":"U0..."}]}

# Create a SEV as this user (no role assignment needed) — created_by is the
# caller's own ID, exactly what cmd/slackbot's sev.created handler now reads
# to invite the creator (10h), even though they hold no role on it.
curl -s -X POST http://localhost:8080/v1/sevs -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Creator invite test","severity_level":2}'
# {"id":"SEV-2026-0001", "title":"Creator invite test", ..., "created_by":"<your user id>"}
# With cmd/slackbot running and Slack configured, its incident channel gets
# one InviteUsers call inviting your resolved Slack ID alongside any
# assigned roles — not a separate call per source.
```

**In the web app:**

1. Log in, click the user-cog icon in the header → **My Profile**. Set your
   Slack User ID / GitHub Username / Jira Account ID, **Save**.
2. Open a SEV, assign a role via **Roles** → type a couple of characters in
   "Search users to link (optional)…", pick a real user from the dropdown —
   the free-text Name field pre-fills, and `user_id` is now set on the
   assignment.
3. Open a SEV whose incident channel was created after this shipped (its
   `slack_channel_id` is set): each role row now has an "Add to chat" icon —
   click it to invite that role's holder directly, without waiting for the
   channel-creation auto-invite. On an older SEV (no channel), the button is
   present but disabled.
4. **Tasks** panel → **Create GitHub issue** (or **Create Jira issue**): the
   new "Assignee" field pre-fills from your own profile the first time you
   open that form; edit or clear it before submitting.
5. With your Slack User ID set, create a new SEV from the web UI without
   assigning yourself any role — once `cmd/slackbot` creates its incident
   channel, you're invited to it as the creator (10h), not just role
   holders.

## Known limitations

- **Name/avatar/password editing** stay out of scope for the Profile page —
  a named follow-up, not silently expanded into here.
- **No Jira account-ID lookup helper** — the Jira client in this codebase has
  only `Ping`/`GetIssue`/`CreateIssue`; an email→accountId search (Jira's
  `GET /rest/api/3/user/search`) is a named follow-up, not built here. Most
  users won't know their own account ID without the manual workaround above.
- **"Add to chat" only works for SEVs whose channel was created after this
  shipped** — `slack_channel_id` has no retroactive backfill for older SEVs;
  the button must (and does) render disabled rather than error for those.
- **The role picker is optional, not mandatory** — assigning by free-text
  display name alone remains fully supported and produces a role with no
  `user_id`, which still resolves via the DisplayName-regex fallback for
  Slack invite, just not via a stored identity.
- **Stale/duplicate identities aren't validated** — nothing checks that a
  Slack User ID or GitHub username you enter actually corresponds to your
  own account; an incorrect value just resolves to the wrong person (Slack)
  or a rejected assignment (GitHub/Jira reject an unrecognized assignee at
  issue-creation time), not silently to nothing.
- **SEVs opened via `/sev open` don't attribute `created_by` to the human
  opener** (10h) — it resolves to the slackbot's own service account
  instead, a harmless no-op for the creator-invite specifically (the human
  is still invited separately via the existing pending-opener path for that
  same command). Threading the real identity through `/sev open` is a named
  follow-up.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 10) for the fuller
sequencing rationale, and [`docs/architecture-evolution.md`](../docs/architecture-evolution.md)
for the resulting architecture shape. Phase 11 (integration-aware SEV UI —
hiding unconfigured tracker/Slack actions, self-service "Join Slack channel")
builds directly on this phase's
`User.SlackUserID`/etc., `ListUserDirectory`, and `SEV.SlackChannelID`.
