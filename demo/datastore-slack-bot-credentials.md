# Demo — Datastore-driven Slack bot credentials (Roadmap Phase 8)

## What was built

Before this change, PagerDuty/GitHub/Jira all preferred a datastore-configured
credential over their static `*_API_KEY`/`*_TOKEN` env var, picking up an
admin's change with no restart — but Slack did not: `cmd/slackbot` built its
real Slack client directly from `SLACK_BOT_TOKEN`/`SLACK_APP_TOKEN` once, at
startup, and saving a new bot token via the admin UI had no effect on the
running bot process (see the interim mitigation this phase closes:
`AdminIntegrationsPage.tsx`'s Slack `note`).

This phase closes that gap: `cmd/slackbot` now starts entirely from a
datastore-configured "slack" integration credential, with no
`SLACK_APP_TOKEN`/`SLACK_BOT_TOKEN` env vars required at all — both **Socket
Mode** (slash commands, `@mentions`) and the **REST client** (channel
creation, posting messages, inviting users, fetching history, looking up a
user by email) prefer it over the static env vars at startup. Only *live*
picking-up of a credential rotated after startup is REST-client-only: the
REST client refreshes on a poll with no restart, Socket Mode does not — see
Known limitations.

- **New RPC `ConfigService.GetSlackBotCredential`** (`GET
  /v1/config/integrations/slack/bot-credential`) — the one deliberate
  exception to "credentials never cross this service's wire". It decrypts
  and returns the `slack` integration's `bot_token`/`app_token` pair, but
  only to the caller authenticated as the specific slackbot service account
  (`SLACKBOT_SERVICE_EMAIL`, now read by `cmd/server` too, not just
  `cmd/slackbot`) — an unrelated Admin gets `PermissionDenied`, and so does
  every caller when no service account is configured server-side at all
  (fail closed). Returns an empty response (not an error) when the `slack`
  integration has no credentials configured, so the caller's own
  static-token fallback applies.
- **`app_token` joins `bot_token`** as the Slack integration's second
  well-known credential key — both are now stored (and editable) together,
  symmetric with how Jira's `cloud_id`/`site_url` already live together.
  `AdminIntegrationsPage.tsx` pre-fills both as their own rows and its note
  now describes what actually happens: both halves of the bot start from
  this pair, but only the REST client picks up a *later* change without a
  restart.
- **`cmd/slackbot`'s startup gate no longer hard-requires
  `SLACK_APP_TOKEN`/`SLACK_BOT_TOKEN`.** Only `API_GRPC_ADDR`/
  `SLACKBOT_SERVICE_EMAIL`/`SLACKBOT_SERVICE_PASSWORD` (needed just to reach
  the API server at all) are still required env vars; the two Slack tokens
  are read as an optional fallback via plain `os.Getenv`. `main()` resolves
  the pair to actually use (datastore preferred, these two as fallback)
  *before* deciding whether to disable — so a deployment that configures
  Slack purely through the admin UI, with neither token set in the
  container's environment, now starts and runs correctly. It still disables
  itself gracefully (exit code 0, no restart loop) when *neither* source
  has anything usable.
- **`cmd/slackbot`'s REST client is now swappable.** A new
  `slackClientResolver` (`cmd/slackbot/slack_resolver.go`) wraps `bot.slack`,
  delegating to whichever concrete client is currently live and swapping it
  in place via `apply(botToken)` — the same shape as `cmd/server`'s
  `*Resolver.apply` (e.g. `pagerdutyResolver`), just living in `cmd/slackbot`
  instead, since this REST client runs in a separate binary with no direct
  datastore/`ENCRYPTION_KEY` access.
- **Startup and the existing settings poller both resolve the pair.**
  `loadSlackSettings` now also calls `GetSlackBotCredential` at startup,
  preferring a datastore-configured pair over the static
  `SLACK_BOT_TOKEN`/`SLACK_APP_TOKEN` env vars (env as fallback, matching
  every other integration) — and `main()` builds *both* Socket Mode and the
  REST client from that one resolved pair, not from the raw env vars.
  `runSettingsRefresher`'s existing periodic poll (already re-fetching
  `default_channel`/`channel_naming_convention` every
  `slackSettingsRefreshInterval`) now also re-polls the credential and calls
  `apply` on the REST client's resolver — folded into the existing poller
  rather than a second one. `apply` is a no-op when the token hasn't
  actually changed, so a routine poll doesn't rebuild (and silently
  "reconnect") an identical client every five minutes.

## Design notes

**Why this isn't the same fix as PagerDuty/GitHub/Jira**, restated from the
roadmap phase because it's the crux of the design: those three resolvers run
in-process inside `cmd/server`, with direct access to
`store.IntegrationConfigStore` and the encryption key — they decrypt locally
and plaintext never leaves that process. `cmd/slackbot` is a separate binary
that only talks to the API over gRPC, and `IntegrationConfigResponse`
deliberately never returns decrypted credentials over that wire. Closing the
gap meant accepting a new gRPC path that puts plaintext secrets on the wire
for the first time in this system — so that path is deliberately as narrow as
a new capability can be: one RPC, one hard-coded purpose, gated to one
specific account rather than "any Admin."

**The caller-identity gate is enforced twice, at two different layers, on
purpose.** RBAC (`internal/auth/rbac.go`) still requires `OrgRoleAdmin` to
call `GetSlackBotCredential` at all — the same floor as every other
integration-config RPC, so the interceptor's existing all-methods-need-an-
entry invariant holds. `ConfigServer.GetSlackBotCredential` then narrows
*inside* the handler by comparing the caller's email against
`SlackbotServiceEmail`. Neither layer alone was enough: RBAC can't express
"this one Admin, not any Admin," and the handler alone (with no RBAC entry)
would have been denied by `HasPermission`'s default-deny-unlisted-methods
rule before ever reaching the handler body.

**`apply`'s no-op-on-unchanged-token behavior is a correctness property, not
just an optimization.** Without it, every `slackSettingsRefreshInterval` tick
would discard and rebuild `bot.slack`'s concrete client even when nothing
changed — harmless for a stateless REST wrapper today, but exactly the kind
of "reconnect for no reason" behavior that would make live Socket Mode
reconnection (the deferred follow-up) much harder to reason about later.
`newSlackClientResolver` is seeded with whichever token was resolved at
startup for the same reason — so the very first periodic poll is a genuine
no-op when the datastore hasn't changed, not an unconditional one-time
rebuild.

**An earlier version of this change still hard-required
`SLACK_APP_TOKEN`/`SLACK_BOT_TOKEN` at startup, defeating the point of the
phase — caught during review, not shipped.** The first pass moved credential
*preference* to the REST client but left `main()`'s original all-five-vars-
required gate in place, so a deployment relying purely on the datastore
(no Slack env vars in the container at all) still logged `SLACK_APP_TOKEN is
not set` / `SLACK_BOT_TOKEN is not set` / `slackbot disabled` and exited
before ever calling `GetSlackBotCredential` — the datastore path was
unreachable code for exactly the deployment shape it was built for. The fix:
only `API_GRPC_ADDR`/`SLACKBOT_SERVICE_EMAIL`/`SLACKBOT_SERVICE_PASSWORD`
remain a hard-required gate (nothing works without reaching the API at all);
the two Slack tokens are read optionally and folded into the same
datastore-preferred resolution `loadSlackSettings` already does, with the
disable-gracefully check moved to *after* that resolution succeeds or fails.
Verified live — see the Walkthrough.

## Prerequisites

- `go build ./... && go vet ./... && go test -race ./...` and `web`'s `tsc -b
  && vitest run` passing.
- No database or live Slack workspace needed for the walkthrough below — the
  new RPC's gating and fallback behavior are all visible against the
  in-memory store (`DATABASE_URL` unset).

## Walkthrough

Run against a real `cmd/server` process (in-memory store), not just unit
tests, since the interesting behavior here is the RBAC/identity gate as seen
over the wire.

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 \
ENCRYPTION_KEY=$(python3 -c "import base64,os; print(base64.b64encode(os.urandom(32)).decode())") \
SLACKBOT_SERVICE_EMAIL=slackbot@example.com \
./server &

# first registered user becomes Admin (existing bootstrap behavior)
curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}'

# the slackbot service account is an ordinary user, starts as Viewer
curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"slackbot@example.com","password":"another-secret-2","name":"Slackbot Service Account"}'

TOKEN_ALICE=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1"}' | jq -r .token)

# not yet configured: an empty response, not an error
curl -s -w '\nHTTP %{http_code}\n' localhost:8080/v1/config/integrations/slack/bot-credential \
  -H "Authorization: Bearer $TOKEN_ALICE"
# {} / HTTP 403 — Alice is Admin but not the slackbot service account (see below);
# even before configuring credentials, identity is checked first.

# promote the service account to Admin (required — RBAC floor is Admin+)
SLACKBOT_ID=$(curl -s "localhost:8080/v1/config/users?query=slackbot" -H "Authorization: Bearer $TOKEN_ALICE" | jq -r '.users[0].id')
curl -s -X PATCH "localhost:8080/v1/config/users/$SLACKBOT_ID/role" -H "Authorization: Bearer $TOKEN_ALICE" \
  -H 'Content-Type: application/json' -d '{"org_role":"admin"}'

# configure the slack integration's credential pair
curl -s -X PUT localhost:8080/v1/config/integrations/slack -H "Authorization: Bearer $TOKEN_ALICE" \
  -H 'Content-Type: application/json' \
  -d '{"integration_type":"slack","credentials":{"bot_token":"xoxb-demo-token","app_token":"xapp-demo-token"}}'
# {"integration_type":"slack","credentials_configured":true, ...}

# Alice — an unrelated Admin — still can't pull the plaintext credential
curl -s -w '\nHTTP %{http_code}\n' localhost:8080/v1/config/integrations/slack/bot-credential \
  -H "Authorization: Bearer $TOKEN_ALICE"
# {"code":7,"message":"only the slackbot service account may call GetSlackBotCredential"}
# HTTP 403

# the slackbot service account itself gets the decrypted pair
TOKEN_SLACKBOT=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"slackbot@example.com","password":"another-secret-2"}' | jq -r .token)
curl -s -w '\nHTTP %{http_code}\n' localhost:8080/v1/config/integrations/slack/bot-credential \
  -H "Authorization: Bearer $TOKEN_SLACKBOT"
# {"bot_token":"xoxb-demo-token","app_token":"xapp-demo-token"}
# HTTP 200
```

This is exactly the trio of behaviors the design targets: fail-closed when no
service account is configured, `PermissionDenied` for a caller who is Admin
but not *the* service account, and a clean plaintext hand-off for the one
caller this exists for.

**Now the actual point of the phase — `cmd/slackbot` starting from *only*
the datastore config, with no Slack env vars in its environment at all:**

```bash
env -i PATH="$PATH" \
  API_GRPC_ADDR=localhost:8080 \
  SLACKBOT_SERVICE_EMAIL=slackbot@example.com \
  SLACKBOT_SERVICE_PASSWORD=another-secret-2 \
  ./slackbot
# {"level":"INFO","msg":"slackbot starting","api_addr":"localhost:8080"}
# {"level":"INFO","msg":"connecting to slack"}
# {"level":"INFO","msg":"connected to event stream","url":"ws://localhost:8080/ws?sev_id=%2A"}
# {"level":"ERROR","msg":"socket mode run failed","err":"invalid_auth"}
```

No `SLACK_APP_TOKEN is not set` / `SLACK_BOT_TOKEN is not set` / `slackbot
disabled` warnings — it resolved `xoxb-demo-token`/`xapp-demo-token` from
the datastore and genuinely attempted a Socket Mode connection to Slack's
real servers with them. `invalid_auth` is Slack's real API rejecting the
fake demo token (there's no real Slack workspace in this environment) — the
*expected* failure mode here, and proof the bot used the resolved
credential rather than declining to start. Live-verified exactly like this
against a real `cmd/server` process; not shown against a real Slack
workspace, since none is available in this environment (see Known
limitations for what that half of the picture still relies on unit tests
for).

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./...
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage:
- `internal/api/grpc/config_test.go`:
  `TestGetSlackBotCredential_ReturnsDecryptedPairForServiceAccount`,
  `TestGetSlackBotCredential_NotConfigured_ReturnsEmptyNotError`,
  `TestGetSlackBotCredential_WrongCallerEmail_PermissionDenied`,
  `TestGetSlackBotCredential_NoServiceEmailConfigured_RejectsEveryCaller`.
- `internal/config/config_test.go`: `SlackbotServiceEmail` added to the
  existing `TestLoad_ReadsEveryField`/`clearEnv` coverage.
- `cmd/slackbot/slack_resolver_test.go` (new):
  `TestSlackClientResolver_DelegatesToCurrentClient`,
  `TestSlackClientResolver_Apply_SwapsInNewClient`,
  `TestSlackClientResolver_Apply_SameTokenIsNoOp`,
  `TestSlackClientResolver_SatisfiesSlackClientInterface`.
- `cmd/slackbot/main_test.go`: `TestLoadSlackSettings_Success` extended for
  the resolved credential pair, plus new
  `TestLoadSlackSettings_NoDatastoreCredential_FallsBackToStaticTokens`.
- `cmd/slackbot/settings_test.go`: new
  `TestRunSettingsRefresher_AppliesDatastoreConfiguredSlackCredential`,
  `TestRunSettingsRefresher_NilRESTClient_DoesNotPanic`,
  `TestRefreshSlackRESTClient_EmptyBotToken_LeavesCurrentClientInPlace`,
  `TestRefreshSlackRESTClient_FetchError_LeavesCurrentClientInPlace`.
- `web/src/pages/admin/AdminIntegrationsPage.test.tsx`: the Slack test
  extended for the second `app_token` credential row and the rewritten note
  text.

Backend aggregate coverage after this change (the same package set and
`-race -covermode=atomic` invocation as `.github/workflows/backend-ci.yml`'s
gate step): **80.0%** (gate: 78.0%, see `demo/test-coverage-ci-gate.md`).

## Known limitations

- **Socket Mode does not live-reconnect.** `smClient` (slash commands,
  `@mentions`) is built once, at startup, from whichever bot/app token pair
  `loadSlackSettings` resolved (datastore preferred, static env vars as
  fallback) — so it *does* benefit from a purely datastore-configured
  deployment at startup, same as the REST client. What it does *not* do is
  pick up a credential rotated *after* startup: a token changed via the
  admin UI reaches `bot.slack`'s REST calls (channel creation, messages,
  invites, history, user lookup) within one refresh interval, but slash
  commands and mentions still need a `slackbot` container restart to pick up
  the new pair. This is the explicitly-scoped-out "genuinely hard part" from
  the roadmap phase, not an oversight — reconnecting a long-lived Socket
  Mode session needs its own retry/backoff design.
- **`app_token` is only ever used for Socket Mode's one-time startup
  construction, never refreshed.** `resolveSlackBotCredential`/
  `GetSlackBotCredentialResponse` return both tokens; `runSettingsRefresher`
  only re-applies `bot_token` to the REST client's `slackClientResolver` on
  its periodic poll — `app_token` has no live-refresh path at all, matching
  Socket Mode's own no-live-reconnect limitation above.
- **The one-time initial rebuild race is theoretically possible but
  harmless.** If the datastore's `bot_token` differs from the static env var
  at startup, the *first* periodic poll (up to `slackSettingsRefreshInterval`
  after startup) could in rare timing rebuild the client a second time with
  the same already-applied value if a concurrent admin write races the
  poll — `apply`'s same-token check makes this a no-op in the common case,
  and even in the rare race it's an idempotent rebuild, not a correctness
  bug.
- **Still no way to rotate the credential without an Admin doing it through
  the API/UI** — this phase doesn't add a CLI or automated rotation path,
  matching every other integration's credential-management story today.
