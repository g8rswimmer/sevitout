# M11 Demo — Slack Bot

## What was built

`cmd/slackbot`, a separate binary (per `docs/architecture.md` §7) that connects to
Slack over **Socket Mode** (no public ingress required) and to the API server over
**gRPC**. It authenticates as its own service-account user, logging itself in
(`POST /auth/login`, using `SLACKBOT_SERVICE_EMAIL`/`SLACKBOT_SERVICE_PASSWORD`) and
keeping its token fresh for as long as it runs (`cmd/slackbot/auth.go`) — no pre-issued
token to generate or manually rotate.

- **Slash commands** (`docs/requirements.md` §13.1):
  - `/sev open [severity 1-4] <title>` — creates a SEV (`SEVService.CreateSEV`);
    severity defaults to 3 when omitted.
  - `/sev update <sev-id> <message>` — posts an internal announcement
    (`AnnouncementService.CreateAnnouncement`).
  - `/sev transition <sev-id> <status>` — moves a SEV to any status
    (`SEVService.TransitionStatus`). The lifecycle state machine
    (`internal/sev.ValidateTransition`, from M02) still enforces which moves
    are legal from the SEV's current status — e.g. `resolved` is only
    reachable from `mitigated` — so this command doesn't skip steps, it just
    lets you drive them from Slack instead of the REST API. Valid statuses:
    `open`, `investigating`, `mitigated`, `resolved`,
    `postmortem_in_progress`, `postmortem_complete`.
  - `/sev resolve <sev-id>` — shorthand for `/sev transition <sev-id>
    resolved` (kept since it's the exact command name
    `docs/project-plan.md` specifies).
  - `/sev capture <sev-id> [limit]` — pulls the last `limit` (default 20) messages
    from the *current* Slack channel into the SEV's chat log
    (`ChatService.AddChatEntry`), oldest first.
- **In-channel commands** — `@sevbot status <sev-id>` and `@sevbot timeline <sev-id>`,
  answered by replying in the channel the mention occurred in.
- **Auto-created incident channel** — on *every* `sev.created` event (any severity, any
  origin — Slack, web UI, API, or an integration like PagerDuty), the bot creates a
  channel named from the `slack` integration config's `channel_naming_convention`
  setting (default `inc-{id}-{title}`), invites the on-call person if one is assigned
  and resolvable to a Slack account by email, and posts the SEV link. If the SEV was
  opened via `/sev open`, the invoking Slack user is invited too — this makes the
  channel `/sev open` was run in a pure "SEV intake" channel, with each SEV's own
  discussion happening in its own dedicated channel instead. Sensitive SEVs are
  excluded (no channel is auto-created for them). The mapping from SEV ID → channel is
  in-memory only (see Known limitations).
- **Bot notifications** — every `sev.created` and `sev.status_changed` event posts to
  the SEV's incident channel if one was auto-created, else to the `slack` integration
  config's `default_channel` setting.
- **Announcement push** — `announcement.created` events with audience `external` or
  `status-page` are pushed to Slack the same way; `internal` announcements are not.
- **Event-driven triggers (depends on M09)** — the bot opens a WebSocket connection to
  the API server subscribed to the broadcast room (`?sev_id=*`, see M09's demo) so it
  hears about every SEV, not just ones it already knows the ID of.
- **Slack credentials from config (depends on M10)** — `default_channel` and
  `channel_naming_convention` are read from `ConfigService.GetIntegrationConfig`
  (integration type `slack`) at startup, with a short retry since `docker-compose`'s
  `depends_on` only orders container start, not readiness. The bot's own Slack
  credentials (`SLACK_APP_TOKEN`, `SLACK_BOT_TOKEN`) come directly from its process
  environment — `ConfigService` never returns decrypted credentials to anyone.
- `internal/integrations/slack`: a thin wrapper around the Slack Web API (create
  channel, invite users, post message, fetch history, look up a user by email, plus a
  `Ping` used by M10's `GET /admin/integrations/health` for an optional
  admin-configured Slack health check, independent of the bot's own token).

If any of `SLACK_APP_TOKEN`, `SLACK_BOT_TOKEN`, `API_GRPC_ADDR`,
`SLACKBOT_SERVICE_EMAIL`, or `SLACKBOT_SERVICE_PASSWORD` is unset, the bot logs a
warning and exits **cleanly (code 0)** instead of crash-looping — every earlier
milestone's demo runs `make up` without Slack configured, and `docker-compose.yml` has
no restart policy, so this keeps those demos quiet.

---

## Prerequisites

- M09 (WebSocket) and M10 (Configuration API) complete.
- A Slack workspace you can install a test app into (a free workspace you create
  yourself works fine).
- `make up` started (or the API server run locally via `go run ./cmd/server`).

## Configuring Slack (one-time setup)

The bot needs a **Slack app** with Socket Mode enabled, an app-level token, a bot
token, a slash command, and an event subscription. The fastest path is Slack's **app
manifest** feature, which creates all of this in one step.

### 1. Create the app from a manifest

Go to <https://api.slack.com/apps> → **Create New App** → **From an app manifest** →
pick your workspace → paste the following (YAML) → **Create**:

```yaml
display_information:
  name: Sevitout
features:
  bot_user:
    display_name: sevbot
    always_online: true
  slash_commands:
    - command: /sev
      description: Open, update, resolve, or capture chat for a SEV
      usage_hint: "open [severity 1-4] <title> | update <sev-id> <message> | transition <sev-id> <status> | resolve <sev-id> | capture <sev-id> [limit]"
      should_escape: false
oauth_config:
  scopes:
    bot:
      - chat:write
      - channels:read
      - channels:history
      - channels:manage
      - users:read
      - users:read.email
      - app_mentions:read
      - commands
settings:
  event_subscriptions:
    bot_events:
      - app_mention
  interactivity:
    is_enabled: true
  socket_mode_enabled: true
  token_rotation_enabled: false
```

(`channels:manage` covers both `conversations.create` and `conversations.invite` for
public channels; if you'd rather the incident channel be private, swap in the
`groups:*` scopes and see Known limitations below.)

### 2. Generate an app-level token → `SLACK_APP_TOKEN`

**Basic Information** → **App-Level Tokens** → **Generate Token and Scopes** → name it
(e.g. `socket-mode`), add the `connections:write` scope → **Generate**. Copy the
`xapp-...` token into `.env` as `SLACK_APP_TOKEN`.

### 3. Install the app → `SLACK_BOT_TOKEN`

**Install App** (left sidebar) → **Install to Workspace** → **Allow**. Copy the **Bot
User OAuth Token** (`xoxb-...`) into `.env` as `SLACK_BOT_TOKEN`.

### 4. Invite the bot to a channel

In Slack, create or pick a channel for default notifications (e.g. `#incidents`) and
run `/invite @sevbot` in it. Note the channel name — you'll set it as `default_channel`
in step 6.

## Provisioning the slackbot service account

The bot authenticates to the API server as an ordinary Sevitout user promoted to Admin
(Admin is required for `ConfigService.GetIntegrationConfig`, which the bot calls at
startup) — there's no separate bot-account mechanism to build. Unlike a human user,
though, the bot logs itself in: it calls `POST /auth/login` with
`SLACKBOT_SERVICE_EMAIL`/`SLACKBOT_SERVICE_PASSWORD` at startup, then keeps its own JWT
fresh for as long as it runs (`cmd/slackbot/auth.go`) — proactively on a fixed interval
and reactively on any call the server rejects as unauthenticated. There's no token to
generate or rotate by hand.

```bash
export API=http://localhost:8080

# One-time: register an admin user if you haven't already (first registrant is Admin).
curl -s -X POST $API/auth/register -d '{"email":"you@example.com","password":"changeme","name":"You"}' \
  -H 'Content-Type: application/json' | jq .

ADMIN_TOKEN=$(curl -s -X POST $API/auth/login -d '{"email":"you@example.com","password":"changeme"}' \
  -H 'Content-Type: application/json' | jq -r .token)

# Register a dedicated service-account user for the bot, then promote it to Admin.
curl -s -X POST $API/auth/register -d '{"email":"slackbot@sevitout.local","password":"a-strong-password","name":"Sevitout Slack Bot"}' \
  -H 'Content-Type: application/json' | jq .

BOT_USER_ID=$(curl -s "$API/v1/config/users" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | jq -r '.users[] | select(.email=="slackbot@sevitout.local") | .id')

curl -s -X PATCH "$API/v1/config/users/$BOT_USER_ID/role" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"org_role":"admin"}' | jq .
```

Put that same email/password straight into `.env` as `SLACKBOT_SERVICE_EMAIL` and
`SLACKBOT_SERVICE_PASSWORD` — that's the whole setup. No `curl .../auth/login | jq -r
.token` step, and nothing to re-run when a token would otherwise have expired.

## Configuring the `slack` integration (optional)

Set a default notification channel and/or a custom incident-channel naming convention;
skip this and the bot uses no default channel and the built-in naming convention.

```bash
curl -s -X PUT "$API/v1/config/integrations/slack" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"integration_type":"slack","settings":{"default_channel":"C0123456","channel_naming_convention":"inc-{id}-{title}"}}' | jq .
```

`default_channel` must be a Slack **channel ID** (e.g. `C0123456`, visible in a
channel's "About" panel), not its `#name` — the bot posts via `chat.postMessage`,
which takes an ID. Make sure `@sevbot` has been invited to that channel first
(`/invite @sevbot`), or `chat.postMessage` will fail with `not_in_channel`.

`default_channel` is only ever used as a fallback now — since every SEV gets its own
auto-created incident channel regardless of severity, `default_channel` really only
matters for the brief window between a SEV being created and its channel finishing
creation (or if channel creation fails outright). In practice, whatever channel you run
`/sev open` in works fine as "SEV intake": once each SEV gets its own channel, that
intake channel never accumulates any single incident's discussion.

The bot polls this config every few minutes (`slackSettingsRefreshInterval` in
`cmd/slackbot/settings.go`) and applies changes automatically — you don't need to
restart the container after changing `default_channel` or
`channel_naming_convention`, just wait for the next poll (or restart it if you don't
want to wait).

## Start the stack

```bash
cp .env.example .env   # fill in JWT_SECRET, ENCRYPTION_KEY, and the SLACK_* / SLACKBOT_SERVICE_* values above
make up
```

Or run both binaries locally without Docker (two terminals):

```bash
JWT_SECRET=changeme go run ./cmd/server
SLACK_APP_TOKEN=xapp-... SLACK_BOT_TOKEN=xoxb-... API_GRPC_ADDR=localhost:8080 \
  SLACKBOT_SERVICE_EMAIL=slackbot@sevitout.local SLACKBOT_SERVICE_PASSWORD=a-strong-password \
  go run ./cmd/slackbot
```

Check the bot's logs for `"connected to slack"` and `"connected to event stream"` —
both must appear before the walkthrough below will work.

## Walkthrough

### 1. Open a SEV-1 from Slack and watch the incident channel appear

In any channel the bot's slash command works in (this is your "SEV intake" channel —
it stays uncluttered since the SEV's own discussion moves to its dedicated channel):

```
/sev open 1 checkout is failing for all customers
```

The bot replies with the new SEV ID. Within a few seconds, a new channel named
`inc-<sev-id>-checkout-is-failing-for-all-customers` appears (check your workspace's
channel list) with an intro message linking back to the SEV — and you (whoever ran the
command) are automatically invited into it alongside the bot and, if one is assigned,
the on-call person.

### 2. Post an update

```
/sev update SEV-2026-0001 mitigation rolled out, monitoring error rate
```

This lands as an **internal** announcement on the SEV (visible via
`GET /v1/sevs/SEV-2026-0001/announcements`), not pushed to Slack again — only
`external`/`status-page` announcements are pushed (see What was built).

### 3. Ask the bot for status in-channel

```
@sevbot status SEV-2026-0001
```

The bot replies in-thread-less plain text with the SEV's current title, severity, and
status.

### 4. Advance it through the lifecycle, then resolve it

`resolved` is only reachable from `mitigated` — `/sev resolve` on a SEV that's still
`open` fails with `invalid status transition`. Drive it through the intermediate
states with `/sev transition` first:

```
/sev transition SEV-2026-0001 investigating
/sev transition SEV-2026-0001 mitigated
/sev resolve SEV-2026-0001
```

The SEV's incident channel receives a `:white_check_mark:` notification on the final
step, and `@sevbot status SEV-2026-0001` now reports `resolved`.

### 5. Capture chat into the SEV

Post a few messages in the incident channel discussing the (fictional) resolution,
then run, in that same channel:

```
/sev capture SEV-2026-0001
```

The bot replies with how many messages it captured; verify with
`GET /v1/sevs/SEV-2026-0001/chat`.

### 6. Push an external announcement

```bash
curl -s -X POST "$API/v1/sevs/SEV-2026-0001/announcements" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"message":"The issue has been resolved.","audience":"external"}' | jq .
```

This one *does* get pushed to Slack — watch it show up in the incident channel.

## Verify tests pass

```bash
go test ./cmd/slackbot/... ./internal/integrations/slack/... ./internal/api/ws/... ./internal/api/grpc/...
golangci-lint run
```

`cmd/slackbot`'s tests use hand-written fakes for the Slack client and every gRPC
client interface (`sevAPI`, `roleAPI`, `announcementAPI`, `chatAPI`, `configAPI`) — no
test talks to a real Slack workspace or a real API server. `internal/integrations/slack`
is tested against an `httptest.Server` standing in for `slack.com/api`, the same
pattern as `internal/integrations/pagerduty` and `.../tasktracker/github`.

## Known limitations

- The SEV → incident-channel mapping lives only in the bot process's memory. A bot
  restart forgets it; subsequent notifications for SEVs opened before the restart fall
  back to `default_channel` (if configured) instead of the now-unknown incident
  channel. A future milestone could persist this mapping (e.g. as an
  `integration_config` setting, or its own store).
- On-call and the `/sev open` caller are invited when an incident channel is created;
  the Incident Commander is not (per `docs/requirements.md` §13.1) — the IC is usually
  assigned *after* the SEV (and its channel) already exist, and inviting them would
  require resolving a Sevitout user ID to an email to a Slack ID, which the bot doesn't
  currently have a path for without an additional `ConfigService` lookup.
- A SEV opened via the web UI, the REST API, or an integration (e.g. PagerDuty
  auto-open) still gets its own incident channel, but has no known Slack opener to
  invite — only on-call (and, later, the IC) get invited for those.
- On-call invites depend on the on-call role's `display_name` containing an email in
  `Name <email>` form (the shape PagerDuty auto-assignment produces, M04). A manually
  entered on-call name with no email is skipped, not an error.
- The manifest above creates a **public** incident channel. Slack's private-channel
  APIs (`groups.create`, `groups.invite`, `groups.history`) are equivalent but distinct
  calls; supporting both would mean either always using the `conversations.*`
  variants everywhere they're available (only some of the SDK's private-channel
  history calls have been consolidated as of this SDK version) or picking based on a
  future `is_private` setting.
- Attribution: SEVs/announcements/status transitions made via Slack are attributed to
  the `slackbot` service-account user in the audit log, not to the individual Slack
  user who ran the command — the Slack username is included in the announcement/chat
  message text as a workaround, but isn't a substitute for real per-user attribution.
- No slash-command signature/request verification beyond Socket Mode's own transport
  (Socket Mode connections are already authenticated to your app; there is no separate
  signing-secret check to add, unlike the classic HTTP Events API).
- `/sev capture` reads from whatever channel the command was run in — it doesn't
  validate that channel is the SEV's own incident channel, so it's possible (if
  unlikely in practice) to capture an unrelated channel's history into a SEV by
  running the command from the wrong place.
- The bot's `POST /auth/login` call and every gRPC call both go out over plain
  HTTP/gRPC (`insecure.NewCredentials()` — matching this repo's existing internal-only
  posture, not a regression introduced here), so `SLACKBOT_SERVICE_PASSWORD` and every
  minted token cross the network unencrypted. Fine for a trusted internal network (e.g.
  same-host Docker Compose); a real deployment across an untrusted network should put
  TLS in front of the API server before relying on this.
