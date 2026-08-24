# M14c Demo — Postmortem Editor

## What was built

The third frontend sub-milestone (`docs/project-plan.md`'s M14c): a full postmortem
editor at `/sevs/:id/postmortem`, wired up against the M05 `PostmortemService` and
M12 `AIService` backends — no backend changes were needed for this milestone.

- **TipTap editor with a Markdown extension** (`components/postmortem/
  PostmortemEditor.tsx`) — `@tiptap/react` + `@tiptap/starter-kit` +
  `tiptap-markdown`, matching `docs/architecture.md` §12's resolved decision that
  postmortems are stored as Markdown text, not ProseMirror JSON or HTML: the editor
  parses `postmortems.content` as Markdown on load and serializes back to Markdown
  (`editor.storage.markdown.getMarkdown()`) on every change, so the stored document
  stays portable and human-readable outside the UI. `editable`/`content` are pushed
  into the (TipTap-owned) editor instance imperatively via two effects rather than
  treated as literal React-controlled values, which is the standard pattern for
  wrapping TipTap in React.
- **Status workflow controls** (`PostmortemStatusControl.tsx`) — Draft → In Review →
  Approved, gated to Incident-Commander+ (matches
  `PostmortemService.TransitionPostmortemStatus`'s RBAC floor) and only offering the
  state machine's actual valid next statuses
  (`internal/postmortem/statemachine.go`), same "client-side UX sugar, server is the
  real authority" pattern `StatusTransitionControl` already established for the SEV
  itself in M14b.
- **Locked state**: read-only by default; the action button is **Edit** (Responder+)
  when the SEV isn't locked, or **Unlock to edit** (Incident-Commander+ — matches
  `UnlockSEV`'s stricter RBAC floor) when it is. Unlocking opens a reason modal
  (`components/postmortem/UnlockDialog.tsx`, built on a new minimal
  `components/ui/dialog.tsx` — no Radix, same "plain element" choice as `select.tsx`/
  `checkbox.tsx`); the reason is written to the audit log server-side
  (`PostmortemService.UnlockSEV`), and the returned short-lived token is attached to
  the next `UpdatePostmortem` call. After a successful save, the token is discarded
  — the next edit needs a fresh reason. This "auto-lock on save" is really just
  "never actually re-lock, always require re-authorization": confirmed directly
  against the backend that `sev.locked` itself is only ever flipped by a SEV status
  transition (`SEVServer.TransitionStatus`), never by `UpdatePostmortem` — the
  unlock token only ever authorizes the one write it's attached to.
- **AI draft suggestion** (`components/postmortem/AIDraftPanel.tsx`) — shows the most
  recent `AI_ACTION_DRAFT_POSTMORTEM` output (from the proactive "Resolved → draft
  postmortem skeleton" trigger, §11.1, or a manually requested one), always visibly
  marked "AI-generated — not authoritative, review before use" (§11.3). A
  Responder+ can regenerate it (`AIService.TriggerAction`) or **Apply to editor**,
  which loads the draft into the editor (entering edit/unlock flow first if needed)
  for review and further editing — it never saves by itself.
- A **Postmortem** button on the SEV detail page header links to the new route.
- **Auto-seeded starter template** (`lib/postmortemTemplate.ts`) — the first time
  anyone opens a blank postmortem for editing, the editor is pre-filled from the
  SEV's own recorded facts instead of a blank page: **Summary** (the SEV's
  description), **Lifecycle** (a Markdown table of Started/Detected/Mitigated/
  Resolved/Postmortem-complete timestamps with the time delta between each
  consecutive pair), **Root Cause** (category label + description), **Business
  Impact**, **Services Affected**, and **Mitigation** — Root Cause includes the
  category, description, and (if set) the root-cause reference link
  (`root_cause_reference_url`, see `demo/M14b-sev-list-detail.md`'s "Root cause
  reference link" follow-up) pointing at the concrete PR/commit/config change that
  caused it. Missing facts render as an
  explanatory placeholder (e.g. `_Not yet determined._`) rather than being omitted,
  so the section headers stay a consistent checklist. This only ever seeds a
  genuinely empty document — once real content exists (human-written or an applied
  AI draft), `Edit` reopens it unchanged.
- **Code-split**: `PostmortemPage` is lazy-loaded (`React.lazy` + `Suspense` in
  `App.tsx`) — TipTap + ProseMirror are by far the heaviest dependency in this app
  (~530KB minified), and there's no reason to make every other page pay for it.

---

## Prerequisites

- M14b complete
- Backend milestones M05 (Postmortem) and M12 (AI Plugin System) — no backend
  changes were needed for this milestone, but its RPCs are what this page calls
- Node.js 22+, `npm`
- `JWT_SECRET` set (or accept the dev default)
- No AI plugin needs to be configured to exercise this demo — the "Generate Draft"
  button is disabled with an explanatory `title` when `AIService.ListPlugins`
  returns none, and the rest of the page works fully without one

---

## Start the stack

Same two options as prior M14 sub-milestones:

```bash
# Option A — Docker Compose
cp .env.example .env   # if you don't already have one
make up

# Option B — local dev servers
JWT_SECRET=dev-secret-please-change JWT_TTL_HOURS=24 go run ./cmd/server   # terminal 1
cd web && npm install && npm run dev                                      # terminal 2
```

Open **http://localhost:3000** (Option A) or **http://localhost:5173** (Option B).

---

## Walkthrough

1. **Open a SEV's postmortem.** From `/sevs/:id` (any SEV — one is auto-created in
   Draft status when the SEV itself is created), click **Postmortem** in the header.
   You land on `/sevs/:id/postmortem` showing the Draft status badge and empty
   editor content.
2. **Edit as a Responder and see the auto-seeded template.** Click **Edit** — the
   editor becomes editable and, since the document is still blank, is pre-filled
   with **Summary / Lifecycle / Root Cause / Business Impact / Services Affected /
   Mitigation** sections built from whatever the SEV record already has (fill in a
   description, business impact, root cause, etc. on the SEV's Details panel first
   to see them show up here). Edit the text as needed and click **Save**. The page
   returns to read-only, rendering the Markdown as formatted output. Click **Edit**
   again — this time the exact content you saved reopens unchanged (the template
   only ever seeds a genuinely empty document).
3. **Transition the workflow as an Incident Commander (or Admin).** With the
   postmortem in Draft, "Transition to: In Review" appears — click it. Then
   "Transition to: Approved, Draft" appears (approved is terminal going forward,
   Draft sends it back for revision) — click **Approved**.
4. **Lock the SEV and confirm the postmortem locks with it.** From `/sevs/:id`,
   transition the SEV itself all the way to `Postmortem Complete` (Investigating →
   Mitigated → Resolved → Postmortem In Progress → Postmortem Complete). Return to
   the postmortem page: it now shows a "SEV Locked" badge, the content is read-only,
   and the action button is **Unlock to edit** instead of **Edit**.
5. **Try it as a plain Responder first** — confirm there's no "Unlock to edit"
   button at all (only Incident-Commander+ can unlock; a Responder sees the locked
   page with genuinely no way to edit it, matching `UnlockSEV`'s RBAC floor being
   stricter than `UpdatePostmortem`'s).
6. **Unlock as an Incident Commander.** Click **Unlock to edit**, type a reason in
   the modal, click **Unlock**. The editor becomes editable immediately using the
   returned token. Make an edit and **Save** — it succeeds. Click **Edit** again
   (well, **Unlock to edit** again) — a fresh reason is required; the previous
   token isn't reused.
7. **Generate and apply an AI draft** (works with or without a configured plugin,
   to show both paths):
   - Without a plugin configured: the **Generate Draft** button in the "AI Draft
     Suggestion" panel is disabled, with a tooltip explaining why.
   - With one configured (`docs/demo/M12-ai-plugin.md` covers registering one):
     click **Generate Draft**, wait for it to appear (clearly marked
     AI-generated), then **Apply to editor** — the draft loads into the editor
     (prompting for an unlock reason first if the SEV is locked) for you to review
     and edit before **Save**.

---

## Verify tests pass

```bash
cd web
npm run build
npx oxlint
npm test
```

The Go side is untouched by this sub-milestone (confirmed by re-running its full
suite unchanged): `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...`,
`go test -race ./...`, and `golangci-lint run ./...` all still pass.

61 Vitest/RTL tests (15 new since the initial M14c build: 9 from the original
editor/lock/AI-draft work, 5 for the auto-seeded template, 1 for the table-
rendering fix below):

- `components/postmortem/PostmortemEditor.tsx` — parses Markdown into formatted
  output, toggles its `contenteditable` DOM attribute correctly for
  `editable={true}`/`{false}`, and renders a Markdown table as an actual `<table>`
  with distinct `<th>`/`<td>` cells rather than run-together text — this exercises
  the real TipTap/ProseMirror runtime in jsdom, not a mock, so it's also
  incidental confirmation the editor works at all outside a real browser.
- `lib/postmortemTemplate.ts` — every section renders from a fully-populated SEV
  (including correct lifecycle-table deltas), falls back to explanatory
  placeholders when nothing is recorded, and shows an "Other" free-text root-cause
  category as-is rather than mangling it.
- `pages/PostmortemPage.tsx` — renders fully read-only for a Viewer with no
  Edit/Unlock/transition controls; a Responder can edit and save an unlocked
  postmortem; opening a *blank* postmortem seeds the auto-template from the SEV's
  facts; opening one that already has content leaves it untouched; a Responder
  sees no Unlock option at all on a locked SEV; an Incident Commander can unlock
  (reason modal → token → save, confirmed in the request body), and sees the
  correct valid-next-status transition buttons; an existing AI draft renders
  clearly marked and Apply loads it into the editor.

Manually verified end-to-end against a live server before writing any frontend
code: `GetPostmortem`/`UpdatePostmortem`/`TransitionPostmortemStatus` (including the
rejected `approved → draft` transition), the full lock/unlock/re-edit cycle
(`SEVService.TransitionStatus` to `postmortem_complete` → `sev.locked: true` →
`UpdatePostmortem` without a token correctly 403s → `PostmortemService.UnlockSEV` →
`UpdatePostmortem` with the token succeeds), and `AIService.TriggerAction`/
`ListOutputs`/`ListPlugins`'s exact wire shapes (protojson enum fields serialize by
name, e.g. `"AI_ACTION_DRAFT_POSTMORTEM"`, not by number).

---

## Bug fix: postmortems now survive an API restart

Reported after this milestone shipped: opening a SEV's postmortem sometimes showed
"Failed to load postmortem: postmortem not found" for a SEV that definitely had one.
Root cause — confirmed by reproducing it directly — was **not** in this milestone's
frontend code: `PostmortemStore` had been in-memory-only since M05 (`docs/project-
plan.md`'s M01 always intended a Postgres implementation; it was simply never
written). A SEV itself is Postgres-backed and survives an `api` process
restart; its postmortem, being in-memory, didn't — so restarting the server (a
redeploy, a crash, `docker compose restart api`) silently orphaned every existing
postmortem while leaving its SEV intact and clickable.

Fixed by adding `internal/store/postgres/postmortem.go` and wiring it into
`cmd/server/main.go`'s `buildStores`. The SQL and generated sqlc code
(`internal/store/sql/postmortems.sql`, `internal/store/queries/postmortems.sql.go`)
had existed unused since M01 — only `CountByStatus` needed a new query
(`CountPostmortemsByStatus`, used by `ReportService.GetDashboardMetrics`'s
postmortem completion rate). No migration was needed; the `postmortems` table has
existed since M01 too.

Verified directly: created a SEV, set postmortem content, killed and restarted the
`go run ./cmd/server` process against the same Postgres, and confirmed
`GetPostmortem` still returned the content afterward (this exact sequence returned
"not found" before the fix) — then transitioned it through to `approved` and
confirmed `GetDashboardMetrics`'s `postmortem_completion_rate` computed correctly
from the new `CountByStatus` query.

**Caveat surfaced by a follow-up report**: the fix only prevents *future* loss. Any
postmortem whose SEV was created before the fix landed in a running process — e.g.
`SEV-2026-0001`/`SEV-2026-0002` in a dev stack that had been restarted since — has
no row in the `postmortems` table at all, because it was never written to Postgres
in the first place; opening it correctly (if unhelpfully) still shows "postmortem
not found". Re-verified end-to-end against the `docker compose` stack
(`sevitout-api-1`): created `SEV-2026-0004`, wrote postmortem content, ran
`docker restart sevitout-api-1` (equivalent to a redeploy), and confirmed
`GetPostmortem` still returned the content afterward. There is no way to recover
content that was lost before the fix shipped — only newly created SEVs (or ones
edited for the first time after the fix landed) benefit.

---

## Bug fix: the Lifecycle table rendered as run-together text

Reported after the auto-seeded template (below) shipped: the Lifecycle section's
Markdown table showed up as one run-together line —
`StageTimestampTime since previousStartedAug 21, 2026...` — instead of an actual
table. Root cause: the editor
(`components/postmortem/PostmortemEditor.tsx`) only registered `StarterKit` +
`tiptap-markdown`'s `Markdown` extension, and neither defines a `table`/
`tableRow`/`tableHeader`/`tableCell` node type. Without those node types in the
schema, markdown-it still parsed the table syntax correctly, but ProseMirror had
nowhere to put the parsed rows/cells and collapsed them into a single paragraph.

Fixed by adding `@tiptap/extension-table`'s `TableKit` (bundles all four node
types in one extension) to the editor's extension list — `tiptap-markdown`
already ships its own Markdown parse/serialize behavior keyed to the `table` node
name, so no further wiring was needed. Added `.tiptap-content table/th/td`
styling in `index.css` (borders, header shading), since a bare `<table>` renders
with no visible structure otherwise.

Verified with a new `PostmortemEditor.test.tsx` case that renders the exact
reported Markdown table against the real TipTap/ProseMirror runtime and asserts
it produces an actual `<table>` with three `<th>`s and correctly-separated
`<td>`s per row, not concatenated text.

---

## Known limitations

- **No live collaboration** — two people editing the same postmortem
  simultaneously will silently overwrite each other's `Save`, same as every other
  edit-in-place panel in this app (Details, Lifecycle). The WebSocket subscription
  on this page only refetches on a `postmortem.updated` event from *another*
  client; it doesn't merge concurrent edits.
- **`tiptap-markdown`'s upstream status**: the package's README carries a notice
  that it won't see further releases (TipTap's own paid conversion extension is the
  suggested future path). It's still fully functional for this v1 scope — pinned at
  `^0.9.0` — but worth knowing if TipTap's own core APIs move out from under it
  later.
- **No streaming AI draft.** `AIService.StreamAction` exists on the backend (and
  itself only re-emits word-chunked pieces rather than true token streaming — see
  `demo/M12-ai-plugin.md`), but this page uses the synchronous `TriggerAction` only;
  adding a streaming "watch it type" UI wasn't part of this milestone's scope.
- **No diff view between the AI draft and the current document** — Apply replaces
  the editor's entire content with the draft; there's no side-by-side comparison or
  partial-apply.
- **The unlock reason isn't shown anywhere in the UI after the fact** — it's
  written to the audit log (`AuditService.ListAuditEntries`, M02), which has no
  frontend surface yet either; both are DetailsPanel/M14d-adjacent gaps, not new
  ones introduced here.
