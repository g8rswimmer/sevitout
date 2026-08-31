# Demo — Schema-driven integration settings (Roadmap Phase 9)

## What was built

Before this change, `AdminIntegrationsPage.tsx` / `IntegrationConfig` had no schema:
credentials and settings were both generic `map[string]string` rows edited through
the same `TagRowsEditor`, so raw storage keys (`bot_token`, `cloud_id`) leaked into
the UI as field labels, credential inputs were plain text instead of
password-masked, and an "Other…" option let an admin create an `integration_type`
that no client in the codebase would ever read.

This phase adds a backend-owned field catalog — the single source of truth for
field names, labels, types, and valid values — exposed via a new endpoint and
enforced server-side on upsert, and rebuilds the admin page around a sidebar of
the fixed integration set with a schema-driven detail form per integration.
Monitoring (tool type + base URL, no credentials) is added as a 5th configurable
integration, closing a `docs/requirements.md` §18.4 gap that had no UI at all
before.

**Step 0** was a [Claude Design canvas mockup](https://claude.ai/code/artifact/71ec44e3-b537-487a-99f5-8af8ebf99385)
of the sidebar + detail-form layout (credential fields password-masked, select
fields as dropdowns), reviewed before any backend/frontend code was written.

- **New package `internal/integrations/catalog`** — a dependency-free static
  registry, not a client for any one integration, so it sits outside
  `internal/integrations/{slack,pagerduty,...}` and is importable by
  `internal/api/grpc` without a cycle:
  - `catalog.Field{Key, Label, Kind (text|secret|select), Required, Help,
    Options}` and `catalog.Integration{Type, Label, CredentialFields,
    SettingsFields}`.
  - `catalog.All` — the fixed, ordered set: PagerDuty (`api_key`), GitHub
    (`token`), Slack (`bot_token` + `app_token`; settings `default_channel`,
    `channel_naming_convention`), Jira (`api_token`; settings `cloud_id`
    required, `site_url` optional), Monitoring (settings only: `tool` as a
    select — datadog/prometheus/cloudwatch, deliberately *without* an "other"
    option since there's no base-URL shape to assume for an unnamed tool — and
    `base_url` as text). All storage keys reuse today's convention exactly, so
    no data migration was needed.
- **New RPC `ConfigService.GetIntegrationCatalog`** → `GET
  /v1/config/integration-catalog` (Admin-only, for consistency with the rest
  of `ConfigService`), a pure translation of `catalog.All` with no store
  access. **Not** nested under `/v1/config/integrations/{integration_type}` as
  first planned — `.../integrations/catalog` collides with that path
  template, and grpc-gateway silently resolves the ambiguity in favor of the
  wildcard route (verified live, then fixed) rather than erroring, so it's a
  sibling path instead, the same non-colliding-path reasoning
  `GetSlackBotCredential` (Phase 8) already used.
- **`UpsertIntegrationConfig` now validates against the catalog** before
  touching the store or crypto: an unknown `integration_type`, an unknown
  credential/settings key, or an invalid `select` value all reject the whole
  request with `codes.InvalidArgument`, matching the handler's existing
  all-or-nothing semantics (it already rolls back on refresher rejection).
  `Required` is intentionally **not** enforced at upsert time — a request can
  supply just credentials or just settings and leave the other side
  untouched, so "required" would have to reason about the merged
  existing+incoming state; the existing fallback-to-static-client behavior in
  `cmd/server/*_resolver.go` already covers "this won't activate without X",
  so `Required` stays a UI-only affordance in this phase.
- **`AdminIntegrationsPage.tsx` rebuilt around the catalog endpoint**, the
  hardcoded `KNOWN_INTEGRATIONS` array gone entirely:
  - Left-hand sidebar of the 5 catalog entries (`api.config.integrations.catalog()`),
    each row showing its label, a Configured/Not-set badge, and its health
    badge — replacing the old separate "Configured integrations" table.
    Selecting a row is the page's primary navigation now, not a table action.
  - Right-hand detail form rendered from the selected integration's schema:
    credential fields as `<Input type="password">` (placeholder communicates
    "leave blank to keep the current value" once something's already
    configured, else "Enter value"), settings fields as `<Input>` or, for a
    `select`-kind field (Monitoring's `tool`), the existing `<Select>`
    component — non-secret settings always show their current value.
  - `TagRowsEditor` stays for SEV tags elsewhere in the app; only this page's
    credential/settings editing moved off it. The "Other…" branch,
    `customType` state, and the generic key/value rows for known integrations
    are gone.
  - `IntegrationDetailForm` is mounted with `key={entry.type}` so switching
    sidebar rows remounts it fresh, resetting its local form state — the
    idiomatic React way to do this, versus an effect that copies props into
    state on every prop change (which oxlint's `react(set-state-in-effect)`
    correctly flagged during development; see Design notes).
  - `web/src/types/api.ts` / `lib/api.ts` gain `CatalogField`/
    `IntegrationCatalogEntry`/`GetIntegrationCatalogResponse` and
    `config.integrations.catalog()`, mirroring every other endpoint's
    existing pattern.

**Follow-up, same phase: a readable health status.** Two gaps surfaced once
an admin actually used the shipped page against real credentials:

- **The `/admin/` path was never proxied at all.** Neither `web/nginx.conf`
  (production) nor `vite.config.ts`'s dev-server proxy has ever forwarded
  the `/admin/` prefix to the API — only `/v1`, `/auth`, `/s/`, `/ws`, and
  `/openapi.json` were. `GET /admin/integrations/health` (a plain
  `net/http` handler, not under `/v1/` like everything else
  `ConfigService`-shaped) silently fell through to the SPA's `index.html`
  fallback instead of reaching the backend, so every health badge always
  looked empty regardless of what was actually configured — a bug that
  predates this phase entirely, just never noticed until this page made
  someone look. Fixed by adding a matching `location /admin/` block to
  `nginx.conf` and a `/admin` entry to `vite.config.ts`'s proxy map.
- **"Connected" wasn't visually distinct from "Not set"/"No health
  check."** `Badge` had no green variant at all — `HEALTH_BADGE.connected`
  used the same neutral `secondary` styling as everything else. Added a
  `success` badge variant (new `--success`/`--success-foreground` oklch
  tokens, same lightness/chroma shape as `--destructive`, just a green hue)
  and pointed `connected` at it.
- **A failed health check's detail was hidden behind a hover tooltip.**
  The error string was already there (`title={h?.error}` on the badge) but
  invisible until you hovered, and easy to miss even then. It now renders
  directly in the detail panel — the actual message each integration's
  client returns (GitHub/Jira include the real API error text, e.g.
  `"jira: status 401: Unauthorized"`; Slack returns its own error code,
  e.g. `"invalid_auth"`/`"token_revoked"`), paired with a short static
  troubleshooting hint per integration type (`TROUBLESHOOTING_HINTS`).
  PagerDuty's checker previously discarded the response body entirely and
  reported only a bare `"unexpected status 401"` — the one integration
  with nothing useful to show — so `internal/integrations/pagerduty`
  gained its own `APIError` type (mirroring `github.APIError`/
  `jira.APIError`'s existing pattern) that captures PagerDuty's real
  `error.message` field.

## Design notes

**The path-collision bug was caught by actually running the server, not by
review.** The roadmap's original plan specified `GET
/v1/config/integrations/catalog` — a natural-looking path that in fact
collides with `GetIntegrationConfig`'s `/v1/config/integrations/{integration_type}`
template. grpc-gateway doesn't error on this ambiguity; it silently routes
`.../integrations/catalog` to `GetIntegrationConfig` with
`integration_type="catalog"`, which just looks like an ordinary 404 ("integration
config not found") — exactly the kind of bug that survives code review and unit
tests (which call the Go method directly, bypassing gateway routing entirely) but
shows up the moment someone curls the real HTTP path. Caught during this phase's
own live verification and fixed by moving the catalog RPC to
`/v1/config/integration-catalog`, a sibling path with no ambiguity.

**`IntegrationDetailForm` as a keyed subcomponent, not an effect.** The first
draft kept `credentialValues`/`settingsValues` state in the parent and reset
them via a `useEffect` keyed on the selected integration — functionally
correct, but oxlint's `react(set-state-in-effect)` rule flagged it as the
anti-pattern it is (an effect synchronously calling `setState` to copy a prop
into state, triggering a second render). The fix follows React's own
documented guidance for "reset state when a prop changes": extract the form
into its own component and mount it with `key={entry.type}` — switching
integrations now remounts the component, and `useState`'s lazy initializer
computes the starting values once, with no effect at all.

**Why `Required` stays UI-only**, restated because it's easy to assume
otherwise: `UpsertIntegrationConfig` supports partial updates — a
credentials-only or settings-only request leaves the other side untouched.
Enforcing "required" server-side would mean reasoning about the *merged*
existing-plus-incoming state (is `cloud_id` still set after this request,
even though this request didn't touch it?), which is real complexity for a
guarantee the existing fallback-to-static-client behavior in
`cmd/server/*_resolver.go` already provides at runtime (an incompletely
configured integration just doesn't activate). `Required` in the catalog
today only drives the "(required)"/"(optional)" hint text in the form.

## Prerequisites

- `go build ./... && go vet ./... && go test -race ./...` and `web`'s `tsc -b
  && vitest run && oxlint` passing.
- No database needed for the walkthrough below — the catalog and validation
  behavior are all visible against the in-memory store (`DATABASE_URL` unset).

## Walkthrough

Live-verified against a real `cmd/server` process (in-memory store):

```bash
JWT_SECRET=demo-secret-please-ignore-1234567890 \
ENCRYPTION_KEY=$(python3 -c "import base64,os; print(base64.b64encode(os.urandom(32)).decode())") \
./server &

curl -s -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1","name":"Alice Admin"}'
TOKEN=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct-horse-battery-1"}' | jq -r .token)

# 1. fetch the catalog — the sidebar + detail form's entire schema in one call
curl -s localhost:8080/v1/config/integration-catalog -H "Authorization: Bearer $TOKEN"
# {"integrations":[{"type":"pagerduty","label":"PagerDuty","credential_fields":
#  [{"key":"api_key","label":"API Key","kind":"secret","required":true}]}, ...
#  {"type":"monitoring","label":"Monitoring","settings_fields":[
#    {"key":"tool","label":"Tool","kind":"select","required":true,
#     "options":["datadog","prometheus","cloudwatch"]},
#    {"key":"base_url","label":"Base URL","kind":"text"}]}]}

# 2. an unknown integration_type is rejected before anything is written
curl -s -w '\nHTTP %{http_code}\n' -X PUT localhost:8080/v1/config/integrations/datadog \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"integration_type":"datadog","settings":{"base_url":"https://x"}}'
# {"code":3,"message":"unknown integration_type \"datadog\""}
# HTTP 400

# 3. an unknown settings key for a real integration_type is rejected
curl -s -w '\nHTTP %{http_code}\n' -X PUT localhost:8080/v1/config/integrations/jira \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"integration_type":"jira","settings":{"default_project":"OPS"}}'
# {"code":3,"message":"jira: unknown settings key \"default_project\""}
# HTTP 400

# 4. an invalid select value is rejected
curl -s -w '\nHTTP %{http_code}\n' -X PUT localhost:8080/v1/config/integrations/monitoring \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"integration_type":"monitoring","settings":{"tool":"new-relic"}}'
# {"code":3,"message":"monitoring: tool must be one of [datadog prometheus cloudwatch], got \"new-relic\""}
# HTTP 400

# 5. a valid Monitoring config (settings-only, no credentials) round-trips
curl -s -X PUT localhost:8080/v1/config/integrations/monitoring \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"integration_type":"monitoring","settings":{"tool":"datadog","base_url":"https://app.datadoghq.com"}}'
curl -s localhost:8080/v1/config/integrations/monitoring -H "Authorization: Bearer $TOKEN"
# {"integration_type":"monitoring","settings":{"base_url":"https://app.datadoghq.com","tool":"datadog"}, ...}
```

This exercises the exact validation the schema-driven frontend form relies on:
the catalog shape it renders from, and each of the three ways a request can be
rejected before it reaches the store. The frontend rebuild itself (sidebar
navigation, password-masked credential inputs with real labels, the Monitoring
select, the blank-credential-omitted-from-payload behavior, and the
server-error-surfaced-through-the-alert-path) is verified by
`AdminIntegrationsPage.test.tsx`'s 8 tests — see Verify tests pass below.

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./...
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New/updated coverage:
- `internal/integrations/catalog/catalog_test.go` (new): structural sanity
  checks over `catalog.All` — unique types/keys, `select` fields have
  options and non-select fields don't, every credential field is
  `KindSecret`, the fixed sidebar order, `Find`/`CredentialKeys`/
  `SettingsField` helpers.
- `internal/api/grpc/config_test.go`: `TestGetIntegrationCatalog_Shape`
  (fixed order, Slack's 2 secret credential fields, Monitoring's
  credential-less + 3-option select shape, no "other" option) plus
  `UpsertIntegrationConfig` rejection tests — unknown `integration_type`,
  unknown credential key, unknown settings key, invalid select value, that
  a catalog rejection happens before the store/crypto are touched — and a
  valid Monitoring config round-tripping through `Get`/`List`. Several
  pre-existing tests that used ad-hoc keys with no real schema (PagerDuty's
  `default_escalation_policy`, GitHub's `default_repo`, a bare `"datadog"`
  integration_type) are updated to catalog-valid equivalents — confirmed
  those keys were never read by any production code.
- `web/src/pages/admin/AdminIntegrationsPage.test.tsx` (rewritten, 9 tests):
  sidebar shows exactly 5 entries with no "Other…" anywhere; labels render
  as "Bot Token" not `bot_token`; credential inputs are `type="password"`;
  Monitoring's `tool` renders as a 3-option select; switching sidebar rows
  swaps the detail form; an existing settings value pre-fills while
  credential fields stay blank; a blank credential is omitted from the save
  payload while settings are still sent; a server validation error surfaces
  through the existing error-alert path; a "Connected" badge carries the
  green `success` styling; a failed health check shows its raw error
  message and troubleshooting hint in the detail panel.
- `internal/integrations/pagerduty/client_test.go`: PagerDuty's real
  `error.message` reaches both `Error()` and the exported `APIError`'s
  fields; a non-JSON or unrecognized error body still falls back cleanly
  to the bare-status message rather than panicking or itself erroring.

Backend aggregate coverage after this change (the same package set and
`-race -covermode=atomic` invocation as `.github/workflows/backend-ci.yml`'s
gate step): **80.1%** (gate: 78.0%, see `demo/test-coverage-ci-gate.md`).

## Known limitations

- **GitHub/Jira default-project and PagerDuty default-escalation-policy
  settings are still unsupported** — no consumer exists yet in the codebase
  for either, so they're not in the catalog. Adding either is a small,
  additive catalog change (plus whatever code would actually read the
  setting) whenever a real consumer is built.
- **The catalog is static Go code, not itself admin-editable.** Every entry
  already has a real client in the codebase (or, for Monitoring, no client
  at all by design), and a 6th integration is a one-file, one-entry catalog
  change when it's actually needed — not worth a datastore-backed
  catalog-of-catalogs for a fixed, rarely-changing set.
- **Monitoring has no live health check by design** — there's nothing to
  poll (no API, just a tool name + URL an admin already knows), so its
  sidebar row always shows "No health check", matching the existing
  fallback badge every other unregistered health checker already gets.
- **`GetIntegrationCatalog`'s path had to move mid-phase** from the
  roadmap's originally planned `/v1/config/integrations/catalog` to
  `/v1/config/integration-catalog`, for the path-collision reason explained
  under Design notes above — noted here since it's a deviation from the
  roadmap doc's literal text, not silently reconciled.
- **Troubleshooting hints are static, per-integration-type text, not
  derived from the actual error.** Jira's hint mentions both a bad API
  token and a mismatched Cloud ID as equally likely causes for the same
  `401`, since the raw error message alone doesn't distinguish which one
  it actually was. Good enough as a starting point; a real improvement
  would need per-error-code guidance from each client, which none of the
  four currently expose in a structured way (just an error string).
