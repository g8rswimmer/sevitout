# M14e Demo — Public Share View & Reporting

## What was built

The fifth and final frontend sub-milestone (`docs/project-plan.md`'s M14e): a public,
unauthenticated shareable-link view, a dedicated Reports page, and a CSV export
button — wired up against the M13 `ShareService`/`ReportService` backends. No backend
changes were needed for this milestone, but getting the share view working in a
browser required fixing a real infrastructure conflict (below).

- **Public share view** (`/s/:token`, `pages/PublicSharePage.tsx`) — the one page in
  this app that renders outside `AppLayout`/`ProtectedRoute`; anyone with the link can
  open it with no session. Fetches `GET /s/{token}` (`api.shareView.get`,
  `internal/api/grpc.ShareViewHandler` — a plain `net/http` handler, not
  gRPC-gateway) and renders exactly the curated fields it returns: title, severity,
  status, lifecycle timestamps, business impact, and `external`-audience
  announcements. Shows the server's own descriptive error text verbatim for a
  revoked/expired/unknown link (`link has been revoked`, `link has expired`, `link
  not found`), rather than a generic "something went wrong."
- **Infrastructure fix: `/s/:token` now serves both a page and an API, at the same
  URL.** The backend hands out share links at `/s/{token}` (`ShareLinkResponse.path`),
  and the M14e plan calls for the frontend route to live at that exact path too — but
  `nginx.conf`/`vite.config.ts` already unconditionally proxied every `/s/` request
  straight to the Go API (added in M14b specifically to keep this path reserved).
  Fixed by content-negotiating on the `Accept` header: a browser's top-level
  navigation to a share link sends `Accept: text/html`, so both proxies now serve the
  SPA shell for that case and fall through to the real backend for everything else
  — including `PublicSharePage`'s own `fetch()` call back to the identical URL
  (default `Accept: */*`, not affected) and every existing `curl` example in
  `demo/M13-reporting-sharing.md`, which all still work completely unchanged. See
  `nginx.conf`'s and `vite.config.ts`'s `/s/` block comments for the mechanism
  (`if ($http_accept ~* "text/html") { rewrite ^ /index.html last; }` in nginx; a
  `bypass` function in Vite's dev proxy).
- **Share link management** (`components/sev/ShareLinkControl.tsx`) — a **Share**
  button on the SEV detail page header (Incident-Commander+, matching
  `CreateShareLink`/`RevokeShareLink`'s RBAC floor; hidden entirely for a Sensitive
  SEV, which the backend refuses to link anyway) opening a dialog to create a link
  (with a configurable expiry) or copy/revoke the current one. There's no
  `ListShareLinks` RPC — only Create/Revoke — so the created link only exists in this
  component's own state for as long as the SEV detail page stays open; state is kept
  in the component that renders the dialog (not the dialog's children, which unmount
  whenever it's closed) specifically so closing and reopening the dialog doesn't lose
  track of it.
- **Reports page** (`/reports`, `pages/ReportsPage.tsx`, linked from the main nav for
  every authenticated role — all three backing RPCs are Viewer+):
  - **MTTR Trend** — the same pure-CSS bar chart Dashboard already had, extracted
    into a shared `components/reports/MTTRTrendChart.tsx` so both pages use one
    implementation.
  - **Postmortem Completion Rate** — a bigger presentation of the same
    `GetDashboardMetrics` field Dashboard's card already shows.
  - **Service × Severity Heatmap** — `frequency_by_service_and_level` as a table with
    services resolved to their registry name (`GET /v1/config/services`, the same
    call `ServiceChipEditor`/`AdminOnCallPage` already make) and cells shaded in a
    few discrete `bg-primary` opacity steps relative to the highest count in the
    grid — no charting library, no hardcoded color (`--primary` is already
    theme-aware).
  - **Recurring Incident Patterns** — `ReportService.GetSEVTrends` (previously
    completely unused by the frontend): every (service, root cause category) pair
    with 2+ matching SEVs, each linked SEV ID a clickable link to its detail page.
- **CSV export** (`SevListPage.tsx`'s new **Export CSV** button) — fetches
  `GET /v1/sevs/export.csv` with whatever severity/status filters are currently
  active on the list page and downloads the raw response via `lib/download.ts`'s
  `downloadTextFile` (the same generic Blob-download helper the postmortem page's
  "Download .md" button introduced, reused here exactly as that milestone's demo
  doc anticipated). `api.ts` gained a `requestText` helper alongside `request` for
  the two endpoints in this app whose response isn't `{"message"|"error": ...}`-shaped
  JSON: this CSV export (raw `text/csv`) and the share view's plain-text errors.

---

## Prerequisites

- M14a–M14d complete
- Backend milestone M13 (Reporting, Analytics & Public Shareable Links) — no
  backend changes were needed for this milestone
- Node.js 22+, `npm`
- `JWT_SECRET` set (reused for share-link signing, same as M13)

---

## Start the stack

```bash
# Option A — Docker Compose (required to exercise the nginx /s/ content
# negotiation described above; the dev proxy handles it too, but nginx is
# what production actually runs)
cp .env.example .env
make up

# Option B — local dev servers
JWT_SECRET=dev-secret-please-change JWT_TTL_HOURS=24 go run ./cmd/server   # terminal 1
cd web && npm install && npm run dev                                      # terminal 2
```

Open **http://localhost:3000** (Option A) or **http://localhost:5173** (Option B).

---

## Walkthrough

1. **Create a SEV with some data to report on.** From `/sevs/new`, create one or two
   SEVs, set a root cause category and affected service on each (Details panel),
   and resolve at least one so it has an MTTR. If you create two SEVs sharing both
   the same affected service and root cause category, the backend auto-links them
   as recurring (M13) — useful for step 4 below.
2. **Share a SEV.** Open a SEV's detail page as an Incident Commander or Admin,
   click **Share** in the header. Set an expiry (default 30 days) and click
   **Create link** — a public URL appears with a **Copy** button and its expiry
   date. Click **Copy**, then open that URL in a private/incognito window (no
   login): you see the curated public summary — title, severity, status, lifecycle
   timestamps, business impact, and any `external`-audience announcements — and
   nothing else (no root cause, no chat log, no internal announcements). Post an
   `internal`-audience announcement on the SEV and confirm it does *not* appear on
   the public view; post an `external` one and confirm it does.
3. **Revoke it.** Back on the SEV detail page, reopen the Share dialog (your
   created link is still shown — it's remembered for as long as this page stays
   open) and click **Revoke link**. Reload the public URL: it now shows "link has
   been revoked" instead of the summary. Try a garbage token in the URL too: "link
   not found".
4. **Try Share as a plain Responder** — confirm there's no Share button at all
   (Incident-Commander-only). Flag a SEV **Sensitive** in its Details panel as an
   Incident Commander and confirm Share disappears for that SEV specifically, even
   for you.
5. **Open Reports** (`/reports` in the main nav — visible to every role). Confirm
   the MTTR trend chart matches Dashboard's, the Postmortem Completion Rate matches
   too, the Service × Severity Heatmap shows a cell for each service/severity
   combination you've created SEVs for (darker shading for higher counts), and — if
   you created the recurring pair in step 1 — a row under Recurring Incident
   Patterns with both SEV IDs linking back to their detail pages.
6. **Export CSV.** On `/sevs`, check a severity filter (e.g. SEV-2), click **Export
   CSV** — a `sevs-export.csv` file downloads containing only SEVs at that severity.
   Open it: the header row and one row per matching SEV, matching
   `demo/M13-reporting-sharing.md`'s `curl` example exactly.

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

102 Vitest/RTL tests (14 new):

- `pages/PublicSharePage.tsx` — renders the curated summary (title, severity,
  status, timestamps, business impact, external announcements) for a valid token;
  shows the server's exact plain-text error message for a revoked/expired/unknown
  link; omits sections entirely for fields the public view doesn't send (no
  "Business impact"/"Updates" heading when absent, not just an empty value).
- `components/sev/ShareLinkControl.tsx` — hidden when `canShare` is false; creates
  a link and shows the copyable URL + expiry; keeps the created link visible after
  closing and reopening the dialog; revokes and returns to the create form; shows
  the server's error message on a failed create (e.g. a sensitive SEV).
- `pages/ReportsPage.tsx` — renders the MTTR trend, postmortem completion rate, a
  service heatmap resolved to service names (not bare IDs) with the right counts,
  and a recurring-patterns table with clickable SEV-ID links; shows an explanatory
  message when there are no recurring patterns yet.
- `pages/SevListPage.tsx` — **Export CSV** requests `/v1/sevs/export.csv` with the
  currently-checked severity filter and downloads the response.
- `pages/SevDetailPage.tsx` — Share is hidden for a Responder, shown for an
  Incident Commander, and hidden again for an Incident Commander on a Sensitive SEV.

Manually verified end-to-end against a live Docker Compose stack (`nginx`, not just
the Vite dev proxy) before writing this doc: created a SEV, created and copied a
share link, confirmed the exact same `/s/{token}` URL returns the rendered SPA page
for a browser-like request (`Accept: text/html`) and the raw JSON/plain-text
response for `curl`'s default request and for a simulated `fetch()` call (no
`Accept: text/html`) — including after revocation, where the browser-like request
still gets `200` (the SPA shell) while the *page's own* fetch to that URL correctly
gets `410` with `link has been revoked`, which is what makes the error render
correctly client-side. Also verified `/sevs/:id` and other existing SPA routes are
completely unaffected, `GET /v1/reports/trends`, and `GET /v1/sevs/export.csv`
through the same nginx container.

### Bug fix: pasting a share link into a real browser showed a JSON-parse error

Reported after this milestone shipped: opening a freshly-copied share link in a new
browser window (not `curl`, a real browser) showed **"Unexpected token '<',
"\<!doctype "... is not valid JSON"** instead of the summary. The Accept-based
content negotiation above was correct and *did* serve the SPA shell for the initial
navigation — the bug was one HTTP request later. `PublicSharePage` mounts and calls
`api.shareView.get(token)`, a plain `fetch()` to that *exact same URL*
(`GET /s/{token}`) to get the real JSON. nginx's static-file handler serves
`index.html` with `Last-Modified`/`ETag` but no `Cache-Control` — which browsers
treat as heuristically cacheable — so that second, same-URL `fetch()` was answered
straight from the browser's own HTTP cache with the already-cached `index.html`,
never reaching nginx (let alone the Accept-header branch) a second time at all.
Confirmed directly: `curl -sI -H "Accept: text/html" .../s/{token}` showed
`Last-Modified`/`ETag` and no `Cache-Control` before the fix.

Fixed with a dedicated `location = /index.html { add_header Cache-Control
"no-store"; }` block in `nginx.conf` — `no-store` forbids caching this response
under any circumstance, for any URL that happens to resolve to it (both the SPA
fallback's `try_files` and the `/s/` block's `rewrite ... last` funnel through it).
This is good practice independent of M14e too: an SPA's `index.html` references
content-hashed asset filenames that change on every deploy, so caching it at all
risks serving a page that points at JS/CSS bundles which no longer exist. The Vite
dev proxy was never affected — Vite's dev server already sends `Cache-Control:
no-cache` for served HTML by default, confirmed directly against `localhost:5173`.

Re-verified after the fix: the same `curl -sI` now shows `Cache-Control: no-store`,
and a plain `curl` GET to the identical URL with no `Accept` override (standing in
for the page's own `fetch()`) correctly returns the real JSON every time, not the
cached shell — this exact scenario is what was broken before.

---

## Known limitations

- **No `ListShareLinks` RPC** — the Share dialog can only ever show the link it
  itself just created or is about to create; there's no way to see or revoke a link
  someone else created (or one created in a previous session), short of the
  backend's own audit log (`sev.share_link_created`/`sev.share_link_revoked`
  entries) or direct database access. This is a backend API gap, not something
  fixable from the frontend alone.
- ~~**`ShareStore` is in-memory only** even when `DATABASE_URL` is set (M13's own
  known limitation, unchanged) — a share link, like an integration credential or AI
  plugin (M14d), does not survive an API restart, even though the SEV it points at
  does.~~ **Fixed**: see `demo/M14f-remaining-store-persistence.md`.
- **The Accept-header content negotiation on `/s/{token}` is a heuristic, not a
  guarantee** — any HTTP client that happens to send `Accept: text/html` (unusual
  for a script, but not impossible) gets the SPA shell instead of JSON. This matches
  how a real browser always behaves for a top-level navigation and how `fetch()`
  always behaves by default, which is what matters for this app's own two callers of
  that URL (a browser tab, and `PublicSharePage`'s own `fetch()`), but a
  hypothetical third-party integration that explicitly sets `Accept: text/html` when
  calling the API would need to know to omit it.
- **The Service × Severity Heatmap and Recurring Patterns table both resolve
  `service_id` via a client-side lookup against the full service registry list** — fine
  at this app's expected scale (`docs/requirements.md` §19), but it's an extra
  request and an O(n) `.find()` per cell/row rather than a server-side join.
- **CSV export from the SEV list only forwards the severity/status filters already
  on that page** — `service_ids`, `root_cause_category`, and the date-range filters
  `ExportSEVsRequest` supports aren't exposed as UI controls anywhere yet (that page
  doesn't filter on them at all, in fact — `SearchService` does, but `ExportSEVs`
  and `SearchSEVs` are separate RPCs with separately-shaped filters, per M13's own
  known limitations).
