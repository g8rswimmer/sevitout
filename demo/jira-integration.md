# Demo — Jira Integration (Roadmap Phase 6a)

## What was built

Sevitout's v1 task-tracker integration was GitHub Issues only
(`docs/requirements.md` §13.3). This phase adds Jira as a second, independent
tracker, closing §13.3's "v2 fast-follow" — mirroring
`internal/integrations/tasktracker/github`'s client shape closely enough
that the new handler code reads almost identically to its GitHub sibling.

- **`internal/integrations/tasktracker/jira`** (new package) — `Client`
  authenticating with a Bearer token (the API token, in the `Authorization`
  header — Jira Cloud's `api.atlassian.com` gateway accepts Bearer auth, not
  HTTP Basic Auth, so no account email is needed alongside it). `NewClient`
  takes a Cloud ID and builds the gateway URL
  (`https://api.atlassian.com/ex/jira/{cloudId}`) internally; a tenant's
  Cloud ID is a UUID, not its `https://{site}.atlassian.net` name — see
  [Atlassian's API token docs](https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/)
  for both of these (an earlier version of this client got both wrong:
  Basic Auth with an email, and a caller-supplied site URL as the base — see
  Design notes below for the correction). `NewClientWithBaseURL` is the
  test-only escape hatch for pointing at an `httptest.Server`, mirroring
  `github.NewClientWithBaseURL`. `Ping`/`GetIssue`/`CreateIssue`, an
  `APIError` type — same shape as `github.Client`, down to the
  `HTTPStatus()` method `internal/api/grpc/task.go`'s shared
  `httpStatusError` interface expects. Handles Jira's REST v3 quirk that
  `description` must be Atlassian Document Format (ADF) JSON, not a plain
  string — `plainTextToADF`/`adfToPlainText` convert both ways (write side
  exact, read side best-effort — see the package's doc comments).
- **`grpchandler.JiraIssueClient`** (new interface, `internal/api/grpc/task.go`)
  — declared alongside the existing `IssueClient`, not as a generalization of
  it: Jira's create-issue call has no owner/repo concept (a project key and
  issue type instead), so forcing one shared signature would mean one side
  passing parameters that don't mean what their names say. This was one of
  the two options the roadmap named going in ("`IssueClient` likely needs
  generalizing (or a second interface)") — see Design notes below for why
  the second-interface path won out.
- **`TaskServer.CreateJiraIssue`** (new RPC, mirrors `CreateGitHubIssue`
  closely) — creates a Jira issue pre-filled with SEV context, links it to
  the SEV (`external_system: "jira"`, `task_id` = the Jira issue key, e.g.
  `"OPS-42"`), and returns the linked task record. `jiraIssueError` maps
  Jira's HTTP status codes to the closest gRPC status, same strategy as
  `githubIssueError` but a separate function — the two trackers' status-code
  vocabularies aren't identical (e.g. Jira uses 400 for a validation
  failure where GitHub's equivalent is 422).
- **`POST /v1/sevs/{sev_id}/jira-issues`** — new REST route (proto:
  `CreateJiraIssueRequest`/`TaskService.CreateJiraIssue` in `task.proto`,
  regenerated via `make proto`), RBAC floor `Responder` (matches
  `CreateGitHubIssue`'s — added to `internal/auth/rbac.go`'s
  `rpcMinRole` table, without which the RPC would be unreachable by anyone,
  admins included, since a method absent from that map is denied to all
  callers by design).
- **`internal/config`**: `JIRA_CLOUD_ID`/`JIRA_API_TOKEN` — both required
  together (unlike GitHub's single `GITHUB_TOKEN`); partial configuration is
  treated the same as none (`cmd/server/main.go` only builds a client when
  both are non-empty) rather than starting with a client that would fail
  every call. `JIRA_SITE_URL` (e.g. `"https://acme.atlassian.net"`) is a
  third, independently optional var — see "Human-clickable issue links"
  under Design notes below.
- **`cmd/server/main.go`**: `jiraIssueClient` (adapts `*jira.Client` to
  `grpchandler.JiraIssueClient`, mirroring `githubIssueClient`) and
  `jiraHealthChecker` (adapts it to `grpchandler.HealthChecker`, registered
  under `"jira"` in the `GET /admin/integrations/health` checker map,
  mirroring `githubHealthChecker`/`pagerdutyHealthChecker`) — the latter
  reads its Jira tenant's `cloud_id` from the Config API's per-integration
  *settings*, not credentials, since it identifies which tenant to call
  rather than authenticating to it. It passes `""` for the site URL, since
  `Ping` never builds an issue link.
- `README.md`, `.env.example`, `deploy/docker-compose.yml` updated with the
  three new env vars, matching every existing optional-integration's
  documentation pattern.

## Design notes

**Correction after initial review: Bearer token + Cloud ID, not Basic Auth +
site URL.** The first version of this client got Jira Cloud's actual
authentication contract wrong — it used HTTP Basic Auth (email + API token)
against a caller-supplied site base URL
(`https://{site}.atlassian.net`), which happens to also accept Basic Auth
directly for some legacy endpoints but is not what
[Atlassian's own service-account API token docs](https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/)
describe: requests through the `api.atlassian.com` gateway (the
supported, forward-looking integration path) use a Bearer token in the
`Authorization` header, addressed by Cloud ID
(`https://api.atlassian.com/ex/jira/{cloudId}`), not the tenant's site name,
and no account email is needed alongside the token at all. Fixed by
reworking `NewClient` to take `(cloudID, apiToken, siteURL)` and build the
gateway URL internally, dropping `email` from `Client` and `Config`
entirely, and switching `setHeaders` from `SetBasicAuth` to a literal
`Bearer ` prefix.

**Human-clickable issue links: a separate, optional `siteURL`.** A Cloud ID
alone doesn't determine the tenant's `https://{site}.atlassian.net` host —
the API gateway and the browsable web UI are different hosts entirely (live
testing confirmed this concretely: the same Bearer token that works against
`api.atlassian.com/ex/jira/{cloudId}` gets a `403` calling
`https://{site}.atlassian.net/rest/api/3/...` directly). So `NewClient`
takes a third, independently optional `siteURL` parameter used *only* to
build `Issue.URL` as `{siteURL}/browse/{key}` — it plays no part in any
actual API call, which always goes through the Cloud-ID-addressed gateway
regardless of whether `siteURL` is set. Left empty, `Issue.URL` falls back
to the API's own `self` resource link (not browsable, but always valid) —
see `Issue`'s doc comment.

**Second interface + second RPC, not a generalized `IssueClient` + a
`taskTrackerFactory`** — the roadmap named both as options going in. The
factory path (mirroring `internal/ai/factory.go`'s provider-switch pattern)
would let `ConfigService` pick GitHub vs. Jira *per service*, replacing
today's "call the specific RPC for the specific tracker" shape with a
selection/routing layer. That's real added complexity — a schema field,
factory wiring, and a frontend selection UI — for a capability nothing in
`docs/requirements.md` §13.3 asks for: v1's shape is simply "GitHub Issues
exist; v2 adds Jira and Linear alongside it," not "an org picks exactly one
tracker." The frontend already has a dedicated "Create GitHub Issue" action;
`CreateJiraIssue` is a sibling action of the same shape, which is the lower-risk
mirror of the existing pattern the roadmap called out as the goal.

**No 422-retry-without-labels analog for Jira.** `CreateGitHubIssue` retries
once without labels if GitHub returns 422 — that's a real GitHub quirk (a
label that doesn't already exist, combined with an org restricting who may
create new ones). Jira Cloud auto-creates unrecognized labels on the issue
instead of rejecting the request, so that failure mode doesn't exist there;
porting the retry loop anyway would be dead code exercised by nothing real.

**`cloud_id` lives in Config API `settings`, not `credentials`.** Every
other integration's per-service config here is pure credentials (a token, a
key). Jira is the first to need a piece of *non-secret* per-tenant
configuration alongside the credential — `jiraHealthChecker.Check` reads it
from `settings["cloud_id"]` accordingly, following `IntegrationConfig`'s
existing credentials-vs-settings split rather than overloading credentials
with a value that isn't secret.

**Frontend UI is out of scope for this phase** — see Known limitations.

## Troubleshooting

**`404`, message ends in a bare `jira: unexpected status 404` with no
detail** (this is what a real run against a live Jira Cloud instance first
surfaced): the client was swallowing the response body whenever it didn't
match Jira's own `{errorMessages, errors}` JSON error shape — which the
`api.atlassian.com` gateway's *own* 404s (a plain-text/HTML "not found"
page, returned when the request never reaches Jira's handler at all) never
do. Fixed: `newAPIError` now falls back to the raw response body when the
structured fields are both empty, and the client's Warn log line includes
it too — a `404` now always shows *something* concrete instead of a bare
status code.

Once the message is visible, a `404` specifically on `CreateJiraIssue`
almost always means **the request never reached Jira's own API at all** —
not "the project or issue type doesn't exist." Unlike GitHub's create-issue
endpoint (whose URL embeds `owner/repo`, so a bad one really does 404 at
GitHub's API), Jira's `POST /rest/api/3/issue` has a fixed path;
`project_key`/`issue_type` are validated in the request *body*, and Jira
returns `400`, not `404`, for an invalid one. So a `404` here points at the
gateway routing layer instead — check, in order:

1. **`JIRA_CLOUD_ID` is correct** — it must be the Cloud ID (a UUID, from
   `admin.atlassian.com`), not the site name. A well-formed but wrong UUID
   still 404s, since the gateway can't map it to any tenant. **This was the
   actual root cause the one time this was hit against a real Jira Cloud
   instance** — `admin.atlassian.com`'s `/s/{cloudId}` URL had been
   misread/mistyped into `.env`. Verify independently of the admin
   console, against the site's own public, unauthenticated tenant-info
   endpoint (no login or token needed):
   ```bash
   curl -s https://{your-site}.atlassian.net/_edge/tenant_info
   # {"cloudId":"44239164-766e-42e3-b0ca-c8c6188c96c2"}
   ```
   If that doesn't match `JIRA_CLOUD_ID`, that's the fix — not the token,
   not `project_key`/`issue_type`.
2. **The token actually supports gateway access.** The Bearer-token-via-
   `api.atlassian.com` flow this client uses is specifically documented for
   [organization-managed service-account API tokens](https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/) —
   a personal API token created the traditional way (`id.atlassian.com` →
   Basic Auth against `https://{site}.atlassian.net` directly) is a
   different mechanism and may not resolve through this gateway path at
   all, independent of whether `project_key`/`issue_type` are valid. To
   isolate this from step 1 (and from any bug in this codebase), test the
   gateway directly, bypassing the server entirely:
   ```bash
   curl -s "https://api.atlassian.com/ex/jira/$JIRA_CLOUD_ID/rest/api/3/myself" \
     -H "Authorization: Bearer $JIRA_API_TOKEN" -H "Accept: application/json"
   ```
   A `404` with `"server: AtlassianEdge"` in the response headers (check
   with `curl -v`) confirms the request is failing at Atlassian's gateway,
   before it ever reaches Jira — i.e. it's (1) or (2) above, not a bug in
   `CreateJiraIssue` itself.
3. Only once both of those check out is a genuinely bad `project_key` or
   `issue_type` worth suspecting — and that will now show as a `400` with
   Jira's own `errors` detail, not a `404`.

## Prerequisites

- `go build ./... && go test ./...` passing
- A Jira Cloud instance and an API token (Jira Settings → Security → API
  tokens) for live testing; unit tests need neither.

## Walkthrough

```bash
# JIRA_CLOUD_ID: find it under admin.atlassian.com, or verify directly
# against the target site (see Troubleshooting above) — it's a UUID, not
# the site name.
export JIRA_CLOUD_ID=1a11d016-8984-4c3e-b9ab-142dd06acb1b
export JIRA_API_TOKEN=...
# Optional — omit for a working but non-browsable issue link (see Design
# notes' "Human-clickable issue links").
export JIRA_SITE_URL=https://acme.atlassian.net
make up
# "Jira integration enabled" in the api container's log, vs. "...DISABLED"
# when JIRA_CLOUD_ID/JIRA_API_TOKEN aren't both set (JIRA_SITE_URL doesn't
# affect this).

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)

curl -s -X POST "http://localhost:8080/v1/sevs/SEV-2026-0001/jira-issues" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"project_key":"OPS","issue_type":"Task","summary":"Investigate root cause",
       "description":"Follow-up from SEV-2026-0001","relationship_type":"action-item","priority":"critical"}'
# {"id":..., "external_system":"jira", "task_id":"OPS-7",
#  "url":"https://acme.atlassian.net/browse/OPS-7", ...}
# (a real, clickable link — this is what a live run actually returned,
# see Live-verified below. With JIRA_SITE_URL unset, url would instead be
# the API's own non-browsable self link.)

# Without JIRA_CLOUD_ID/JIRA_API_TOKEN set, the same call returns 503:
# {"code":14, "message":"Jira integration is not configured (JIRA_CLOUD_ID/JIRA_API_TOKEN not set)"}
```

## Verify tests pass

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... && go test -race ./... && golangci-lint run ./...
```

New coverage: `internal/integrations/tasktracker/jira/client_test.go` (client,
85.2% — Error()/HTTPStatus() untested, matching the existing `github`
package's own 78.7% baseline for the same trivial methods; includes
`TestCreateIssue_NonJSONErrorBody_SurfacedVerbatim` and
`TestCreateIssue_EmptyErrorBody_NoMessages` covering the raw-body fallback
described in Troubleshooting above, and
`TestGetIssue_SiteURLConfigured_*`/`TestCreateIssue_SiteURLConfigured_*`
covering the browse-link construction — including trailing-slash trimming
and key-escaping — described in Design notes), `task_test.go`'s
`TestCreateJiraIssue_*` suite (mirrors every `TestCreateGitHubIssue_*` case:
valid creation, not-configured, API-error-to-status-code mapping, generic
error, duplicate-link conflict, SEV-not-found, validation errors, event
publish, sensitive-SEV suppresses publish), `internal/auth/rbac_test.go`
(the new `CreateJiraIssue` RBAC floor), `internal/config/config_test.go`
(the three new env vars).

Live-verified end-to-end against a running server (in-memory store): route
registered and reachable (confirmed via the generated swagger — `grep jira
internal/api/pb/sevitout/v1/task.swagger.json`), correct `503` when
unconfigured, and — this caught a real bug before it shipped — the RPC was
initially unreachable by *any* caller, admin included, with a `403
insufficient permissions` response, because `internal/auth/rbac.go`'s
`rpcMinRole` table (a method absent from it is denied to everyone by
design) hadn't been updated alongside the new RPC. Fixed by adding the
missing entry; the live retest after the fix confirmed the correct `503`.

Separately, live-verified against a **real Jira Cloud instance**, twice:
first, after correcting a misconfigured `JIRA_CLOUD_ID` (see Troubleshooting
above), `CreateJiraIssue` created a real issue and linked it to a SEV,
confirming the client's gateway URL construction, Bearer auth, and ADF
description encoding all work against Jira's actual API, not just the unit
tests' mocked one; second, after adding `JIRA_SITE_URL`, the same call
returned `"url":"https://sevitout.atlassian.net/browse/KAN-6"` — a real,
correctly-formed browse link, not the API self link. Both test issues were
deleted afterward via a direct `DELETE /rest/api/3/issue/{key}` call against
the gateway host (not something this codebase's `JiraIssueClient` exposes —
Sevitout has no delete-issue feature — done directly against the API to
leave the test project clean; the same `DELETE` against the site's own host
instead of the gateway returned `403`, independently confirming the two
hosts are genuinely separate auth surfaces, not just different URLs for the
same backend).

## Known limitations

- **Human-clickable issue links require `JIRA_SITE_URL`** — without it,
  `Issue.URL` (and the `url` field on the resulting `TaskResponse`) falls
  back to the Jira REST API's own `self` resource link, not a
  `https://{site}.atlassian.net/browse/{key}` page a person can open in a
  browser. This is opt-in rather than automatic because the Cloud ID used
  for actual API calls doesn't determine the tenant's site host — see
  Design notes' "Human-clickable issue links" for why they're two separate
  values.
- **No frontend UI** — `TasksPanel.tsx`'s "Create GitHub Issue" action has
  no Jira sibling yet. The API is fully functional and independently usable
  (per `CLAUDE.md`'s API-first principle) via `curl`, the Slack bot, or a
  future frontend change; adding the UI form is a follow-up, not bundled
  into this backend-focused phase (the roadmap's own Phase 6a scope named
  only backend components).
- **No Linear support yet** — `docs/requirements.md` §13.3 groups Jira and
  Linear as the same "v2 fast-follow." This phase only implements Jira;
  Linear can follow the same `JiraIssueClient`-sibling shape later.
- **`adfToPlainText` is best-effort**, not a full ADF renderer — it extracts
  plain text from `text`/`paragraph` nodes only. A real Jira description
  using tables, mentions, code blocks, or other ADF node types will lose
  that formatting when read back via `GetIssue` (not currently called by
  any handler — `CreateJiraIssue` only writes). Revisit if a future feature
  needs to display existing Jira issue descriptions.

See [`docs/roadmap.md`](../docs/roadmap.md) (Phase 6a) for the fuller
sequencing rationale.
