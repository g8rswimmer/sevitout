# Demo — Linked Issues frontend (Roadmap Phase 7)

## What was built

Phase 6a shipped `CreateJiraIssue` on the backend
(`POST /v1/sevs/{sev_id}/jira-issues`) with no frontend caller — an explicit
Known limitation there. Reviewing the running "Linked tasks" panel
(`TasksPanel.tsx`) surfaced a second, independent gap: `TaskResponse
.external_system` was fetched but never rendered, so a GitHub issue, a Jira
issue, and a plain manually-linked URL were visually indistinguishable in the
list.

**7a. Create Jira issue from the UI**

- `web/src/types/api.ts`: new `CreateJiraIssueRequest` (`project_key`,
  `issue_type`, `summary`, `description?`, `relationship_type`, `priority`),
  mirroring `CreateGitHubIssueRequest`'s shape and the backend proto exactly.
- `web/src/lib/api.ts`: `tasks.createJiraIssue(sevId, req)` — `POST
  /v1/sevs/{sevId}/jira-issues`, mirroring `tasks.createGitHubIssue`.
- `web/src/components/sev/TasksPanel.tsx`: `Mode` extended to `'link' |
  'github' | 'jira'`, a third "Create Jira issue" button, and a third form
  (project key + issue type in place of owner/repo, the shared title input
  relabeled "Summary" — Jira's own naming for the field GitHub calls
  "title"). As planned, there's no SEV-level field to pre-fill a default
  project key the way `github_repo`/`parseRepo()` does for GitHub — the
  field starts empty every time; that stays a follow-up, not folded into
  this phase.

**7b. Distinguish github / jira / generic in the list**

- `web/src/types/api.ts`: `TaskResponse.external_system` tightened from a
  bare `string` to `KnownExternalSystem | (string & {})` — `'github' |
  'jira' | 'generic'` get real labels/colors, while any other value (the
  field stays unvalidated free text server-side) still round-trips and
  renders as-is via the `(string & {})` widening trick, rather than being
  narrowed away.
- `web/src/components/sev/badges.tsx`: new `ExternalSystemBadge({ system })`,
  the same thin-`<Badge>`-wrapper-over-a-lookup-map pattern as
  `SeverityBadge`/`StatusBadge` (`EXTERNAL_SYSTEM_LABELS`/
  `EXTERNAL_SYSTEM_BADGE_CLASS` in `types/api.ts`). Used in
  `TasksPanel.tsx`'s list render, next to each entry's relationship-type
  badge. No new icon dependency — consistent with how relationship
  type/priority/overdue already communicate purely through badge color+text
  in this panel.

## Design notes

**`(string & {})` instead of a plain `string` fallback**, so
`TaskResponse.external_system`'s type keeps offering `'github'`/`'jira'`/
`'generic'` as editor autocomplete while still accepting (and round-tripping)
any string — a bare `'github' | 'jira' | 'generic' | string` collapses to
just `string` in TypeScript and loses the literal suggestions entirely.

**An unrecognized `external_system` renders its raw value in an outline
badge**, not a blank or generic label — `ExternalSystemBadge` falls through
to `<Badge variant="outline">{system}</Badge>` for anything outside the three
known values, since the field has always been unvalidated free text
server-side (a caller can `LinkTask` with any string) and silently hiding
that would be worse than showing it plainly.

**The shared Title input relabels to "Summary" in Jira mode** rather than
adding a second, Jira-specific title field — `title`/`summary`/`owner+repo`
all feed the same underlying form state slots that already existed for
link/GitHub, so switching modes is a rename plus a field-set swap, not new
state.

## Prerequisites

- `web`'s `tsc -b && vitest run` passing.
- A running server with `JIRA_CLOUD_ID`/`JIRA_API_TOKEN` configured (Phase
  6a) to exercise the create-issue path against a real Jira Cloud instance.

## Walkthrough

Live-verified end-to-end: registered a Responder, created a SEV, then issued
the exact request `TasksPanel`'s new Jira form now sends
(`POST /v1/sevs/{id}/jira-issues` with `project_key`/`issue_type`/`summary`/
`description`/`relationship_type`/`priority`) against a real running server
with real Jira credentials:

```bash
curl -s -X POST localhost:8080/v1/sevs/$SEV_ID/jira-issues \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
    "project_key": "KAN", "issue_type": "Task",
    "summary": "Phase7 frontend smoke test",
    "description": "Elevated 500s on checkout",
    "relationship_type": "action-item", "priority": "non-critical"
  }'
# {"id":"6","sev_id":"SEV-2026-0051","external_system":"jira",
#  "task_id":"KAN-8","url":"https://sevitout.atlassian.net/browse/KAN-8", ...}

curl -s localhost:8080/v1/sevs/$SEV_ID/tasks -H "Authorization: Bearer $TOKEN"
# ListTasks returns external_system: "jira" — exactly what
# ExternalSystemBadge needs to render the blue "Jira" badge.
```

The server log confirmed a genuine ~1.15s round trip to Jira's real
create-issue API (`jira issue created project_key=KAN key=KAN-8`), so the
frontend's request shape and the backend's response mapping are both
confirmed correct against the live integration, not just a mocked test.

## Verify tests pass

```bash
cd web && npx tsc -b && npx vitest run && npx oxlint
```

New coverage: `web/src/components/sev/TasksPanel.test.tsx` (new file) —
per-tracker badge labeling including the unknown-value fallback, hiding the
management controls when `canManage` is false, a full create-Jira-issue
round trip asserting the posted body and the post-success form reset, and
the error-message-surfaced-on-failure path.

## Known limitations

- **No SEV-level default project key.** Unlike GitHub's `github_repo`
  (pre-filling owner/repo via `parseRepo()`), there's no equivalent Jira
  field on a SEV yet — the project-key/issue-type inputs start empty on
  every visit. Adding one is a schema/proto change, deliberately out of
  scope for this phase.
- **The live-verified Jira issue (`KAN-8`) could not be confirmed to persist
  after creation** — a follow-up `GET`/JQL search on the same tenant a few
  seconds later found the `KAN` project itself gone (`project/search`
  returned zero projects), despite the site/Cloud ID (`_edge/tenant_info`)
  still resolving correctly. The create call itself round-tripped genuinely
  (real latency, a real key and browse URL logged server-side) — this looks
  like state on the external Jira Cloud test tenant changing independently
  of this session (e.g. the project being deleted through Atlassian's UI or
  a trial/plan change), not a defect in the request this phase's UI sends.
  Not reproducible from this codebase alone; noted here rather than silently
  dropped.
